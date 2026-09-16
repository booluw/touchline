package competition

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/touchline/backend/pkg/eventbus"
)

// rolloverCountry completes the just-finished season of every league in the
// country and advances promotion/relegation. It runs only when every league's
// fixtures are finished, so the cascade is atomic per country. Entry sets are
// stayers + clubs promoted from the tier below + clubs relegated from the
// tier above; the team count of each next season must reproduce the league's
// team_count exactly (guaranteed by symmetric adjacency rules).
func (s *Service) rolloverCountry(ctx context.Context, tx pgx.Tx, worldID, countryID uuid.UUID) error {
	leagues, err := s.leaguesByCountry(ctx, tx, countryID)
	if err != nil {
		return err
	}

	type active struct {
		league *League
		season *Season
		order  []uuid.UUID
	}
	actives := []active{}
	byLeague := map[uuid.UUID]*active{}
	for i := range leagues {
		l := &leagues[i]
		season, err := s.activeSeason(ctx, tx, l.ID, worldID)
		if err != nil && err != ErrNoSeason {
			return err
		}
		if season == nil {
			continue
		}
		order, err := s.orderedClubIDs(ctx, tx, season.ID)
		if err != nil {
			return err
		}
		a := &active{league: l, season: season, order: order}
		actives = append(actives, *a)
		byLeague[l.ID] = a

		if _, err := tx.Exec(ctx, `
			UPDATE competition.seasons SET status = 'completed', end_date = now()
			WHERE id = $1`, season.ID); err != nil {
			return fmt.Errorf("complete season: %w", err)
		}
	}

	if len(actives) == 0 {
		return nil
	}

	nextEntries := map[uuid.UUID][]uuid.UUID{}
	promotedOut := map[uuid.UUID][]uuid.UUID{}
	relegatedOut := map[uuid.UUID][]uuid.UUID{}

	for _, a := range actives {
		l := a.league
		prom := min(l.Promotions, len(a.order))
		rel := min(l.Relegations, len(a.order))
		promoted := a.order[:prom]
		relegated := a.order[len(a.order)-rel:]

		// Stayers plus any clubs that arrived earlier (promoted from below or
		// relegated from above) — never overwrite already-accumulated entries.
		stayers := a.order[prom : len(a.order)-rel]
		nextEntries[l.ID] = append(nextEntries[l.ID], stayers...)
		promotedOut[l.ID] = promoted
		relegatedOut[l.ID] = relegated

		for _, clubID := range promoted {
			if l.PromotesTo != nil {
				nextEntries[*l.PromotesTo] = append(nextEntries[*l.PromotesTo], clubID)
			}
		}
		for _, clubID := range relegated {
			if l.RelegatesTo != nil {
				nextEntries[*l.RelegatesTo] = append(nextEntries[*l.RelegatesTo], clubID)
			}
		}
		s.emitSeasonCompleted(ctx, tx, worldID, a.season, a.order)
	}

	for _, a := range actives {
		l := a.league
		for _, clubID := range promotedOut[l.ID] {
			if err := s.emitClubMoved(ctx, tx, worldID, a.season, "CLUB_PROMOTED", clubID, l.PromotesTo); err != nil {
				return err
			}
		}
		for _, clubID := range relegatedOut[l.ID] {
			if err := s.emitClubMoved(ctx, tx, worldID, a.season, "CLUB_RELEGATED", clubID, l.RelegatesTo); err != nil {
				return err
			}
		}
	}

	for _, a := range actives {
		l := a.league
		entries := nextEntries[l.ID]
		if len(entries) != l.TeamCount {
			return fmt.Errorf("%w: %s next season has %d entries, want %d",
				errInternalRollover, l.Name, len(entries), l.TeamCount)
		}
		anchor, err := s.lastScheduledDay(ctx, tx, l.ID, worldID)
		if err != nil {
			return err
		}
		next, err := s.createSeason(ctx, tx, worldID, l.ID, anchor, entries)
		if err != nil {
			return err
		}
		count, _, err := s.createFixtures(ctx, tx, worldID, l.ID, entries, anchor)
		if err != nil {
			return err
		}
		if err := s.emitNextSeason(ctx, tx, worldID, l.ID, next, entries, count); err != nil {
			return err
		}
	}
	return nil
}

// orderedClubIDs returns a league season's final ranking as club ids using the
// same tie-breakers as GetStandings.
func (s *Service) orderedClubIDs(ctx context.Context, tx pgx.Tx, seasonID uuid.UUID) ([]uuid.UUID, error) {
	rows, err := tx.Query(ctx, `
		SELECT c.id
		FROM competition.standings st
		JOIN club.clubs c ON c.id = st.club_id
		WHERE st.season_id = $1
		ORDER BY st.points DESC, (st.goals_for - st.goals_against) DESC, st.goals_for DESC, c.name`, seasonID)
	if err != nil {
		return nil, fmt.Errorf("ordered club ids: %w", err)
	}
	defer rows.Close()
	out := []uuid.UUID{}
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("scan ordered club id: %w", err)
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

// lastScheduledDay is the day the next season's fixtures anchor to: the day
// after the reference date of the season just completed (its last scheduled
// fixture), so calendaring stays continuous across seasons.
func (s *Service) lastScheduledDay(ctx context.Context, tx pgx.Tx, leagueID, worldID uuid.UUID) (time.Time, error) {
	var last time.Time
	var start time.Time
	err := tx.QueryRow(ctx, `
		(SELECT MAX(f.scheduled_at) FROM match.fixtures f
		 WHERE f.competition_id = $1 AND f.world_id = $2 AND f.status <> 'cancelled')`,
		leagueID, worldID).Scan(&last)
	if err != nil {
		return time.Time{}, fmt.Errorf("last scheduled day: %w", err)
	}
	if err := tx.QueryRow(ctx, `
		SELECT start_date FROM competition.seasons s
		WHERE s.competition_id = $1 AND s.world_id = $2
		ORDER BY s.season_number DESC LIMIT 1`, leagueID, worldID).Scan(&start); err != nil {
		return time.Time{}, fmt.Errorf("season start: %w", err)
	}
	if last.IsZero() {
		last = start
	}
	if last.Before(start) {
		last = start
	}
	return last, nil
}

func (s *Service) emitSeasonCompleted(ctx context.Context, tx pgx.Tx, worldID uuid.UUID, season *Season, order []uuid.UUID) error {
	champion := uuid.Nil
	if len(order) > 0 {
		champion = order[0]
	}
	// country_id lets the academy seasonal hook target just this league's
	// country (S08-01); nil for a country-less competition.
	var countryID *uuid.UUID
	if err := tx.QueryRow(ctx,
		`SELECT country_id FROM competition.competitions WHERE id = $1`,
		season.CompetitionID).Scan(&countryID); err != nil {
		return fmt.Errorf("season completed: load country: %w", err)
	}
	return s.recordSeedEvent(ctx, tx, &eventbus.Event{
		WorldID:   worldID,
		EventType: "SEASON_COMPLETED",
		Payload: mustJSON(map[string]any{
			"season_id":        season.ID,
			"competition_id":   season.CompetitionID,
			"country_id":       countryID,
			"champion_club_id": champion,
		}),
	})
}

func (s *Service) emitClubMoved(ctx context.Context, tx pgx.Tx, worldID uuid.UUID, season *Season,
	eventType string, clubID uuid.UUID, dest *uuid.UUID) error {
	payload := map[string]any{
		"club_id":        clubID,
		"season_id":      season.ID,
		"competition_id": season.CompetitionID,
	}
	if dest != nil {
		payload["destination_id"] = *dest
	}
	return s.recordSeedEvent(ctx, tx, &eventbus.Event{
		WorldID:   worldID,
		EventType: eventType,
		Payload:   mustJSON(payload),
	})
}

func (s *Service) emitNextSeason(ctx context.Context, tx pgx.Tx, worldID uuid.UUID, leagueID uuid.UUID,
	season *Season, entries []uuid.UUID, fixtureCount int) error {
	clubIDs := make([]string, 0, len(entries))
	for _, e := range entries {
		clubIDs = append(clubIDs, e.String())
	}
	return s.recordSeedEvent(ctx, tx, &eventbus.Event{
		WorldID:   worldID,
		EventType: "SEASON_CREATED",
		Payload: mustJSON(map[string]any{
			"competition_id": leagueID,
			"season_id":      season.ID,
			"season_label":   season.SeasonLabel,
			"season_number":  season.SeasonNumber,
			"team_count":     len(entries),
			"fixture_count":  fixtureCount,
			"entries":        clubIDs,
		}),
	})
}
