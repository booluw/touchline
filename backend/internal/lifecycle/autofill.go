// AI squad auto-fill (A09): when a season rolls over, AI-controlled clubs
// whose squad has dropped below the target size automatically sign the best
// free agents available in their country pool. Signing is deterministic —
// seeded from (world_id, season, club_id) — so the same world state always
// reproduces the same signings. Runs inside the lifecycle rollover tx, after
// intake and retirement, and emits one AI_AUTO_FILL event per club.
package lifecycle

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math/rand"
	"sort"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/touchline/backend/internal/playerpool"
	"github.com/touchline/backend/pkg/eventbus"
)

// TargetSquadSize is the steady-state squad size AI clubs maintain through
// auto-fill signings.
const TargetSquadSize = 24

// autoFillCandidate is one free agent eligible for an AI signing.
type autoFillCandidate struct {
	ID       uuid.UUID
	Position string
	Overall  int
}

// groupQuota is the number of players each squad group must hold before the
// template stops counting it as a gap (2 GK, 7 DEF, 7 MID, 6 FWD, 2 flex).
var groupQuota = map[string]int{"GK": 2, "DEF": 7, "MID": 7, "FWD": 6}

// AutoFill walks every AI club in the country (world-wide when countryID is
// uuid.Nil) and signs free agents until each squad reaches TargetSquadSize,
// returning the total number of players signed. Position gaps are filled
// first using the squad template, then the remainder by overall rating. The
// signing order is derived from a per-club RNG seeded from (world_id, season,
// club_id) applied as a tie-break on equal overall. Signings that fail the
// free-agent guards (e.g. a street prospect under 18) are skipped; a club is
// left short only when its country pool is exhausted.
func (s *Service) AutoFill(ctx context.Context, tx pgx.Tx, worldID, countryID uuid.UUID, season int, ref time.Time) (int, error) {
	clubs, err := s.aiClubsInCountry(ctx, tx, worldID, countryID)
	if err != nil {
		return 0, err
	}
	total := 0
	for _, clubID := range clubs {
		n, err := s.autoFillClub(ctx, tx, worldID, countryID, clubID, season, ref)
		if err != nil {
			return total, err
		}
		total += n
	}
	return total, nil
}

// aiClubsInCountry lists AI-controlled clubs in the country. When countryID
// is uuid.Nil the query falls back to the whole world (no country filter).
func (s *Service) aiClubsInCountry(ctx context.Context, tx pgx.Tx, worldID, countryID uuid.UUID) ([]uuid.UUID, error) {
	rows, err := tx.Query(ctx, `
		SELECT DISTINCT c.id
		FROM club.clubs c
		LEFT JOIN competition.club_competitions cc ON cc.club_id = c.id
		LEFT JOIN competition.competitions comp ON comp.id = cc.competition_id
		WHERE c.world_id = $1 AND c.is_ai_controlled = TRUE
		  AND ($2::uuid = '00000000-0000-0000-0000-000000000000'
		       OR comp.country_id = $2)`,
		worldID, countryID)
	if err != nil {
		return nil, fmt.Errorf("lifecycle ai clubs: %w", err)
	}
	defer rows.Close()

	var ids []uuid.UUID
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("lifecycle ai clubs scan: %w", err)
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

// autoFillClub signs players into one AI club until its squad reaches the
// target size. Returns the number of players signed.
func (s *Service) autoFillClub(ctx context.Context, tx pgx.Tx, worldID, countryID, clubID uuid.UUID, season int, ref time.Time) (int, error) {
	squadSize, err := activeSquadSize(ctx, tx, clubID)
	if err != nil {
		return 0, err
	}
	if squadSize >= TargetSquadSize {
		return 0, nil
	}
	need := TargetSquadSize - squadSize

	candidates, err := s.countryPoolCandidates(ctx, tx, worldID, countryID)
	if err != nil {
		return 0, err
	}
	if len(candidates) == 0 {
		return 0, nil
	}

	// Deterministic ordering: rotate the same-overall ties with the club seed
	// so equal-quality prospects resolve stably per (world, season, club).
	rng := rand.New(rand.NewSource(hashSeed(worldID, clubID, season)))
	ordered := orderAutoFillCandidates(candidates, squadGroupCounts(ctx, tx, clubID), need, rng)
	if len(ordered) == 0 {
		return 0, nil
	}

	signed := 0
	var playerIDs []uuid.UUID
	for _, c := range ordered {
		if signed >= need {
			break
		}
		wage := int64(c.Overall) * 150
		err := playerpool.SignFreeAgent(ctx, tx, s.bus, worldID, c.ID, clubID, wage, 1, ref)
		switch {
		case err == nil:
			signed++
			playerIDs = append(playerIDs, c.ID)
		case errors.Is(err, playerpool.ErrStreetUnder18),
			errors.Is(err, playerpool.ErrClubCannotAfford):
			continue // skip ineligible / unaffordable and keep filling
		default:
			return signed, fmt.Errorf("lifecycle autofill sign: %w", err)
		}
	}

	if signed > 0 {
		if err := s.emitAutoFill(ctx, tx, worldID, clubID, signed, playerIDs); err != nil {
			return signed, err
		}
	}
	return signed, nil
}

// activeSquadSize counts the club's active players.
func activeSquadSize(ctx context.Context, tx pgx.Tx, clubID uuid.UUID) (int, error) {
	var n int
	err := tx.QueryRow(ctx, `
		SELECT COUNT(*) FROM player.players
		WHERE club_id = $1 AND status = 'active'`, clubID).Scan(&n)
	if err != nil {
		return 0, fmt.Errorf("lifecycle squad size: %w", err)
	}
	return n, nil
}

// squadGroupCounts counts the club's active players per squad group
// (GK / DEF / MID / FWD), used to decide which template groups are empty.
func squadGroupCounts(ctx context.Context, tx pgx.Tx, clubID uuid.UUID) map[string]int {
	rows, err := tx.Query(ctx, `
		SELECT primary_position, COUNT(*) FROM player.players
		WHERE club_id = $1 AND status = 'active'
		GROUP BY primary_position`, clubID)
	if err != nil {
		return nil
	}
	defer rows.Close()

	counts := map[string]int{}
	for rows.Next() {
		var pos string
		var n int
		if err := rows.Scan(&pos, &n); err == nil {
			counts[positionGroup(pos)] += n
		}
	}
	return counts
}

// countryPoolCandidates loads the country pool's free agents with positional
// overall for the AI signing pass. uuid.Nil countryID means the whole world.
func (s *Service) countryPoolCandidates(ctx context.Context, tx pgx.Tx, worldID, countryID uuid.UUID) ([]autoFillCandidate, error) {
	rows, err := tx.Query(ctx, `
		SELECT p.id, p.primary_position,
		       COALESCE(ROUND(AVG(a.value))::int, 0) AS overall
		FROM player.players p
		LEFT JOIN player.player_attributes a ON a.player_id = p.id
		WHERE p.world_id = $1 AND p.club_id IS NULL AND p.status = 'free_agent'
		  AND ($2::uuid = '00000000-0000-0000-0000-000000000000' OR p.country_id = $2)
		GROUP BY p.id
		ORDER BY overall DESC, p.id`,
		worldID, countryID)
	if err != nil {
		return nil, fmt.Errorf("lifecycle autofill pool query: %w", err)
	}
	defer rows.Close()

	var candidates []autoFillCandidate
	for rows.Next() {
		var c autoFillCandidate
		if err := rows.Scan(&c.ID, &c.Position, &c.Overall); err != nil {
			return nil, fmt.Errorf("lifecycle autofill pool scan: %w", err)
		}
		candidates = append(candidates, c)
	}
	return candidates, rows.Err()
}

// orderAutoFillCandidates returns up to `need` candidates in signing order,
// filling squad template gaps first then topping up by overall. A gap is a
// squad group (GK/DEF/MID/FWD) with fewer active players than its template
// quota; the deepest gap is filled first, and per group the best overall
// candidate is chosen. Candidates sharing an overall are ordered by the
// seeded rng, keeping the result deterministic for a given (world, season,
// club) while giving equal prospects a stable, non-arbitrary turn order.
func orderAutoFillCandidates(candidates []autoFillCandidate, squad map[string]int, need int, rng *rand.Rand) []autoFillCandidate {
	if need <= 0 || len(candidates) == 0 || rng == nil {
		return nil
	}

	// Stable sort key: overall DESC, then per-ID rng draw for ties.
	ordered := append([]autoFillCandidate(nil), candidates...)
	sort.SliceStable(ordered, func(i, j int) bool {
		if ordered[i].Overall != ordered[j].Overall {
			return ordered[i].Overall > ordered[j].Overall
		}
		return rngKey(rng, ordered[i].ID) < rngKey(rng, ordered[j].ID)
	})

	// Remaining quota deficit per group, deepest gaps first; tied gaps are
	// ordered by a fixed precedence (GK > DEF > MID > FWD) so the signoff
	// order is fully deterministic.
	var groupRank = map[string]int{"GK": 0, "DEF": 1, "MID": 2, "FWD": 3}
	deficit := make(map[string]int, len(groupQuota))
	var order []string
	for g, quota := range groupQuota {
		d := quota - squad[g]
		if d > 0 {
			deficit[g] = d
			order = append(order, g)
		}
	}
	sort.SliceStable(order, func(i, j int) bool {
		if deficit[order[i]] != deficit[order[j]] {
			return deficit[order[i]] > deficit[order[j]]
		}
		return groupRank[order[i]] < groupRank[order[j]]
	})

	var out []autoFillCandidate
	used := make(map[uuid.UUID]bool, len(ordered))
	filled := make(map[string]int, len(order))

	// Pass 1: close each gap group's whole quota deficit, deepest gap first.
	for _, g := range order {
		if len(out) >= need {
			break
		}
		target := min(deficit[g], need-len(out))
		for _, cand := range ordered {
			if filled[g] >= target || len(out) >= need {
				break
			}
			if used[cand.ID] || positionGroup(cand.Position) != g {
				continue
			}
			used[cand.ID] = true
			filled[g]++
			out = append(out, cand)
		}
	}

	// Pass 2: best overall remaining players top up the squad.
	for _, cand := range ordered {
		if len(out) >= need {
			break
		}
		if !used[cand.ID] {
			used[cand.ID] = true
			out = append(out, cand)
		}
	}
	return out
}

// positionGroup maps a primary position to its squad group.
func positionGroup(pos string) string {
	switch pos {
	case "GK":
		return "GK"
	case "CB", "LB", "RB":
		return "DEF"
	case "DM", "CM", "AM", "LM", "RM":
		return "MID"
	default:
		return "FWD" // LW, RW, ST and any future forward role
	}
}

// rngKey returns a stable per-ID ordering value drawn from the seeded rng.
func rngKey(rng *rand.Rand, id uuid.UUID) float64 {
	return rng.Float64() + float64(id[0])/1e9
}

// emitAutoFill records the AI_AUTO_FILL event for a club's rollover signings.
func (s *Service) emitAutoFill(ctx context.Context, tx pgx.Tx, worldID, clubID uuid.UUID, signed int, playerIDs []uuid.UUID) error {
	payload, _ := json.Marshal(map[string]any{
		"club_id":      clubID,
		"signed_count": signed,
		"player_ids":   playerIDs,
	})
	actor := "system"
	return eventbus.WriteTx(ctx, s.bus, tx, &eventbus.Event{
		WorldID:   worldID,
		EventType: EventAIAutoFill,
		ActorType: &actor,
		Payload:   payload,
	})
}
