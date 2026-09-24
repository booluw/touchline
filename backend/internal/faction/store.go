// Persistence + graph read layer for squad dynamics (S09-02). The engine math
// stays pure (engine.go/generate.go); this file owns the recursive-CTE
// connected-component read, deterministic edge generation/upsert, the
// aggregate dressing-room moods, and the SQUAD_UNREST_TRIGGERED side effect.
// Every writer takes the caller's pgx.Tx so edges, event and audit rows land
// atomically with a transfer.
package faction

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/touchline/backend/pkg/eventbus"
	"github.com/touchline/backend/pkg/explanation"
)

// EventSquadUnrestTriggered is emitted when severe contagion crosses the
// documented unrest threshold after a squad member leaves.
const EventSquadUnrestTriggered = "SQUAD_UNREST_TRIGGERED"

// querier is the read/write surface shared by *pgxpool.Pool and pgx.Tx so the
// store can run both inside a caller's transfer transaction and standalone.
type querier interface {
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
}

type store struct{}

// worldSeed returns the world's deterministic seed, falling back to a hash of
// the world id when world_seed is NULL (older worlds).
func (s *store) worldSeed(ctx context.Context, q querier, worldID uuid.UUID) (int64, error) {
	var seed *int64
	if err := q.QueryRow(ctx, `SELECT world_seed FROM world.worlds WHERE id = $1`, worldID).Scan(&seed); err != nil {
		return 0, fmt.Errorf("faction: world seed: %w", err)
	}
	if seed != nil {
		return *seed, nil
	}
	return int64(SeedStream(0, worldID.String())), nil
}

// memberProfiles loads the squad's behavioural attributes for generation.
func (s *store) memberProfiles(ctx context.Context, q querier, clubID uuid.UUID) ([]MemberProfile, error) {
	rows, err := q.Query(ctx, `
		SELECT p.id::text, pe.display_name,
		       COALESCE(pp.leadership, 50), COALESCE(pp.sociability, 50),
		       COALESCE(pp.emotional_volatility, 50), COALESCE(pp.loyalty, 50),
		       COALESCE(pe.nationality_code, ''), COALESCE(pe.second_nationality_code, ''),
		       COALESCE(DATE_PART('year', AGE(pe.date_of_birth))::int, 0),
		       COALESCE(p.primary_position, ''),
		       COALESCE(p.is_academy_product, FALSE)
		FROM player.players p
		JOIN person.people pe ON pe.id = p.person_id
		LEFT JOIN player.player_personality pp ON pp.player_id = p.id
		WHERE p.club_id = $1
		ORDER BY p.id`, clubID)
	if err != nil {
		return nil, fmt.Errorf("faction: squad profiles: %w", err)
	}
	defer rows.Close()

	var out []MemberProfile
	for rows.Next() {
		var m MemberProfile
		if err := rows.Scan(&m.PlayerID, &m.Name, &m.Leadership, &m.Sociability,
			&m.Volatility, &m.Loyalty, &m.Nationality, &m.SecondNationality,
			&m.Age, &m.Position, &m.AcademyProduct); err != nil {
			return nil, fmt.Errorf("faction: scan profile: %w", err)
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// upsertEdges persists generated edges idempotently, keyed on the table's
// canonical (entity_a_id, entity_b_id, relationship_type) uniqueness.
func (s *store) upsertEdges(ctx context.Context, q querier, worldID uuid.UUID, edges []RelationshipEdge) error {
	for _, e := range edges {
		sentiment := clampInt(e.Strength, -100, 100)
		if _, err := q.Exec(ctx, `
			INSERT INTO social.relationships
				(world_id, entity_a_id, entity_a_type, entity_b_id, entity_b_type,
				 relationship_type, strength, trust, sentiment, last_interaction_at, created_at)
			VALUES ($1, $2, 'player', $3, 'player', $4, $5, $6, $7, now(), now())
			ON CONFLICT (entity_a_id, entity_b_id, relationship_type)
			DO UPDATE SET
				strength = EXCLUDED.strength,
				trust = EXCLUDED.trust,
				sentiment = EXCLUDED.sentiment,
				last_interaction_at = EXCLUDED.last_interaction_at`,
			worldID, e.From, e.To, e.Kind, clampInt(e.Strength, -100, 100), clampInt(e.Trust, -100, 100), sentiment); err != nil {
			return fmt.Errorf("faction: upsert edge: %w", err)
		}
	}
	return nil
}

// ensureSquadRelationships generates and upserts the club's player↔player edges
// deterministically. Idempotent: re-running on the same squad is a no-op state
// change. It returns the members it saw, so callers can build former-teammate
// edges or a pre-sale contagion snapshot without a second query.
func (s *store) ensureSquadRelationships(ctx context.Context, q querier, worldID, clubID uuid.UUID) ([]MemberProfile, error) {
	seed, err := s.worldSeed(ctx, q, worldID)
	if err != nil {
		return nil, err
	}
	members, err := s.memberProfiles(ctx, q, clubID)
	if err != nil {
		return nil, err
	}
	if len(members) < 2 {
		return members, nil
	}
	if err := s.upsertEdges(ctx, q, worldID, GenerateEdges(seed, members)); err != nil {
		return nil, err
	}
	return members, nil
}

// squadSnapshot loads the members, edges and CTE-derived component roots that
// the pure engine runs on.
func (s *store) squadSnapshot(ctx context.Context, q querier, worldID, clubID uuid.UUID) (SquadSnapshot, error) {
	profiles, err := s.memberProfiles(ctx, q, clubID)
	if err != nil {
		return SquadSnapshot{}, err
	}
	snap := SquadSnapshot{Members: make([]SquadMember, 0, len(profiles))}
	for _, m := range profiles {
		snap.Members = append(snap.Members, SquadMember{
			PlayerID: m.PlayerID, Leadership: m.Leadership,
			Loyalty: m.Loyalty, Volatility: m.Volatility,
		})
	}
	snap.Edges, err = s.squadEdges(ctx, q, worldID, clubID)
	if err != nil {
		return SquadSnapshot{}, err
	}
	snap.Roots, err = s.componentRoots(ctx, q, worldID, clubID)
	if err != nil {
		return SquadSnapshot{}, err
	}
	return snap, nil
}

// squadEdges loads all player↔player relationships among the club's players.
func (s *store) squadEdges(ctx context.Context, q querier, worldID, clubID uuid.UUID) ([]RelationshipEdge, error) {
	rows, err := q.Query(ctx, `
		SELECT r.entity_a_id::text, r.entity_b_id::text, r.strength, r.trust, r.relationship_type
		FROM social.relationships r
		JOIN player.players pa ON pa.id = r.entity_a_id AND pa.club_id = $2
		JOIN player.players pb ON pb.id = r.entity_b_id AND pb.club_id = $2
		WHERE r.world_id = $1
		  AND r.entity_a_type = 'player' AND r.entity_b_type = 'player'
		ORDER BY r.entity_a_id, r.entity_b_id, r.relationship_type`, worldID, clubID)
	if err != nil {
		return nil, fmt.Errorf("faction: squad edges: %w", err)
	}
	defer rows.Close()

	var out []RelationshipEdge
	for rows.Next() {
		var e RelationshipEdge
		if err := rows.Scan(&e.From, &e.To, &e.Strength, &e.Trust, &e.Kind); err != nil {
			return nil, fmt.Errorf("faction: scan edge: %w", err)
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// componentRoots resolves each squad member's connected-component root (the
// minimum reachable player id) with an undirected recursive CTE. This is the
// authoritative "factions from the graph" read — nothing is denormalized.
func (s *store) componentRoots(ctx context.Context, q querier, worldID, clubID uuid.UUID) (map[string]string, error) {
	rows, err := q.Query(ctx, `
		WITH RECURSIVE undirected AS (
			SELECT r.entity_a_id AS a, r.entity_b_id AS b
			FROM social.relationships r
			JOIN player.players pa ON pa.id = r.entity_a_id AND pa.club_id = $2
			JOIN player.players pb ON pb.id = r.entity_b_id AND pb.club_id = $2
			WHERE r.world_id = $1 AND r.entity_a_type = 'player' AND r.entity_b_type = 'player'
			UNION
			SELECT r.entity_b_id AS a, r.entity_a_id AS b
			FROM social.relationships r
			JOIN player.players pa ON pa.id = r.entity_a_id AND pa.club_id = $2
			JOIN player.players pb ON pb.id = r.entity_b_id AND pb.club_id = $2
			WHERE r.world_id = $1 AND r.entity_a_type = 'player' AND r.entity_b_type = 'player'
		),
		reach AS (
			SELECT p.id AS start, p.id AS node
			FROM player.players p WHERE p.club_id = $2
			UNION
			SELECT r.start, u.b
			FROM reach r JOIN undirected u ON u.a = r.node
		)
		SELECT start::text, (array_agg(node ORDER BY node))[1]::text
		FROM reach
		GROUP BY start`, worldID, clubID)
	if err != nil {
		return nil, fmt.Errorf("faction: component roots: %w", err)
	}
	defer rows.Close()

	out := map[string]string{}
	for rows.Next() {
		var start, root string
		if err := rows.Scan(&start, &root); err != nil {
			return nil, fmt.Errorf("faction: scan root: %w", err)
		}
		out[start] = root
	}
	return out, rows.Err()
}

// dressingRoomMood maps mean player morale [0,1] to [0,100]. Neutral 50 when
// the club has no condition rows yet.
func (s *store) dressingRoomMood(ctx context.Context, q querier, clubID uuid.UUID) (int, error) {
	var n int
	var avg float64
	if err := q.QueryRow(ctx, `
		SELECT COUNT(*), COALESCE(AVG(pc.morale), 0)::float8
		FROM player.player_condition pc
		JOIN player.players p ON p.id = pc.player_id
		WHERE p.club_id = $1`, clubID).Scan(&n, &avg); err != nil {
		return 0, fmt.Errorf("faction: dressing room mood: %w", err)
	}
	if n == 0 {
		return 50, nil
	}
	return clampInt(int(avg*100+0.5), 0, 100), nil
}

// managerSupport maps mean player→manager sentiment [-100,100] to [0,100].
// Neutral 50 when no player has an opinion about the manager yet.
func (s *store) managerSupport(ctx context.Context, q querier, worldID, clubID, managerID uuid.UUID) (int, error) {
	var n int
	var avg float64
	if err := q.QueryRow(ctx, `
		SELECT COUNT(*), COALESCE(AVG(r.sentiment), 0)::float8
		FROM social.relationships r
		WHERE r.world_id = $1
		  AND r.entity_a_type = 'player' AND r.entity_b_type = 'manager'
		  AND r.entity_b_id = $3
		  AND r.entity_a_id IN (SELECT id FROM player.players WHERE club_id = $2)`,
		worldID, clubID, managerID).Scan(&n, &avg); err != nil {
		return 0, fmt.Errorf("faction: manager support: %w", err)
	}
	if n == 0 {
		return 50, nil
	}
	return clampInt(int((avg+100)/2+0.5), 0, 100), nil
}

// recentUnrest returns the latest SQUAD_UNREST_TRIGGERED for the club inside
// the documented 14-day window, if any.
func (s *store) recentUnrest(ctx context.Context, q querier, worldID, clubID uuid.UUID) (*Unrest, error) {
	var payload []byte
	err := q.QueryRow(ctx, `
		SELECT payload FROM world.events
		WHERE world_id = $1 AND event_type = $2
		  AND payload->>'club_id' = $3
		  AND occurred_at > now() - interval '14 days'
		ORDER BY occurred_at DESC LIMIT 1`,
		worldID, EventSquadUnrestTriggered, clubID.String()).Scan(&payload)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("faction: recent unrest: %w", err)
	}
	var p struct {
		PlayerID string   `json:"player_id"`
		Severity int      `json:"severity"`
		Demand   string   `json:"demand"`
		Affected []string `json:"affected"`
	}
	if err := json.Unmarshal(payload, &p); err != nil {
		return nil, fmt.Errorf("faction: decode unrest: %w", err)
	}
	return &Unrest{
		Severity: p.Severity,
		Demand:   p.Demand,
		Affected: p.Affected,
		Explanation: explanation.New("squad_unrest", p.Severity).
			Add("affected players", len(p.Affected)).
			Add("severity", p.Severity),
	}, nil
}

// emitUnrest writes the SQUAD_UNREST_TRIGGERED event through the transfer
// transaction via the outbox.
func (s *store) emitUnrest(ctx context.Context, q querier, pub eventbus.Publisher, worldID uuid.UUID,
	tick int64, clubID, playerID uuid.UUID, u *Unrest,
) error {
	tx, ok := q.(pgx.Tx)
	if !ok {
		return fmt.Errorf("faction: unrest emit requires a transaction")
	}
	payload, err := json.Marshal(map[string]any{
		"club_id":   clubID.String(),
		"player_id": playerID.String(),
		"severity":  u.Severity,
		"demand":    u.Demand,
		"affected":  u.Affected,
	})
	if err != nil {
		return err
	}
	exp, err := json.Marshal(u.Explanation)
	if err != nil {
		return err
	}
	actorType := "system"
	e := eventbus.Event{
		ID:          uuid.New(),
		WorldID:     worldID,
		WorldTick:   tick,
		EventType:   EventSquadUnrestTriggered,
		ActorType:   &actorType,
		Payload:     payload,
		Explanation: exp,
		OccurredAt:  time.Now().UTC(),
	}
	return eventbus.WriteTx(ctx, pub, tx, &e)
}

// sortedTiers renders the tier map as a deterministic, name-bearing slice for
// API responses and explanations.
func sortedTiers(tiers map[string]Tier, names map[string]string) []TierView {
	out := make([]TierView, 0, len(tiers))
	for id, tier := range tiers {
		out = append(out, TierView{PlayerID: id, Name: names[id], Tier: tier, Player: playerRef(id, names)})
	}
	sort.Slice(out, func(i, j int) bool {
		if tierRank(out[i].Tier) != tierRank(out[j].Tier) {
			return tierRank(out[i].Tier) < tierRank(out[j].Tier)
		}
		return out[i].PlayerID < out[j].PlayerID
	})
	return out
}

func tierRank(t Tier) int {
	switch t {
	case TierTeamLeader:
		return 0
	case TierHighlyInfluential:
		return 1
	case TierInfluential:
		return 2
	default:
		return 3
	}
}
