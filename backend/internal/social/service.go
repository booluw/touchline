package social

import (
	"context"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/touchline/backend/pkg/eventbus"
	"github.com/touchline/backend/pkg/realtime"
)

// Service coordinates the social graph, manager profiles (S06-04a), direct
// messaging (S06-04b) and auto-tracked rivalries (S06-04c). It reads and writes
// the social schema itself and stays dependency-light so later match-delivery
// and messaging hooks can call back into it without import cycles.
type Service struct {
	pool *pgxpool.Pool
	bus  eventbus.EventBus
	rt   realtime.Broker // optional S06-04b live feed fan-out; nil disables pushes
}

// NewService builds the social service. bus may be nil in tests.
func NewService(pool *pgxpool.Pool, bus eventbus.EventBus) *Service {
	return &Service{pool: pool, bus: bus}
}

// WithRealtime installs the broker realtime envelopes (social_message) are
// pushed through once a message commits. It mirrors the optional-fan-out
// pattern of the matchday runner: nil (the default) publishes nothing, so
// tests and pid-less runs stay safe.
func (s *Service) WithRealtime(rt realtime.Broker) *Service {
	s.rt = rt
	return s
}

// GetManagerProfile assembles the profile page for targetID as seen by
// viewerID, both scoped to worldID (OPD-15). Cross-world reads are rejected.
func (s *Service) GetManagerProfile(ctx context.Context, worldID, viewerID, targetID uuid.UUID) (*Profile, error) {
	target, err := s.loadManagerBase(ctx, targetID)
	if err != nil {
		return nil, err
	}
	if target.worldID != worldID {
		return nil, ErrManagerNotInWorld
	}

	viewer, err := s.loadManagerBase(ctx, viewerID)
	if err != nil {
		return nil, err
	}

	profile := &Profile{
		ID:          targetID,
		Name:        managerDisplayName(target),
		Status:      target.status,
		IsPolicyBot: target.isBot,
		Career:      CareerSummary{},
		Trophies:    []Trophy{},
		Rivalries:   []RivalEdge{},
	}

	if target.clubID != nil {
		club, err := s.loadClubRef(ctx, *target.clubID)
		if err != nil {
			return nil, err
		}
		profile.ActiveClub = club
	}

	if profile.Career, err = s.careerSummary(ctx, targetID); err != nil {
		return nil, err
	}
	if profile.Trophies, err = s.trophies(ctx, targetID); err != nil {
		return nil, err
	}
	if profile.TrustScore, err = s.trustScore(ctx, targetID); err != nil {
		return nil, err
	}

	profile.H2HVsViewer = s.resolveH2H(ctx, viewer, target)

	profile.Rivalries, err = s.relationshipEdges(ctx, worldID, targetID, target.clubID)
	if err != nil {
		return nil, err
	}
	return profile, nil
}

// ListRelationships is the GET /api/relationships read (S06-04c): every graph
// edge attached to the caller's manager (personal manager↔manager edges) plus
// those attached to their active club (club↔club edges). World scoping is
// enforced against the caller's manager row.
func (s *Service) ListRelationships(ctx context.Context, worldID, managerID uuid.UUID) ([]RivalEdge, error) {
	m, err := s.loadManagerBase(ctx, managerID)
	if err != nil {
		return nil, err
	}
	if m.worldID != worldID {
		return nil, ErrManagerNotInWorld
	}
	return s.relationshipEdges(ctx, worldID, managerID, m.clubID)
}

// relationshipEdges assembles an entity's full edge surface: its personal edges
// plus, when it runs a club, that club's edges. Club edges are the full
// rivalry truth for AI-managed clubs; human managers additionally see their
// personal edges (S06-04c).
func (s *Service) relationshipEdges(ctx context.Context, worldID, entityID uuid.UUID, clubID *uuid.UUID) ([]RivalEdge, error) {
	edges, err := s.rivalEdges(ctx, worldID, entityID, "manager")
	if err != nil {
		return nil, err
	}
	if clubID != nil {
		clubEdges, err := s.rivalEdges(ctx, worldID, *clubID, "club")
		if err != nil {
			return nil, err
		}
		edges = append(edges, clubEdges...)
	}
	return edges, nil
}

// resolveH2H links the viewer's and target's current clubs. Self-view and
// co-managed clubs produce no record (there is no fixture against yourself).
func (s *Service) resolveH2H(ctx context.Context, viewer, target *managerBaseRow) *H2HRecord {
	if viewer == nil || target == nil || viewer.clubID == nil || target.clubID == nil {
		return nil
	}
	if *viewer.clubID == *target.clubID {
		return nil
	}
	h2h, err := s.h2hRecord(ctx, *viewer.clubID, *target.clubID)
	if err != nil {
		// A fixture query failure must not take down the profile; surface an
		// empty record rather than dropping the whole page.
		return nil
	}
	return h2h
}

// managerDisplayName falls back to a generic label for AI actors without a
// person record.
func managerDisplayName(m *managerBaseRow) string {
	if m == nil || m.name == "" {
		return "AI Manager"
	}
	return m.name
}
