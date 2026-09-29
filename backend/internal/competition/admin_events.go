package competition

import (
	"context"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/touchline/backend/pkg/eventbus"
)

// Admin configuration events (IM27). Every admin change to the competition
// world — countries, leagues, regions, reputation, cup setup — is recorded in
// world.events in the same transaction as the change, like every other state
// change (Tech Plan §4 event spine; OPD-23 atomicity).
const (
	EventCountryCreated        = "COUNTRY_CREATED"
	EventLeagueCreated         = "LEAGUE_CREATED"
	EventLeagueAdjacencySet    = "LEAGUE_ADJACENCY_SET"
	EventLeagueReputationSet   = "LEAGUE_REPUTATION_SET"
	EventRegionCreated         = "REGION_CREATED"
	EventRegionDeleted         = "REGION_DELETED"
	EventCountryRegionSet      = "COUNTRY_REGION_SET"
	EventCupCreated            = "CUP_CREATED"
	EventCupQualificationSet   = "CUP_QUALIFICATION_SET"
	EventCupFinalDatePolicySet = "CUP_FINAL_DATE_POLICY_SET"
)

// recordAdminEvent appends one admin configuration event inside tx, stamped
// with the world's current tick and the system actor (admin endpoints carry
// no manager actor).
func (s *Service) recordAdminEvent(ctx context.Context, tx pgx.Tx, worldID uuid.UUID, eventType string, payload map[string]any) error {
	return s.recordSeedEvent(ctx, tx, &eventbus.Event{
		WorldID:   worldID,
		EventType: eventType,
		Payload:   mustJSON(payload),
	})
}
