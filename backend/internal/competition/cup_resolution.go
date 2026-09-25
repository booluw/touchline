package competition

// IM09's campaign integration: the sweep runner that feeds ResolveField at
// campaign start and the transactional commitment-news publisher. The sweep is
// pure (resolve.go); this file only gathers deterministic inputs from the world
// inside the campaign's transaction.

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// resolveContinentalCups runs the IM09 sweep over every continental cup in the
// world: it recomputes each cup's entitlement-max field through the IM07
// engine (bands read from the campaign's transaction), loads the manager cup
// choices for the world, and returns the resolved per-cup assignment plus the
// cascade chain. Writes nothing. The caller must use the starting cup's
// assignment — and only that cup's — for memberships/entries.
func (s *Service) resolveContinentalCups(ctx context.Context, tx pgx.Tx, worldID uuid.UUID) (*ResolveOutput, map[uuid.UUID]Field, error) {
	refs, err := s.continentalCupRefs(ctx, tx, worldID)
	if err != nil {
		return nil, nil, err
	}
	if len(refs) == 0 {
		return &ResolveOutput{Assignments: map[uuid.UUID][]Entrant{}}, map[uuid.UUID]Field{}, nil
	}

	fields := make([]Field, 0, len(refs))
	fieldsByCup := map[uuid.UUID]Field{}
	for _, ref := range refs {
		bands, err := s.bandsForCup(ctx, tx, ref.ID)
		if err != nil {
			return nil, nil, err
		}
		f, err := ComputeField(ctx, QualifyField{Cup: ref, Bands: bands}, s, s, s)
		if err != nil {
			return nil, nil, fmt.Errorf("sweep field of cup %s: %w", ref.ID, err)
		}
		fields = append(fields, *f)
		fieldsByCup[ref.ID] = *f
	}

	tables, err := s.sweepTables(ctx, fields)
	if err != nil {
		return nil, nil, err
	}
	choices, err := s.sweepChoices(ctx, tx, worldID)
	if err != nil {
		return nil, nil, err
	}

	out, err := ResolveField(ResolveInput{Fields: fields, Tables: tables, Choices: choices})
	if err != nil {
		return nil, nil, err
	}
	return out, fieldsByCup, nil
}

// continentalCupRefs lists every continental cup in the world in creation
// order (the sweep is insensitive to order; this is just deterministic input
// assembly).
func (s *Service) continentalCupRefs(ctx context.Context, q databaseQuerier, worldID uuid.UUID) ([]CompetitionRef, error) {
	rows, err := q.Query(ctx, `
		SELECT c.id, c.country_id, c.region_id, c.name, c.competition_type, COALESCE(c.tier, 0), c.created_at
		FROM competition.competitions c
		WHERE c.world_id = $1 AND c.competition_type = 'continental'
		ORDER BY c.created_at, c.id`, worldID)
	if err != nil {
		return nil, fmt.Errorf("continental cups: %w", err)
	}
	defer rows.Close()

	out := make([]CompetitionRef, 0, 8)
	for rows.Next() {
		var ref CompetitionRef
		var countryID, regionID *uuid.UUID
		var tier *int
		if err := rows.Scan(&ref.ID, &countryID, &regionID, &ref.Name, &ref.Type, &tier, &ref.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan continental cup: %w", err)
		}
		ref.WorldID = worldID
		if tier != nil {
			ref.Tier = *tier
		}
		if regionID != nil {
			ref.Scope = ScopeRegion
			ref.RegionID = *regionID
		} else {
			ref.Scope = ScopeCountry
			if countryID != nil {
				ref.CountryID = *countryID
			}
		}
		out = append(out, ref)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

// sweepTables loads the last-completed standings for every distinctly banded
// league across the fields — the same read the IM07 provider itself uses, so
// cascade next-best picks agree with the fields' ranks.
func (s *Service) sweepTables(ctx context.Context, fields []Field) (map[uuid.UUID]Table, error) {
	leagueIDs := map[uuid.UUID]bool{}
	for _, f := range fields {
		for _, b := range f.Bands {
			leagueIDs[b.LeagueID] = true
		}
	}
	tables := map[uuid.UUID]Table{}
	for lid := range leagueIDs {
		t, err := s.LastCompletedStandings(ctx, lid)
		if err != nil {
			return nil, fmt.Errorf("sweep standings of %s: %w", lid, err)
		}
		if t != nil {
			tables[lid] = *t
		}
	}
	return tables, nil
}

// sweepChoices loads the world's manager_cup_choices rows as the
// club → cups map ResolveField consumes.
func (s *Service) sweepChoices(ctx context.Context, tx pgx.Tx, worldID uuid.UUID) (map[uuid.UUID][]uuid.UUID, error) {
	rows, err := tx.Query(ctx, `
		SELECT club_id, cup_id FROM competition.manager_cup_choices WHERE world_id = $1
		ORDER BY club_id, cup_id`, worldID)
	if err != nil {
		return nil, fmt.Errorf("manager cup choices: %w", err)
	}
	defer rows.Close()
	choices := map[uuid.UUID][]uuid.UUID{}
	for rows.Next() {
		var clubID, cupID uuid.UUID
		if err := rows.Scan(&clubID, &cupID); err != nil {
			return nil, fmt.Errorf("scan manager cup choice: %w", err)
		}
		choices[clubID] = append(choices[clubID], cupID)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return choices, nil
}

// publishCupCascadeNews writes one world.news_stories row per IM09 cascade
// that touches the starting cup (KeptCupID or LostCupID), inside the same
// transaction as the campaign writes. Stories use category 'general' and sit
// in the world feed (country_id NULL), related to the CUP_CAMPAIGN_STARTED
// event. Cascades for other cups are deliberately not published here: they
// will be covered when those cups start, avoiding duplicate stories.
func (s *Service) publishCupCascadeNews(ctx context.Context, tx pgx.Tx, worldID, cupID, relatedEventID uuid.UUID, cascades []Cascade) error {
	ids := make([]uuid.UUID, 0, len(cascades))
	kept := map[uuid.UUID][]Cascade{}
	for _, cas := range cascades {
		if cas.KeptCupID != cupID && cas.LostCupID != cupID {
			continue
		}
		ids = append(ids, cas.ClubID)
		if cas.ReplacementID != uuid.Nil {
			ids = append(ids, cas.ReplacementID)
		}
		kept[cas.KeptCupID] = append(kept[cas.KeptCupID], cas)
	}
	if len(ids) == 0 {
		return nil
	}

	clubNames, cupNames, err := s.cascadeNames(ctx, tx, ids, cascades)
	if err != nil {
		return err
	}
	for cupAID, list := range kept {
		cupAName := cupNames[cupAID]
		for _, cas := range list {
			headline := fmt.Sprintf("%s will play in %s", clubNames[cas.ClubID], cupAName)
			body := buildCascadeBody(cas, cupAName, cupNames, clubNames)
			if _, err := tx.Exec(ctx, `
				INSERT INTO world.news_stories (world_id, headline, body, category, related_event_id, country_id)
				VALUES ($1, $2, $3, 'general', $4, NULL)`,
				worldID, headline, body, relatedEventID); err != nil {
				return fmt.Errorf("publish cup cascade news: %w", err)
			}
		}
	}
	return nil
}

func buildCascadeBody(cas Cascade, keptName string, cupNames map[uuid.UUID]string, clubNames map[uuid.UUID]string) string {
	lostName := cupNames[cas.LostCupID]
	clubName := clubNames[cas.ClubID]
	if cas.ReplacementID == uuid.Nil {
		return fmt.Sprintf("%s forfeited its slot in %s but keeps %s; the freed place stays open.",
			clubName, lostName, keptName)
	}
	replName := clubNames[cas.ReplacementID]
	return fmt.Sprintf("%s forfeited its slot in %s to play in %s; %s joins %s as the next-best replacement.",
		clubName, lostName, keptName, replName, lostName)
}

// cascadeNames loads the club and cup names a cascade story references.
func (s *Service) cascadeNames(ctx context.Context, tx pgx.Tx, clubIDs []uuid.UUID, cascades []Cascade) (map[uuid.UUID]string, map[uuid.UUID]string, error) {
	clubNames := map[uuid.UUID]string{}
	cupNames := map[uuid.UUID]string{}
	if len(clubIDs) > 0 {
		rows, err := tx.Query(ctx,
			`SELECT id, name FROM club.clubs WHERE id = ANY($1::uuid[])`, clubIDs)
		if err != nil {
			return nil, nil, fmt.Errorf("cascade club names: %w", err)
		}
		defer rows.Close()
		for rows.Next() {
			var id uuid.UUID
			var name string
			if err := rows.Scan(&id, &name); err != nil {
				return nil, nil, fmt.Errorf("scan cascade club name: %w", err)
			}
			clubNames[id] = name
		}
		if err := rows.Err(); err != nil {
			return nil, nil, err
		}
	}

	cupIDs := map[uuid.UUID]bool{}
	for _, cas := range cascades {
		cupIDs[cas.KeptCupID] = true
		cupIDs[cas.LostCupID] = true
	}
	if len(cupIDs) > 0 {
		all := make([]uuid.UUID, 0, len(cupIDs))
		for id := range cupIDs {
			all = append(all, id)
		}
		rows, err := tx.Query(ctx,
			`SELECT id, name FROM competition.competitions WHERE id = ANY($1::uuid[])`, all)
		if err != nil {
			return nil, nil, fmt.Errorf("cascade cup names: %w", err)
		}
		defer rows.Close()
		for rows.Next() {
			var id uuid.UUID
			var name string
			if err := rows.Scan(&id, &name); err != nil {
				return nil, nil, fmt.Errorf("scan cascade cup name: %w", err)
			}
			cupNames[id] = name
		}
		if err := rows.Err(); err != nil {
			return nil, nil, err
		}
	}
	return clubNames, cupNames, nil
}
