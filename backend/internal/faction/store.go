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
	"hash/fnv"
	"io"
	"sort"
	"strconv"
	"strings"
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

// memberProfiles loads the squad's behavioural attributes for generation plus
// the read-model enrichment (uniform, role, six attribute-category means) the
// dynamics wire carries. The six means use the same round-half-up arithmetic as
// internal/squad's categoryMean. Generation must NOT feed on the enrichment
// block — memberHash deliberately omits it so development-driven attribute
// drift never re-rolls the graph.
func (s *store) memberProfiles(ctx context.Context, q querier, clubID uuid.UUID) ([]MemberProfile, error) {
	rows, err := q.Query(ctx, `
		SELECT p.id::text, pe.display_name,
		       COALESCE(pp.leadership, 50), COALESCE(pp.sociability, 50),
		       COALESCE(pp.emotional_volatility, 50), COALESCE(pp.loyalty, 50),
		       COALESCE(pe.nationality_code, ''), COALESCE(pe.second_nationality_code, ''),
		       COALESCE(DATE_PART('year', AGE(pe.date_of_birth))::int, 0),
		       COALESCE(p.primary_position, ''),
		       COALESCE(p.is_academy_product, FALSE),
		       p.squad_number, COALESCE(p.status, 'active'),
		       COALESCE(cr.squad_role, 'squad_player'),
		       COALESCE(ROUND(AVG(a.value) FILTER (WHERE a.attribute_category = 'technical')), 0)::int,
		       COALESCE(ROUND(AVG(a.value) FILTER (WHERE a.attribute_category = 'physical')), 0)::int,
		       COALESCE(ROUND(AVG(a.value) FILTER (WHERE a.attribute_category = 'mental')), 0)::int,
		       COALESCE(ROUND(AVG(a.value) FILTER (WHERE a.attribute_category = 'tactical')), 0)::int,
		       COALESCE(ROUND(AVG(a.value) FILTER (WHERE a.attribute_category = 'goalkeeping')), 0)::int,
		       COALESCE(ROUND(AVG(a.value) FILTER (WHERE a.attribute_category = 'positional')), 0)::int
		FROM player.players p
		JOIN person.people pe ON pe.id = p.person_id
		LEFT JOIN player.player_personality pp ON pp.player_id = p.id
		LEFT JOIN LATERAL (
			SELECT squad_role FROM player.contracts
			WHERE player_id = p.id AND status = 'active'
			ORDER BY start_date DESC LIMIT 1
		) cr ON TRUE
		LEFT JOIN player.player_attributes a ON a.player_id = p.id
		WHERE p.club_id = $1
		GROUP BY p.id, pe.display_name, pp.leadership, pp.sociability, pp.emotional_volatility,
		         pp.loyalty, pe.nationality_code, pe.second_nationality_code, pe.date_of_birth,
		         p.primary_position, p.is_academy_product, p.squad_number, p.status, cr.squad_role
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
			&m.Age, &m.Position, &m.AcademyProduct, &m.SquadNumber, &m.Status,
			&m.SquadRole, &m.Attributes.Technical, &m.Attributes.Physical,
			&m.Attributes.Mental, &m.Attributes.Tactical, &m.Attributes.Goalkeeping,
			&m.Attributes.Positional); err != nil {
			return nil, fmt.Errorf("faction: scan profile: %w", err)
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// upsertEdges persists generated edges idempotently in one batched statement,
// keyed on the table's canonical (entity_a_id, entity_b_id, relationship_type)
// uniqueness. The previous per-edge loop paid a round-trip per bond; a dense
// squad (~1100 edges) now costs a single round-trip. sentiment mirrors strength.
func (s *store) upsertEdges(ctx context.Context, q querier, worldID uuid.UUID, edges []RelationshipEdge) error {
	if len(edges) == 0 {
		return nil
	}
	from := make([]uuid.UUID, 0, len(edges))
	to := make([]uuid.UUID, 0, len(edges))
	kinds := make([]string, 0, len(edges))
	strengths := make([]int, 0, len(edges))
	trusts := make([]int, 0, len(edges))
	for _, e := range edges {
		a, errA := uuid.Parse(e.From)
		b, errB := uuid.Parse(e.To)
		if errA != nil || errB != nil {
			return fmt.Errorf("faction: invalid edge endpoint (%s, %s): %w", e.From, e.To, errors.Join(errA, errB))
		}
		from = append(from, a)
		to = append(to, b)
		kinds = append(kinds, e.Kind)
		strengths = append(strengths, clampInt(e.Strength, -100, 100))
		trusts = append(trusts, clampInt(e.Trust, -100, 100))
	}
	_, err := q.Exec(ctx, `
		INSERT INTO social.relationships
			(world_id, entity_a_id, entity_a_type, entity_b_id, entity_b_type,
			 relationship_type, strength, trust, sentiment, last_interaction_at, created_at)
		SELECT $1, a, 'player', b, 'player', t, st, tr, st, now(), now()
		FROM unnest($2::uuid[], $3::uuid[], $4::text[], $5::int[], $6::int[]) AS x(a, b, t, st, tr)
		ON CONFLICT (entity_a_id, entity_b_id, relationship_type)
		DO UPDATE SET
			strength = EXCLUDED.strength,
			trust = EXCLUDED.trust,
			sentiment = EXCLUDED.sentiment,
			last_interaction_at = EXCLUDED.last_interaction_at`,
		worldID, from, to, kinds, strengths, trusts)
	if err != nil {
		return fmt.Errorf("faction: upsert edges: %w", err)
	}
	return nil
}

// ensureSquadRelationships generates and upserts the club's player↔player edges
// deterministically. Idempotent: once the persisted graph matches the current
// squad's fingerprint it is a no-op (no rewrite at all), which is the steady-state
// cost driver for GET /dynamics. It returns the members it saw, so callers can
// build former-teammate edges or the read model without a second query.
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
	hash := memberHash(seed, members)

	var stored int64
	err = q.QueryRow(ctx, `
		SELECT member_hash FROM social.squad_graph_state
		WHERE world_id = $1 AND club_id = $2`, worldID, clubID).Scan(&stored)
	if err == nil && stored == hash {
		return members, nil
	}
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return nil, fmt.Errorf("faction: squad graph state: %w", err)
	}
	if err := s.upsertEdges(ctx, q, worldID, GenerateEdges(seed, members)); err != nil {
		return nil, err
	}
	if _, err := q.Exec(ctx, `
		INSERT INTO social.squad_graph_state (world_id, club_id, member_hash)
		VALUES ($1, $2, $3)
		ON CONFLICT (world_id, club_id) DO UPDATE
		SET member_hash = EXCLUDED.member_hash, updated_at = now()`,
		worldID, clubID, hash); err != nil {
		return nil, fmt.Errorf("faction: record squad graph state: %w", err)
	}
	return members, nil
}

// memberHash fingerprints the exact inputs GenerateEdges consumes (world seed
// + the per-member identity/behaviour block). Membership and the generation
// fields land in the hash; read-model enrichment (attributes, uniform, role,
// name) deliberately does not, so nothing the graph doesn't depend on triggers
// a rewrite. Canonicalized by player id and rendered with value separators, so
// it is deterministic across runs and stable against reordering.
func memberHash(seed int64, members []MemberProfile) int64 {
	sorted := make([]MemberProfile, len(members))
	copy(sorted, members)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].PlayerID < sorted[j].PlayerID })

	var sb strings.Builder
	sb.WriteString(strconv.FormatInt(seed, 10))
	for _, m := range sorted {
		sb.WriteByte(0)
		sb.WriteString(m.PlayerID)
		sb.WriteByte(0)
		sb.WriteString(m.Nationality)
		sb.WriteByte(0)
		sb.WriteString(m.SecondNationality)
		sb.WriteByte(0)
		sb.WriteString(strconv.Itoa(m.Age))
		sb.WriteByte(0)
		sb.WriteString(m.Position)
		sb.WriteByte(0)
		sb.WriteString(strconv.FormatBool(m.AcademyProduct))
		sb.WriteByte(0)
		sb.WriteString(strconv.Itoa(m.Leadership))
		sb.WriteByte(0)
		sb.WriteString(strconv.Itoa(m.Sociability))
		sb.WriteByte(0)
		sb.WriteString(strconv.Itoa(m.Volatility))
	}
	h := fnv.New64a()
	_, _ = io.WriteString(h, sb.String())
	return int64(h.Sum64())
}

// squadSnapshot loads the members (from the profiles the caller already has),
// edges and CTE-derived component roots that the pure engine runs on.
func (s *store) squadSnapshot(ctx context.Context, q querier, worldID, clubID uuid.UUID, profiles []MemberProfile) (SquadSnapshot, error) {
	snap := SquadSnapshot{Members: make([]SquadMember, 0, len(profiles))}
	for _, m := range profiles {
		snap.Members = append(snap.Members, SquadMember{
			PlayerID: m.PlayerID, Leadership: m.Leadership,
			Loyalty: m.Loyalty, Volatility: m.Volatility,
		})
	}
	var err error
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
