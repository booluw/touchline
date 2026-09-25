package competition

// IM09's manager-facing surface: the club's continental outlook (projected
// entries, conflicts, recorded choices, next-best replacements) and the choice
// upsert. Both are read-only database views over the IM07 fields; the sweep
// itself (resolve.go) is what turns these choices into final assignments at
// campaign start.

import (
	"context"
	"fmt"
	"sort"

	"github.com/google/uuid"
)

// CupConflictView is another continental cup claiming the club (with its soft
// tier, for the manager's tier-precedence awareness).
type CupConflictView struct {
	CupID uuid.UUID `json:"cup_id"`
	Tier  int       `json:"tier"`
}

// CupNextBestView is one club that would take the club's place in the cup if
// it forfeited (the next-best past the club's band cut, in league-table order).
type CupNextBestView struct {
	ClubID uuid.UUID `json:"club_id"`
	Name   string    `json:"name,omitempty"`
	Rank   int       `json:"rank"`
}

// CupQualificationView is the club's outlook for one continental cup.
type CupQualificationView struct {
	CupID          uuid.UUID         `json:"cup_id"`
	CupName        string            `json:"cup_name"`
	Tier           int               `json:"tier"`
	Scope          string            `json:"scope"`
	Origin         string            `json:"origin,omitempty"`
	Rank           int               `json:"rank,omitempty"`
	Projected      bool              `json:"projected"`
	Conflicts      []CupConflictView `json:"conflicts"`
	RecordedChoice uuid.UUID         `json:"recorded_choice_id,omitempty"`
	NextBest       []CupNextBestView `json:"next_best"`
}

// ClubCupQualifications lists every continental cup in the club's world with
// the club's projected entry, conflicts, recorded choice, and the next-best
// replacements that would take its place if it forfeits. Purely read-only;
// world-scoped to the club the caller owns.
func (s *Service) ClubCupQualifications(ctx context.Context, worldID, clubID uuid.UUID) ([]CupQualificationView, error) {
	refs, err := s.continentalCupRefs(ctx, s.pool, worldID)
	if err != nil {
		return nil, err
	}

	fields := make([]Field, 0, len(refs))
	byCup := map[uuid.UUID]Field{}
	for _, ref := range refs {
		f, err := s.ComputeCupField(ctx, ref.ID)
		if err != nil {
			return nil, err
		}
		fields = append(fields, *f)
		byCup[ref.ID] = *f
	}
	tables, err := s.sweepTables(ctx, fields)
	if err != nil {
		return nil, err
	}
	choices, err := s.loadClubChoices(ctx, s.pool, worldID, clubID)
	if err != nil {
		return nil, err
	}

	views := make([]CupQualificationView, 0, len(refs))
	for _, ref := range refs {
		f := byCup[ref.ID]
		view := CupQualificationView{
			CupID:          ref.ID,
			CupName:        ref.Name,
			Tier:           ref.Tier,
			Scope:          string(ref.Scope),
			Projected:      false,
			RecordedChoice: choices[ref.ID],
		}
		for _, e := range f.Entrants {
			if e.ClubID != clubID {
				continue
			}
			view.Projected = true
			view.Origin = e.Origin
			view.Rank = e.Rank
		}
		for otherID, conflicts := range f.Conflicts {
			for _, c := range conflicts {
				view.Conflicts = append(view.Conflicts, CupConflictView{CupID: otherID, Tier: c.Tier})
			}
		}
		sort.Slice(view.Conflicts, func(i, j int) bool {
			return view.Conflicts[i].CupID.String() < view.Conflicts[j].CupID.String()
		})
		if view.Projected {
			view.NextBest = nextBestFor(tables, f, clubID, 2)
		}
		views = append(views, view)
	}

	names, err := s.nextBestNames(ctx, s.pool, views)
	if err != nil {
		return nil, err
	}
	for i := range views {
		for j := range views[i].NextBest {
			views[i].NextBest[j].Name = names[views[i].NextBest[j].ClubID]
		}
	}

	sort.Slice(views, func(i, j int) bool {
		if views[i].Tier != views[j].Tier {
			return views[i].Tier > views[j].Tier
		}
		return views[i].CupID.String() < views[j].CupID.String()
	})
	return views, nil
}

// RecordCupChoice upserts the caller's opt-in for a cup. The club must be
// projected for that cup in its current entitlement-max field, and the cup
// must be a continental cup in the same world. Duplicate re-records (same
// cup) are a no-op upsert. The choice takes effect at the cup's next campaign
// start, when the sweep consumes it.
func (s *Service) RecordCupChoice(ctx context.Context, managerID, clubID, cupID uuid.UUID) error {
	worldID, err := s.RequireOwnership(ctx, managerID, clubID)
	if err != nil {
		return err
	}

	ref, err := s.qualCupRef(ctx, cupID)
	if err != nil {
		return err
	}
	if ref.WorldID != worldID {
		return ErrCompetitionWorldMismatch
	}
	if ref.Type != "continental" {
		return ErrChoiceNotEligible
	}
	f, err := s.ComputeCupField(ctx, cupID)
	if err != nil {
		return err
	}
	if !containsClub(f.Entrants, clubID) {
		return ErrChoiceNotEligible
	}

	_, err = s.pool.Exec(ctx, `
		INSERT INTO competition.manager_cup_choices (world_id, club_id, cup_id, chosen_at)
		VALUES ($1, $2, $3, now())
		ON CONFLICT (club_id, cup_id) DO UPDATE SET chosen_at = now()`,
		worldID, clubID, cupID)
	if err != nil {
		return fmt.Errorf("record cup choice: %w", err)
	}
	return nil
}

// loadClubChoices returns cup → chosen for the club's recorded choices in a
// world.
func (s *Service) loadClubChoices(ctx context.Context, q databaseQuerier, worldID, clubID uuid.UUID) (map[uuid.UUID]uuid.UUID, error) {
	rows, err := q.Query(ctx, `
		SELECT cup_id FROM competition.manager_cup_choices
		WHERE world_id = $1 AND club_id = $2 ORDER BY cup_id`, worldID, clubID)
	if err != nil {
		return nil, fmt.Errorf("club cup choices: %w", err)
	}
	defer rows.Close()
	out := map[uuid.UUID]uuid.UUID{}
	for rows.Next() {
		var cupID uuid.UUID
		if err := rows.Scan(&cupID); err != nil {
			return nil, fmt.Errorf("scan club cup choice: %w", err)
		}
		out[cupID] = cupID
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

// nextBestFor lists the clubs that would take clubID's seat in the cup if it
// forfeited: the highest-ranked clubs in the club's league past the band cut,
// skipping clubs already in the field. Limit bounds the list for the outlook.
func nextBestFor(tables map[uuid.UUID]Table, f Field, clubID uuid.UUID, limit int) []CupNextBestView {
	leagueID := leagueOfEntry(f, tables, clubID)
	if leagueID == uuid.Nil {
		return nil
	}
	var band *Band
	for i := range f.Bands {
		if f.Bands[i].LeagueID == leagueID {
			band = &f.Bands[i]
			break
		}
	}
	if band == nil {
		return nil
	}
	t, ok := tables[leagueID]
	if !ok {
		return nil
	}
	inField := map[uuid.UUID]bool{clubID: true}
	for _, e := range f.Entrants {
		inField[e.ClubID] = true
	}
	out := []CupNextBestView{}
	for i := effectiveTo(t.Ranks, *band); i < len(t.Ranks) && len(out) < limit; i++ {
		c := t.Ranks[i]
		if inField[c] {
			continue
		}
		out = append(out, CupNextBestView{ClubID: c, Rank: i + 1})
	}
	return out
}

// nextBestNames batch-loads the club names referenced by the outlook's
// next-best lists.
func (s *Service) nextBestNames(ctx context.Context, q databaseQuerier, views []CupQualificationView) (map[uuid.UUID]string, error) {
	ids := map[uuid.UUID]bool{}
	for _, v := range views {
		for _, nb := range v.NextBest {
			ids[nb.ClubID] = true
		}
	}
	if len(ids) == 0 {
		return map[uuid.UUID]string{}, nil
	}
	all := make([]uuid.UUID, 0, len(ids))
	for id := range ids {
		all = append(all, id)
	}
	rows, err := q.Query(ctx, `SELECT id, name FROM club.clubs WHERE id = ANY($1::uuid[])`, all)
	if err != nil {
		return nil, fmt.Errorf("next-best club names: %w", err)
	}
	defer rows.Close()
	names := map[uuid.UUID]string{}
	for rows.Next() {
		var id uuid.UUID
		var name string
		if err := rows.Scan(&id, &name); err != nil {
			return nil, fmt.Errorf("scan next-best club name: %w", err)
		}
		names[id] = name
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return names, nil
}
