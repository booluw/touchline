// The persisted-side of the squad package (matchsim addendum v1.4 Part 8):
// internal/squad reads the REAL player.player_attributes EAV, hidden traits,
// personality, and active player_emotional_states rows into the pure
// aggregation structs. The package stays split along the same line as
// internal/form — pure mechanics in squad.go/rng.go/scouting.go/ratings.go,
// persistence here.
package squad

import (
	"context"
	"errors"
	"fmt"
	"math"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// LoadedPlayer is the persisted view team selection reasons about before a
// spot is fixed. `Attributes` is the category roll-up of the defensive EAV
// (the same integer mean pkg/playergen/attributes.go defines with
// CategoryAverage); `CurrentSentiment` is derived from the player's active
// emotional states (recency-weighted, signed).
type LoadedPlayer struct {
	PlayerID uuid.UUID
	Position string
	Status   string

	// Available reports whether the player may start on `onDate`: club-held,
	// not retired/suspended/on-loan status, and no open injury whose
	// expected_recovery_date still covers the matchday.
	Available bool

	Leadership int
	Hidden     PlayerHiddenTraitsSnapshot
	Attributes AttributeSnapshot

	// Penalties is the raw 'penalties' technical key (designated-taker
	// tie-break, Part 7 §3). 0 when the player has no such key (a GK).
	Penalties int

	// CurrentSentiment is signed: magnitude ≈ emotional intensity, sign =
	// valence (see SentimentFromStates).
	CurrentSentiment int
}

// EmotionalStateRef is one ACTIVE emotional-state row as sentiment derivation
// consumes it.
type EmotionalStateRef struct {
	State     string
	Intensity int
}

// StatePolarity is the versioned valence table for sentiment derivation: a
// dominant player sentiment contributes proportionally to that magnitude; the
// mild positive never reads as aimless, the extreme negatives pull hardest
// (PM "Recent Emotional States" — normalised, not summed).
var StatePolarity = map[string]float64{
	"happy": 0.7, "content": 0.15, "motivated": 0.6, "excited": 0.8,
	"confident": 0.7, "ambitious": 0.5,
	"frustrated": -0.6, "anxious": -0.7, "angry": -0.8, "homesick": -0.5,
	"betrayed": -0.9, "isolated": -0.6,
}

// SentimentFromStates aggregates a player's ACTIVE emotional states into the
// signed CurrentSentiment in [-100, 100]. Recency-weighted per PM "Recent
// Emotional States": the newest state weighs 1.0, each older one half of its
// predecessor (0.5^n); each state contributes polarity × intensity/100 and
// the result is normalised by the weight sum so one strong state still reads
// strongly. Unknown states deflate to a neutral small-positive (data change
// only, never a code path).
func SentimentFromStates(states []EmotionalStateRef) int {
	var num, den float64
	w := 1.0
	for _, st := range states {
		polar := StatePolarity[st.State]
		if polar == 0 {
			polar = StatePolarity["content"]
		}
		num += polar * float64(st.Intensity) * w
		den += w
		w *= 0.5
	}
	if den == 0 {
		return 0
	}
	return clampSentiment(int(math.Round(num / den)))
}

// Store loads persisted club/player data into the pure squad structs.
type Store struct {
	pool *pgxpool.Pool
}

// NewStore builds the squad data store.
func NewStore(pool *pgxpool.Pool) *Store {
	return &Store{pool: pool}
}

// LoadSquad loads a club's FIRST-TEAM eligible players joined with their
// hidden traits, leadership, penalty attribute, and recency-weighted
// sentiment. onDate is the fixture's matchday; availability and open-injury
// windows are evaluated against it so a resimulated matchday re-reads the same
// availability.
func (s *Store) LoadSquad(ctx context.Context, clubID uuid.UUID, onDate time.Time) ([]LoadedPlayer, error) {
	base, err := s.loadPlayerRows(ctx, clubID, onDate)
	if err != nil {
		return nil, err
	}
	if len(base) == 0 {
		return nil, nil
	}

	rows, err := s.pool.Query(ctx, `
		SELECT p.id, es.emotional_state, es.intensity
		FROM player.player_emotional_states es
		JOIN player.players p ON p.id = es.player_id AND p.club_id = $1
		WHERE es.expires_at IS NULL OR es.expires_at > now()
		ORDER BY p.id, es.occurred_at DESC`, clubID)
	if err != nil {
		return nil, fmt.Errorf("load emotional states: %w", err)
	}
	emot := make(map[uuid.UUID][]EmotionalStateRef)
	for rows.Next() {
		var pid uuid.UUID
		var st EmotionalStateRef
		if err := rows.Scan(&pid, &st.State, &st.Intensity); err != nil {
			rows.Close()
			return nil, fmt.Errorf("scan emotional state: %w", err)
		}
		emot[pid] = append(emot[pid], st)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate emotional states: %w", err)
	}

	for i := range base {
		base[i].CurrentSentiment = SentimentFromStates(emot[base[i].PlayerID])
	}
	return base, nil
}

// loadPlayerRows loads the players + hidden traits + leadership + availability
// in one pass, leaving sentiment to the caller.
func (s *Store) loadPlayerRows(ctx context.Context, clubID uuid.UUID, onDate time.Time) ([]LoadedPlayer, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT p.id, p.primary_position, p.status,
		       COALESCE(h.consistency, 50), COALESCE(h.temperament, 50),
		       COALESCE(h.pressure_handling, 50), COALESCE(h.professionalism, 50),
		       COALESCE(h.adaptability, 50), COALESCE(ps.leadership, 50),
		       CASE WHEN EXISTS (
		            SELECT 1 FROM player.injuries i
		            WHERE i.player_id = p.id
		              AND i.actual_recovery_date IS NULL
		              AND i.expected_recovery_date >= $2::date)
		       THEN TRUE ELSE FALSE END
		FROM player.players p
		LEFT JOIN player.player_hidden_traits h ON h.player_id = p.id
		LEFT JOIN player.player_personality  ps ON ps.player_id = p.id
		WHERE p.club_id = $1
		ORDER BY p.id`, clubID, onDate)
	if err != nil {
		return nil, fmt.Errorf("load players: %w", err)
	}
	defer rows.Close()

	var out []LoadedPlayer
	for rows.Next() {
		var (
			p                                   LoadedPlayer
			open                                bool
			cons, temp, pres, prof, adapt, lead int
		)
		if err := rows.Scan(&p.PlayerID, &p.Position, &p.Status,
			&cons, &temp, &pres, &prof, &adapt, &lead, &open); err != nil {
			return nil, fmt.Errorf("scan player: %w", err)
		}
		p.Available = p.Status == "active" && !open
		p.Leadership = lead
		p.Hidden = PlayerHiddenTraitsSnapshot{
			Consistency: cons, Temperament: temp, PressureHandling: pres,
			Professionalism: prof, Adaptability: adapt,
		}
		out = append(out, p)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate players: %w", err)
	}
	return out, s.attachAttributes(ctx, clubID, out)
}

// attachAttributes rolls the attribute EAV up into AttributeSnapshot per
// player (integer category mean, round-half-up — the same arithmetic as
// playergen.CategoryAverage) and lifts the raw penalties key for taker
// designation.
func (s *Store) attachAttributes(ctx context.Context, clubID uuid.UUID, players []LoadedPlayer) error {
	rows, err := s.pool.Query(ctx, `
		SELECT a.player_id, a.attribute_category, a.attribute_key, a.value
		FROM player.player_attributes a
		JOIN player.players p ON p.id = a.player_id AND p.club_id = $1
		ORDER BY a.player_id`, clubID)
	if err != nil {
		return fmt.Errorf("load attributes: %w", err)
	}
	defer rows.Close()

	acc := make(map[uuid.UUID]map[string][]int)
	for rows.Next() {
		var (
			pid uuid.UUID
			cat string
			key string
			val int
		)
		if err := rows.Scan(&pid, &cat, &key, &val); err != nil {
			return fmt.Errorf("scan attribute: %w", err)
		}
		if acc[pid] == nil {
			acc[pid] = make(map[string][]int)
		}
		acc[pid][cat] = append(acc[pid][cat], val)
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate attributes: %w", err)
	}

	for i := range players {
		byCat := acc[players[i].PlayerID]
		snap := AttributeSnapshot{
			Technical:   categoryMean(byCat["technical"]),
			Physical:    categoryMean(byCat["physical"]),
			Mental:      categoryMean(byCat["mental"]),
			Tactical:    categoryMean(byCat["tactical"]),
			Goalkeeping: categoryMean(byCat["goalkeeping"]),
			Positional:  categoryMean(byCat["positional"]),
		}
		players[i].Attributes = snap
	}
	// Penalties needs the row-level key, not the roll-up: lift it in a second
	// cheap pass.
	pRows, err := s.pool.Query(ctx, `
		SELECT a.player_id, a.value
		FROM player.player_attributes a
		JOIN player.players p ON p.id = a.player_id AND p.club_id = $1
		WHERE a.attribute_key = 'penalties'`, clubID)
	if err != nil {
		return fmt.Errorf("load penalties: %w", err)
	}
	defer pRows.Close()
	pen := make(map[uuid.UUID]int)
	for pRows.Next() {
		var pid uuid.UUID
		var v int
		if err := pRows.Scan(&pid, &v); err != nil {
			return fmt.Errorf("scan penalties: %w", err)
		}
		pen[pid] = v
	}
	if err := pRows.Err(); err != nil {
		return fmt.Errorf("iterate penalties: %w", err)
	}
	for i := range players {
		players[i].Penalties = pen[players[i].PlayerID]
	}
	return nil
}

func categoryMean(vals []int) int {
	if len(vals) == 0 {
		return 0
	}
	var sum int
	for _, v := range vals {
		sum += v
	}
	return int(math.Round(float64(sum) / float64(len(vals))))
}

// LoadClubDNA loads the competitiveness slice of club.dna.
func (s *Store) LoadClubDNA(ctx context.Context, clubID uuid.UUID) (ClubDNAInput, error) {
	var d ClubDNAInput
	err := s.pool.QueryRow(ctx,
		`SELECT club_id, competitive_ambition FROM club.club_dna WHERE club_id = $1`, clubID).
		Scan(&d.ClubID, &d.CompetitiveAmbition)
	if errors.Is(err, pgx.ErrNoRows) {
		// no DNA row — a relaxed, coasting neutral as documented default.
		return ClubDNAInput{ClubID: clubID, CompetitiveAmbition: 50}, nil
	}
	if err != nil {
		return ClubDNAInput{}, fmt.Errorf("load club dna: %w", err)
	}
	return d, nil
}

// RivalryIntensityThreshold is the intensity at which a rivalry link counts
// as a derby (docs/design/derby-rivalry-determination.md).
const RivalryIntensityThreshold = 60

// LoadRivalryIntensity returns the maximum one-direction intensity linking
// the two clubs, or 0 when none exists.
func (s *Store) LoadRivalryIntensity(ctx context.Context, home, away uuid.UUID) (int, error) {
	var intensity int
	err := s.pool.QueryRow(ctx, `
		SELECT intensity FROM club.rivalries
		WHERE (club_a_id = $1 AND club_b_id = $2)
		   OR (club_a_id = $2 AND club_b_id = $1)
		ORDER BY intensity DESC
		LIMIT 1`, home, away).Scan(&intensity)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, nil
	}
	if err != nil {
		return 0, fmt.Errorf("load rivalry intensity: %w", err)
	}
	return intensity, nil
}

// ClubRow is the persisted club view the orchestration service consumes.
type ClubRow struct {
	ID             uuid.UUID
	Name           string
	Reputation     int
	Tier           int
	IsAIControlled bool
}

// LoadClub loads a club's identity, reputation and control flag. Reputation is
// deliberately read from club.clubs (not a squad proxy) — the giant-killing
// "significantly more reputable opponent" test (Part 5 §2) needs the club
// standing, and the squad proxy would make upsets gate on nothing.
func (s *Store) LoadClub(ctx context.Context, clubID uuid.UUID) (ClubRow, error) {
	var c ClubRow
	err := s.pool.QueryRow(ctx, `
		SELECT id, name, reputation, tier, is_ai_controlled
		FROM club.clubs WHERE id = $1`, clubID).
		Scan(&c.ID, &c.Name, &c.Reputation, &c.Tier, &c.IsAIControlled)
	if err != nil {
		return ClubRow{}, fmt.Errorf("load club: %w", err)
	}
	return c, nil
}

// LoadLineup loads a user-managed club's preferred XI (slot → player).
func (s *Store) LoadLineup(ctx context.Context, clubID uuid.UUID) (map[int]uuid.UUID, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT slot, player_id FROM club.club_lineups WHERE club_id = $1 ORDER BY slot`, clubID)
	if err != nil {
		return nil, fmt.Errorf("load lineup: %w", err)
	}
	defer rows.Close()
	out := make(map[int]uuid.UUID)
	for rows.Next() {
		var slot int
		var pid uuid.UUID
		if err := rows.Scan(&slot, &pid); err != nil {
			return nil, fmt.Errorf("scan lineup: %w", err)
		}
		out[slot] = pid
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate lineup: %w", err)
	}
	return out, nil
}

// TacticsRow is a club's saved Simple-Mode setup (club.club_tactics). An empty
// Style/Formation means "never set" — ResolveTactics renders the row into a
// playable style key and slot order with balanced/4-3-3 defaults. UpdatedAt is
// the last write time, nil when never written.
type TacticsRow struct {
	Style     string
	Formation string
	UpdatedAt *time.Time
}

// LoadTactics reads a club's saved tactics row (if any). A missing row is not
// an error: the club plays default balanced/4-3-3.
func (s *Store) LoadTactics(ctx context.Context, clubID uuid.UUID) (TacticsRow, error) {
	var t TacticsRow
	var updatedAt *time.Time
	err := s.pool.QueryRow(ctx,
		`SELECT style, formation, updated_at FROM club.club_tactics WHERE club_id = $1`, clubID).
		Scan(&t.Style, &t.Formation, &updatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return TacticsRow{}, nil
	}
	if err != nil {
		return TacticsRow{}, fmt.Errorf("load tactics: %w", err)
	}
	t.UpdatedAt = updatedAt
	return t, nil
}

// PlayerCondition is one XI member's match-condition dims
// (player.player_condition; S05-01). All values in [0,1].
type PlayerCondition struct {
	PlayerID            uuid.UUID
	Fatigue             float64
	Fitness             float64
	Sharpness           float64
	InjuryRisk          float64
	TacticalFamiliarity float64
}

// LoadConditions reads condition rows for a set of players. Players without a
// row are simply absent from the map (treated as neutral at matchday).
func (s *Store) LoadConditions(ctx context.Context, playerIDs []uuid.UUID) (map[uuid.UUID]PlayerCondition, error) {
	out := make(map[uuid.UUID]PlayerCondition, len(playerIDs))
	if len(playerIDs) == 0 {
		return out, nil
	}
	rows, err := s.pool.Query(ctx, `
		SELECT player_id, fatigue, fitness, sharpness, injury_risk, tactical_familiarity
		FROM player.player_condition
		WHERE player_id = ANY($1)`, playerIDs)
	if err != nil {
		return nil, fmt.Errorf("load conditions: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var c PlayerCondition
		if err := rows.Scan(&c.PlayerID, &c.Fatigue, &c.Fitness, &c.Sharpness,
			&c.InjuryRisk, &c.TacticalFamiliarity); err != nil {
			return nil, fmt.Errorf("scan condition: %w", err)
		}
		out[c.PlayerID] = c
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate conditions: %w", err)
	}
	return out, nil
}

// ConditionFactor folds a player's sharpness/fatigue into their matchday
// contribution (docs/design/tactics-training-numerics.md §2.4):
// (0.9 + 0.4 × sharpness) × (1 − 0.3 × fatigue). A missing row (no Condition
// supplied) is neutral 1.0.
func ConditionFactor(c PlayerCondition) float64 {
	return (0.9 + 0.4*clamp01(c.Sharpness)) * (1 - 0.3*clamp01(c.Fatigue))
}

// XIFitness is the XI's mean fitness — the engine's Team.Fitness seed. Fallback
// 1.0 when no condition rows exist yet.
func XIFitness(conds map[uuid.UUID]PlayerCondition, xi []SquadMember) float64 {
	var sum float64
	n := 0
	for _, m := range xi {
		if c, ok := conds[m.PlayerID]; ok {
			sum += c.Fitness
			n++
		}
	}
	if n == 0 {
		return 1
	}
	return clamp01(sum / float64(n))
}

func clamp01(f float64) float64 {
	if f < 0 {
		return 0
	}
	if f > 1 {
		return 1
	}
	return f
}

// PlayerIDs extracts the id list of an XI for condition loading.
func PlayerIDs(xi []SquadMember) []uuid.UUID {
	out := make([]uuid.UUID, 0, len(xi))
	for _, m := range xi {
		out = append(out, m.PlayerID)
	}
	return out
}
