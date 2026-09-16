package dashboard

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/touchline/backend/pkg/eventbus"
	"github.com/touchline/backend/pkg/realtime"
)

// Service aggregates the read-only dashboard feed and owns the realtime
// `dashboard_update` push. Postgres is authoritative; the push is best-effort
// and the client dedupes on stable item IDs, so a skipped push just means the
// client sees the update on the next GET.
type Service struct {
	store  *Store
	pool   *pgxpool.Pool
	broker realtime.Broker

	mu        sync.Mutex
	pushedIDs map[string]map[string]bool // feedID -> pushed item ID set
}

// NewService builds the aggregator. The bus is accepted for future
// publish/subscribe hooks (the realtime push itself goes through the broker
// set by WithRealtime).
func NewService(pool *pgxpool.Pool, _ eventbus.EventBus) *Service {
	return &Service{
		store:     NewStore(pool),
		pool:      pool,
		pushedIDs: make(map[string]map[string]bool),
	}
}

// WithRealtime attaches the broker used to publish dashboard_update events into
// the world's socket feed. Without it (tests, read-only pods) the GET path
// still works; pushes are no-ops.
func (s *Service) WithRealtime(broker realtime.Broker) *Service {
	s.broker = broker
	return s
}

// GetDashboard assembles the current urgent / important / interesting feed for
// a manager in a world. A manager with no active club gets empty sections (the
// response shape is still valid).
func (s *Service) GetDashboard(ctx context.Context, worldID, managerID uuid.UUID) (Snapshot, error) {
	clubs, err := s.store.ManagerClubs(ctx, worldID, managerID)
	if err != nil {
		return Snapshot{}, err
	}
	return s.build(ctx, worldID, managerID, clubs)
}

// build queries every feed and partitions the items into the three sections.
// Each query is best-effort: a transient failure in one source (e.g. a
// standings read during season rollover) degrades that section instead of
// failing the whole dashboard.
func (s *Service) build(ctx context.Context, worldID, managerID uuid.UUID, clubs []uuid.UUID) (Snapshot, error) {
	snap := Snapshot{
		Urgent:      []Item{},
		Important:   []Item{},
		Interesting: []Item{},
	}

	urgent, err := s.buildUrgent(ctx, managerID, clubs)
	if err != nil {
		return snap, err
	}
	snap.Urgent = urgent

	important, err := s.buildImportant(ctx, managerID, clubs)
	if err != nil {
		return snap, err
	}
	snap.Important = important

	interesting, err := s.buildInteresting(ctx, worldID, clubs)
	if err != nil {
		return snap, err
	}
	snap.Interesting = interesting

	return snap, nil
}

// buildUrgent = open bids, expiring contracts, imminent fixtures, board
// confidence at/below the critical line, unresolved crisis.
func (s *Service) buildUrgent(ctx context.Context, managerID uuid.UUID, clubs []uuid.UUID) ([]Item, error) {
	out := make([]Item, 0, 8)

	bids, err := s.store.PendingBids(ctx, clubs)
	if err != nil {
		log.Printf("dashboard: pending bids: %v", err)
	}
	for _, b := range bids {
		out = append(out, Item{
			ID:       "bids:" + b.BidID.String(),
			Priority: PriorityUrgent,
			Category: CatBids,
			Title:    fmt.Sprintf("Bid on %s for %s", b.PlayerName, money(b.Fee)),
			Description: fmt.Sprintf("%s want your player %s (%s offer, round %d).",
				b.BiddingClubName, b.PlayerName, money(b.Fee), b.Round),
			CreatedAt: b.CreatedAt,
			Action: &Action{
				Kind:     ActionRespondBid,
				BidID:    ptr(b.BidID),
				ClubID:   ptr(b.SellingClubID),
				PlayerID: ptr(b.PlayerID),
			},
		})
	}

	contracts, err := s.store.ExpiringContracts(ctx, clubs, ContractExpiryWindowDays)
	if err != nil {
		log.Printf("dashboard: expiring contracts: %v", err)
	}
	for _, c := range contracts {
		out = append(out, Item{
			ID:          "contracts:" + c.PlayerID.String(),
			Priority:    PriorityUrgent,
			Category:    CatContracts,
			Title:       fmt.Sprintf("%s's contract expires %s", c.PlayerName, c.EndDate.Format("2006-01-02")),
			Description: "Agree a renewal or the player talks to other clubs when the window opens.",
			CreatedAt:   c.EndDate,
			Action: &Action{
				Kind:     ActionRenewContract,
				PlayerID: ptr(c.PlayerID),
				ClubID:   ptr(c.ClubID),
			},
		})
	}

	fixtures, err := s.store.NextUrgentFixtures(ctx, clubs, FixtureUrgencyWindow)
	if err != nil {
		log.Printf("dashboard: next fixtures: %v", err)
	}
	for _, f := range fixtures {
		out = append(out, Item{
			ID:       "match:" + f.FixtureID.String(),
			Priority: PriorityUrgent,
			Category: CatMatch,
			Title:    fmt.Sprintf("%s vs %s", f.HomeName, f.AwayName),
			Description: fmt.Sprintf("Kickoff %s — pick a lineup and tactics before it's too late.",
				f.ScheduledAt.Local().Format("Mon 15:04")),
			CreatedAt: f.ScheduledAt,
			Action: &Action{
				Kind:      ActionSetLineup,
				ClubID:    ptr(manageClub(f.HomeClubID, f.AwayClubID, clubs)),
				FixtureID: ptr(f.FixtureID),
			},
		})
	}

	snaps, err := s.store.BoardSnapshots(ctx, managerID)
	if err != nil {
		log.Printf("dashboard: board snapshots: %v", err)
	}
	for _, t := range snaps {
		if t.TotalScore <= BoardCriticalTotal {
			out = append(out, Item{
				ID:       "board:" + t.ClubID.String(),
				Priority: PriorityUrgent,
				Category: CatBoard,
				Title:    "Board confidence is critically low",
				Description: fmt.Sprintf("Confidence at %d/100 — you are close to being sacked.",
					t.TotalScore),
				CreatedAt: t.CreatedAt,
				Action: &Action{
					Kind:   ActionViewBoard,
					ClubID: ptr(t.ClubID),
				},
			})
		}
	}

	crises, err := s.store.UnresolvedCrises(ctx, clubs)
	if err != nil {
		log.Printf("dashboard: unresolved crises: %v", err)
	}
	for _, c := range crises {
		out = append(out, Item{
			ID:          "finance:" + c.ClubID.String(),
			Priority:    PriorityUrgent,
			Category:    CatFinance,
			Title:       fmt.Sprintf("%s is in financial %s", c.ClubName, crisisLabel(c.Stage)),
			Description: "The club's finances are under active management — review before it escalates.",
			CreatedAt:   c.StartedAt,
			Action: &Action{
				Kind:   ActionViewFinances,
				ClubID: ptr(c.ClubID),
			},
		})
	}

	return limitItems(out, MaxPerSection), nil
}

// buildImportant = sharp board-confidence drops, negative cash, unhappy players.
func (s *Service) buildImportant(ctx context.Context, managerID uuid.UUID, clubs []uuid.UUID) ([]Item, error) {
	out := make([]Item, 0, 8)

	snaps, err := s.store.BoardSnapshots(ctx, managerID)
	if err != nil {
		log.Printf("dashboard: board snapshots: %v", err)
	}
	// Snapshots come back newest-first per club (capped two per club). A recent
	// week-over-week drop below the attention line deserves a nudge.
	for i, t := range snaps {
		if i+1 >= len(snaps) || snaps[i+1].ClubID != t.ClubID {
			continue
		}
		prev := snaps[i+1]
		if delta := t.TotalScore - prev.TotalScore; delta <= BoardDropAttention {
			out = append(out, Item{
				ID:       "board-drop:" + t.ClubID.String(),
				Priority: PriorityImportant,
				Category: CatBoard,
				Title:    "Board confidence is slipping",
				Description: fmt.Sprintf("Confidence fell %d points to %d/100 over the last review.",
					-delta, t.TotalScore),
				CreatedAt: t.CreatedAt,
				Action: &Action{
					Kind:   ActionViewBoard,
					ClubID: ptr(t.ClubID),
				},
			})
		}
	}

	cash, err := s.store.CashBalances(ctx, clubs)
	if err != nil {
		log.Printf("dashboard: cash balances: %v", err)
	}
	for _, c := range cash {
		if c.Cash < 0 {
			out = append(out, Item{
				ID:          "cash:" + c.ClubID.String(),
				Priority:    PriorityImportant,
				Category:    CatFinance,
				Title:       fmt.Sprintf("%s is overdrawn by %s", c.ClubName, money(-c.Cash)),
				Description: "Negative balance — transfer departures or revenue are needed.",
				CreatedAt:   time.Now().UTC(),
				Action: &Action{
					Kind:   ActionViewFinances,
					ClubID: ptr(c.ClubID),
				},
			})
		}
	}

	unhappy, err := s.store.UnhappyPlayers(ctx, clubs, MoraleUnhappyThreshold)
	if err != nil {
		log.Printf("dashboard: unhappy players: %v", err)
	}
	for _, p := range unhappy {
		title := fmt.Sprintf("%s is unhappy", p.PlayerName)
		desc := fmt.Sprintf("Morale %.0f%%.", p.Morale*100)
		if p.HasTransferTr {
			title = fmt.Sprintf("%s has submitted a transfer request", p.PlayerName)
			desc = "They want out — respond to the request or try to reassure them."
		}
		out = append(out, Item{
			ID:          "morale:" + p.PlayerID.String(),
			Priority:    PriorityImportant,
			Category:    CatMorale,
			Title:       title,
			Description: desc,
			CreatedAt:   time.Now().UTC(),
			Action: &Action{
				Kind:     ActionViewPlayer,
				PlayerID: ptr(p.PlayerID),
			},
		})
	}

	return limitItems(out, MaxPerSection), nil
}

// buildInteresting = league table position, recent rival results, market news.
func (s *Service) buildInteresting(ctx context.Context, worldID uuid.UUID, clubs []uuid.UUID) ([]Item, error) {
	out := make([]Item, 0, 8)

	spots, err := s.store.StandingsPositions(ctx, clubs)
	if err != nil {
		log.Printf("dashboard: standings: %v", err)
	}
	for _, st := range spots {
		out = append(out, Item{
			ID:          "standings:" + st.ClubID.String(),
			Priority:    PriorityInteresting,
			Category:    CatStandings,
			Title:       fmt.Sprintf("%s sit %s in the %s", st.ClubName, ordinal(st.Position), st.SeasonLabel),
			Description: fmt.Sprintf("%d played, %d points.", st.Played, st.Points),
			CreatedAt:   time.Now().UTC(),
			Action: &Action{
				Kind:   ActionViewStandings,
				ClubID: ptr(st.ClubID),
			},
		})
	}

	results, err := s.store.RivalResults(ctx, worldID, clubs, 5)
	if err != nil {
		log.Printf("dashboard: rival results: %v", err)
	}
	for _, r := range results {
		out = append(out, Item{
			ID:          "rivals:" + r.FixtureID.String(),
			Priority:    PriorityInteresting,
			Category:    CatRivals,
			Title:       fmt.Sprintf("%s vs %s — %s", r.HomeName, r.AwayName, finalScore(r.HomeScore, r.AwayScore)),
			Description: "A rival fixture went to the book.",
			CreatedAt:   r.CompletedAt,
			Action: &Action{
				Kind:      ActionViewFixture,
				FixtureID: ptr(r.FixtureID),
			},
		})
	}

	market, err := s.store.RecentMarketEvents(ctx, worldID, MarketNewest)
	if err != nil {
		log.Printf("dashboard: market events: %v", err)
	}
	for _, m := range market {
		switch m.EventType {
		case "PLAYER_LISTED":
			out = append(out, Item{
				ID:          "market:" + m.EventID.String(),
				Priority:    PriorityInteresting,
				Category:    CatMarket,
				Title:       fmt.Sprintf("%s is on the market", m.PlayerName),
				Description: fmt.Sprintf("%s listed %s.", m.ClubName, m.PlayerName),
				CreatedAt:   m.OccurredAt,
				Action: &Action{
					Kind:     ActionViewPlayer,
					PlayerID: ptr(m.PlayerID),
				},
			})
		case "PLAYER_LISTING_WITHDRAWN":
			out = append(out, Item{
				ID:          "market:" + m.EventID.String(),
				Priority:    PriorityInteresting,
				Category:    CatMarket,
				Title:       fmt.Sprintf("%s was withdrawn from the market", m.PlayerName),
				Description: fmt.Sprintf("%s pulled the listing.", m.ClubName),
				CreatedAt:   m.OccurredAt,
				Action: &Action{
					Kind:     ActionViewPlayer,
					PlayerID: ptr(m.PlayerID),
				},
			})
		case "TRANSFER_COMPLETED":
			out = append(out, Item{
				ID:          "market:" + m.EventID.String(),
				Priority:    PriorityInteresting,
				Category:    CatMarket,
				Title:       fmt.Sprintf("%s completed for %s", m.PlayerName, moneyPtr(m.Fee)),
				Description: fmt.Sprintf("%s completed the signing of %s.", m.ClubName, m.PlayerName),
				CreatedAt:   m.OccurredAt,
				Action: &Action{
					Kind:     ActionViewPlayer,
					PlayerID: ptr(m.PlayerID),
				},
			})
		}
	}

	return limitItems(out, MaxPerSection), nil
}

// ManagerForClub resolves the active manager of a club within a world. Used by
// the bid-event hook to route an urgent push to the selling club's manager.
func (s *Service) ManagerForClub(ctx context.Context, worldID, clubID uuid.UUID) (uuid.UUID, error) {
	return s.store.ManagerForClub(ctx, worldID, clubID)
}

// PushCategory publishes a section's freshly rebuilt items to a single
// manager's socket feed, only pushing feed items whose IDs are new since the
// last push (dedupe on the client is the same key). Used by the bid-event hook
// to surface an urgent item the moment a bid lands.
func (s *Service) PushCategory(ctx context.Context, worldID, managerID uuid.UUID, section Priority) error {
	if s.broker == nil {
		return nil
	}
	clubs, err := s.store.ManagerClubs(ctx, worldID, managerID)
	if err != nil {
		return err
	}
	var items []Item
	switch section {
	case PriorityUrgent:
		items, err = s.buildUrgent(ctx, managerID, clubs)
	case PriorityImportant:
		items, err = s.buildImportant(ctx, managerID, clubs)
	default:
		items, err = s.buildInteresting(ctx, worldID, clubs)
	}
	if err != nil {
		return err
	}
	return s.push(ctx, worldID, managerID, section, items)
}

// PushWorldDelta re-snapshots every managed club in the world and pushes the
// changed sections to the affected managers. Called from the world-tick worker
// after the daily/weekly/monthly passes.
func (s *Service) PushWorldDelta(ctx context.Context, worldID uuid.UUID) error {
	if s.broker == nil {
		return nil
	}
	managers, err := s.managersInWorld(ctx, worldID)
	if err != nil {
		return err
	}
	for _, managerID := range managers {
		if err := s.PushCategory(ctx, worldID, managerID, PriorityUrgent); err != nil {
			log.Printf("dashboard push urgent %s: %v", managerID, err)
		}
		if err := s.PushCategory(ctx, worldID, managerID, PriorityImportant); err != nil {
			log.Printf("dashboard push important %s: %v", managerID, err)
		}
		if err := s.PushCategory(ctx, worldID, managerID, PriorityInteresting); err != nil {
			log.Printf("dashboard push interesting %s: %v", managerID, err)
		}
	}
	return nil
}

// push publishes one dashboard_update event carrying only the not-yet-pushed
// items of a section. Items stick once pushed (feed is append-only per
// session); removals arrive on the next GET.
func (s *Service) push(ctx context.Context, worldID, managerID uuid.UUID, section Priority, items []Item) error {
	feedID := feedKey(worldID, managerID, section)

	s.mu.Lock()
	known := s.pushedIDs[feedID]
	if known == nil {
		known = make(map[string]bool)
		s.pushedIDs[feedID] = known
	}
	fresh := make([]Item, 0, len(items))
	for _, it := range items {
		if !known[it.ID] {
			fresh = append(fresh, it)
		}
	}
	for _, it := range fresh {
		known[it.ID] = true
	}
	s.mu.Unlock()

	if len(fresh) == 0 {
		return nil
	}

	ev, err := realtime.NewEvent(realtime.EventDashboardUpdate, worldID, DashboardUpdatePayload{Category: section, Items: fresh})
	if err != nil {
		return err
	}
	if err := s.broker.Publish(ctx, ev); err != nil {
		return fmt.Errorf("dashboard publish: %w", err)
	}
	return nil
}

// managersInWorld lists the human managers in a world who hold an active club.
func (s *Service) managersInWorld(ctx context.Context, worldID uuid.UUID) ([]uuid.UUID, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id FROM manager.managers
		WHERE world_id = $1 AND status = 'active' AND current_club_id IS NOT NULL
		ORDER BY id`, worldID)
	if err != nil {
		return nil, fmt.Errorf("managers in world: %w", err)
	}
	defer rows.Close()
	var out []uuid.UUID
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("scan manager: %w", err)
		}
		out = append(out, id)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("managers in world rows: %w", err)
	}
	return out, nil
}

func feedKey(worldID, managerID uuid.UUID, section Priority) string {
	return worldID.String() + ":" + managerID.String() + ":" + string(section)
}

// manageClub picks the club in `clubs` that is a fixture participant; default
// to home when neither matches (callers always pass a managed club).
func manageClub(home, away uuid.UUID, clubs []uuid.UUID) uuid.UUID {
	for _, c := range clubs {
		if c == home || c == away {
			return c
		}
	}
	return home
}

// limitItems caps a section so a noisy late-season feed stays skimmable.
func limitItems(items []Item, n int) []Item {
	if len(items) <= n {
		return items
	}
	return items[:n]
}

func ordinal(n int) string {
	suffix := "th"
	switch n % 100 {
	case 11, 12, 13:
		// th
	default:
		switch n % 10 {
		case 1:
			suffix = "st"
		case 2:
			suffix = "nd"
		case 3:
			suffix = "rd"
		}
	}
	return fmt.Sprintf("%d%s", n, suffix)
}

func finalScore(a, b *int) string {
	hs, as := 0, 0
	if a != nil {
		hs = *a
	}
	if b != nil {
		as = *b
	}
	return fmt.Sprintf("%d–%d", hs, as)
}

func crisisLabel(stage string) string {
	switch stage {
	case "warning":
		return "warning"
	case "restriction":
		return "restriction"
	case "emergency":
		return "emergency"
	case "administration_risk":
		return "administration risk"
	case "ownership_intervention":
		return "ownership intervention"
	case "bankruptcy":
		return "bankruptcy"
	default:
		return stage
	}
}

// money renders an int64 money value (pence) as a readable amount.
func money(p int64) string {
	whole := p / 100
	frac := p % 100
	if frac < 0 {
		frac = -frac
	}
	return fmt.Sprintf("£%d.%02d", whole, frac)
}

func moneyPtr(p *int64) string {
	if p == nil {
		return "a fee"
	}
	return money(*p)
}
