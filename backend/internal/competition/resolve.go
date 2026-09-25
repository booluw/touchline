package competition

// The IM09 resolution sweep. IM07 computes each continental cup's
// entitlement-maximal field (qualify.go) and flags double-booked champions;
// only this engine can answer "which cup does a double-qualified club actually
// play" because that needs the cross-cup comparison IM07 deliberately avoids.
//
// ResolveField is pure and deterministic: given every continental cup's field
// plus the clubs' recorded manager_cup_choices it returns a per-cup field
// assignment (the campaign start writes these), the cascade chain (for news),
// or ErrResolutionImpossible when a residual violation survives the fixpoint —
// a club is never silently dropped from every cup.

import (
	"fmt"
	"sort"
	"time"

	"github.com/google/uuid"
)

// Cascade is one forfeit/replacement step the sweep produced: Club forfeited
// its slot in LostCup to KeptCup, and LostCup's +1 went to Replacement
// (uuid.Nil when no next-best existed). News text names all of them.
type Cascade struct {
	ClubID        uuid.UUID
	LostCupID     uuid.UUID
	KeptCupID     uuid.UUID
	ReplacementID uuid.UUID
}

// ResolveInput is everything the sweep needs. Fields are the IM07
// entitlement-maximal views for every continental cup in the world; Tables are
// the same completed-season league tables IM07 read (needed to find each
// loss's next-best); Choices are the manager_cup_choices rows (club → cups it
// opted into).
type ResolveInput struct {
	Fields  []Field
	Tables  map[uuid.UUID]Table
	Choices map[uuid.UUID][]uuid.UUID
}

// ResolveOutput is the resolution: the final per-cup assignment (origins
// preserved, cascade promotions tagged OriginCascadeReplacement) plus the
// cascade chain the campaign start turns into news.
type ResolveOutput struct {
	Assignments map[uuid.UUID][]Entrant
	Cascades    []Cascade
}

// ResolveField runs the IM09 sweep to a fixpoint and then checks the
// invariants a campaign start cannot tolerate. It never touches the database
// or the wall clock: identical fields + league tables + choices ⇒ identical
// assignments (the unit tests assert byte-for-byte equality).
func ResolveField(in ResolveInput) (*ResolveOutput, error) {
	if in.Tables == nil {
		in.Tables = map[uuid.UUID]Table{}
	}
	if in.Choices == nil {
		in.Choices = map[uuid.UUID][]uuid.UUID{}
	}

	fields := make(map[uuid.UUID]Field, len(in.Fields))
	cupIDs := make([]uuid.UUID, 0, len(in.Fields))
	for _, f := range in.Fields {
		fields[f.Cup.ID] = f
		cupIDs = append(cupIDs, f.Cup.ID)
	}
	sort.Slice(cupIDs, func(i, j int) bool { return cupIDs[i].String() < cupIDs[j].String() })

	// Champion entitlement, judged on the ORIGINAL fields: a club is the
	// reigning champion of every cup whose entitlement-max field carries it
	// with a champion_* origin.
	championOf := map[uuid.UUID][]uuid.UUID{}
	for _, f := range in.Fields {
		for _, e := range f.Entrants {
			if isChampionOrigin(e.Origin) {
				championOf[e.ClubID] = append(championOf[e.ClubID], f.Cup.ID)
			}
		}
	}

	// Working assignment: cup → per-cup entrants. Starts as a copy of the
	// entitlement-max input.
	assign := map[uuid.UUID][]Entrant{}
	for _, f := range in.Fields {
		assign[f.Cup.ID] = append([]Entrant(nil), f.Entrants...)
	}
	clubCups := map[uuid.UUID][]uuid.UUID{} // club → cups it currently holds a seat in
	for cupID, entrants := range assign {
		for _, e := range entrants {
			clubCups[e.ClubID] = append(clubCups[e.ClubID], cupID)
		}
	}

	var cascades []Cascade
	// resolved marks (club, cup) pairs that reached a final decision: the club
	// was once removed from that cup and must never be re-promoted into it, so
	// the fixpoint makes progress instead of cycling the same club.
	resolved := map[[2]uuid.UUID]bool{}
	for {
		club := firstDoubleBooked(clubCups)
		if club == uuid.Nil {
			break
		}
		cups := sortedCupIDs(clubCups[club])
		winner := pickWinner(club, cups, fields, championOf, in.Choices)
		for _, loser := range cups {
			if loser == winner {
				continue
			}
			if _, ok := removeEntrant(assign, loser, club); !ok {
				return nil, fmt.Errorf("%w: club %s held no seat in cup %s", ErrResolutionImpossible, club, loser)
			}
			key := [2]uuid.UUID{club, loser}
			resolved[key] = true
			replacement := cascadeReplacement(fields, in.Tables, assign, loser, club, resolved)
			cascades = append(cascades, Cascade{
				ClubID:        club,
				LostCupID:     loser,
				KeptCupID:     winner,
				ReplacementID: replacement,
			})
			if replacement != uuid.Nil {
				assign[loser] = append(assign[loser], Entrant{
					ClubID: replacement,
					Origin: OriginCascadeReplacement,
					Rank:   rankOf(in.Tables, loser, replacement, fields),
				})
				clubCups[replacement] = append(clubCups[replacement], loser)
			}
			clubCups[club] = dropCup(clubCups[club], loser)
		}
		if len(resolved) > maxResolutionSteps(in.Fields) {
			return nil, ErrResolutionImpossible
		}
	}

	// Invariants a campaign cannot tolerate: a projected cup with fewer than
	// two clubs, or a champion that vanished from every cup.
	for _, f := range in.Fields {
		if len(assign[f.Cup.ID]) < 2 {
			return nil, fmt.Errorf("%w: cup %s shrunk below two clubs", ErrResolutionImpossible, f.Cup.ID)
		}
	}
	// A champion must end up in at least one cup, but not necessarily the cup it
	// is champion of — tier precedence or a manager choice may carry it into a
	// sibling cup, which is legitimate. Dropping it from *every* cup is not.
	for clubID := range championOf {
		inAny := false
		for _, entrants := range assign {
			if containsClub(entrants, clubID) {
				inAny = true
				break
			}
		}
		if !inAny {
			return nil, fmt.Errorf("%w: champion %s was dropped from every cup", ErrResolutionImpossible, clubID)
		}
	}

	out := &ResolveOutput{
		Assignments: map[uuid.UUID][]Entrant{},
		Cascades:    sortCascades(cascades),
	}
	for _, cupID := range cupIDs {
		out.Assignments[cupID] = sortEntrants(assign[cupID])
	}
	return out, nil
}

// pickWinner decides which cup keeps the double-booked club: strict tier
// precedence, then the manager's unique recorded choice among the conflicted
// cups, then the defend-default (reigning champion of), then the earlier
// created cup. Deterministic by construction.
func pickWinner(clubID uuid.UUID, cups []uuid.UUID, fields map[uuid.UUID]Field, championOf map[uuid.UUID][]uuid.UUID, choices map[uuid.UUID][]uuid.UUID) uuid.UUID {
	best := cups[0]
	for _, cupID := range cups[1:] {
		if cupKeyBetter(clubID, cupID, best, fields, championOf, choices) {
			best = cupID
		}
	}
	return best
}

// cupKeyBetter reports whether cup a beats cup b for this club. The key tuple
// (tier desc, unique-choice desc, champion desc, created asc, id asc) is a
// total ordering, so the pairwise fold is transitive and order-independent.
func cupKeyBetter(clubID uuid.UUID, a, b uuid.UUID, fields map[uuid.UUID]Field, championOf map[uuid.UUID][]uuid.UUID, choices map[uuid.UUID][]uuid.UUID) bool {
	ka := clubCupKey(clubID, a, fields, championOf, choices)
	kb := clubCupKey(clubID, b, fields, championOf, choices)
	if ka.tier != kb.tier {
		return ka.tier > kb.tier
	}
	if ka.chosen != kb.chosen {
		return ka.chosen == 1
	}
	if ka.champion != kb.champion {
		return ka.champion == 1
	}
	if !ka.created.Equal(kb.created) {
		return ka.created.Before(kb.created)
	}
	return a.String() < b.String()
}

// clubCupKey is the deterministic score of cup `cupID` keeping `clubID`.
func clubCupKey(clubID, cupID uuid.UUID, fields map[uuid.UUID]Field, championOf map[uuid.UUID][]uuid.UUID, choices map[uuid.UUID][]uuid.UUID) (k struct {
	tier     int
	chosen   int
	champion int
	created  time.Time
}) {
	f := fields[cupID]
	k.tier = f.Cup.Tier
	k.created = f.Cup.CreatedAt
	for _, c := range championOf[clubID] {
		if c == cupID {
			k.champion = 1
		}
	}
	if uniqueChoiceFor(clubID, cupID, fields, choices) {
		k.chosen = 1
	}
	return k
}

// uniqueChoiceFor reports whether cupID is the single cup the club opted into
// among the cups it is currently projected for. Choices for cups the club
// isn't projected for are noise (rejected at POST time anyway).
func uniqueChoiceFor(clubID, cupID uuid.UUID, fields map[uuid.UUID]Field, choices map[uuid.UUID][]uuid.UUID) bool {
	projected := map[uuid.UUID]bool{}
	for _, f := range fields {
		for _, e := range f.Entrants {
			if e.ClubID == clubID {
				projected[f.Cup.ID] = true
			}
		}
	}
	match := uuid.Nil
	for _, c := range choices[clubID] {
		if !projected[c] {
			continue
		}
		if match != uuid.Nil {
			return false // more than one meaningful choice ⇒ default applies
		}
		match = c
	}
	return match == cupID
}

// cascadeReplacement is the +1 next-best for a cup that just lost clubID: the
// highest-ranked club in the losing club's league past the band cut that is
// not already in the cup's current field and not already decided for this cup
// (resolved). uuid.Nil when the league has no band row in the cup (a
// champion_direct slot has no natural replacement) or the ranks have nothing
// left.
func cascadeReplacement(fields map[uuid.UUID]Field, tables map[uuid.UUID]Table, assign map[uuid.UUID][]Entrant, cupID, clubID uuid.UUID, resolved map[[2]uuid.UUID]bool) uuid.UUID {
	f := fields[cupID]
	leagueID := leagueOfEntry(f, tables, clubID)
	if leagueID == uuid.Nil {
		return uuid.Nil
	}
	var band *Band
	for i := range f.Bands {
		if f.Bands[i].LeagueID == leagueID {
			band = &f.Bands[i]
			break
		}
	}
	if band == nil {
		return uuid.Nil // champion_direct: the league has no band row here
	}
	t, ok := tables[leagueID]
	if !ok || len(t.Ranks) == 0 {
		return uuid.Nil
	}
	skip := map[uuid.UUID]bool{}
	for _, e := range assign[cupID] {
		skip[e.ClubID] = true
	}
	for i := effectiveTo(t.Ranks, *band); i < len(t.Ranks); i++ {
		c := t.Ranks[i]
		if skip[c] || resolved[[2]uuid.UUID{c, cupID}] {
			continue
		}
		return c
	}
	return uuid.Nil
}

// leagueOfEntry finds the league a club's seat in a cup draws from: the band
// whose league-table ranks contain the club. A club with no rank (direct
// champion) has no league context.
func leagueOfEntry(f Field, tables map[uuid.UUID]Table, clubID uuid.UUID) uuid.UUID {
	for _, b := range f.Bands {
		t, ok := tables[b.LeagueID]
		if !ok {
			continue
		}
		for _, c := range t.Ranks {
			if c == clubID {
				return b.LeagueID
			}
		}
	}
	return uuid.Nil
}

// rankOf returns the club's league-table rank for a brand-new cascade entry
// (0 = unknown / not yet ranked in the plan).
func rankOf(tables map[uuid.UUID]Table, cupID uuid.UUID, clubID uuid.UUID, fields map[uuid.UUID]Field) int {
	f := fields[cupID]
	leagueID := leagueOfEntry(f, tables, clubID)
	if leagueID == uuid.Nil {
		return 0
	}
	t, ok := tables[leagueID]
	if !ok {
		return 0
	}
	for i, c := range t.Ranks {
		if c == clubID {
			return i + 1
		}
	}
	return 0
}

func containsClub(entrants []Entrant, clubID uuid.UUID) bool {
	for _, e := range entrants {
		if e.ClubID == clubID {
			return true
		}
	}
	return false
}

func removeEntrant(assign map[uuid.UUID][]Entrant, cupID, clubID uuid.UUID) (Entrant, bool) {
	list := assign[cupID]
	for i, e := range list {
		if e.ClubID == clubID {
			assign[cupID] = append(list[:i:i], list[i+1:]...)
			return e, true
		}
	}
	return Entrant{}, false
}

func dropCup(list []uuid.UUID, cupID uuid.UUID) []uuid.UUID {
	out := list[:0]
	for _, c := range list {
		if c != cupID {
			out = append(out, c)
		}
	}
	return out
}

// firstDoubleBooked returns the smallest club id that currently holds a seat
// in more than one cup, or uuid.Nil when none does.
func firstDoubleBooked(clubCups map[uuid.UUID][]uuid.UUID) uuid.UUID {
	var best uuid.UUID
	for clubID, cups := range clubCups {
		if len(cups) < 2 {
			continue
		}
		if best == uuid.Nil || clubID.String() < best.String() {
			best = clubID
		}
	}
	return best
}

func sortedCupIDs(ids []uuid.UUID) []uuid.UUID {
	out := append([]uuid.UUID(nil), ids...)
	sort.Slice(out, func(i, j int) bool { return out[i].String() < out[j].String() })
	return out
}

func sortEntrants(entrants []Entrant) []Entrant {
	out := append([]Entrant(nil), entrants...)
	sort.Slice(out, func(i, j int) bool { return out[i].ClubID.String() < out[j].ClubID.String() })
	return out
}

func sortCascades(cascades []Cascade) []Cascade {
	out := append([]Cascade(nil), cascades...)
	sort.Slice(out, func(i, j int) bool {
		if out[i].ClubID != out[j].ClubID {
			return out[i].ClubID.String() < out[j].ClubID.String()
		}
		return out[i].LostCupID.String() < out[j].LostCupID.String()
	})
	return out
}

func isChampionOrigin(origin string) bool {
	return origin == OriginChampionDirect ||
		origin == OriginChampionNextBest ||
		origin == OriginChampionOutOfBand
}

// maxResolutionSteps bounds the fixpoint by one decision per club×cup seat (a
// generous multiple; the sweep must never spin on a graph error).
func maxResolutionSteps(fields []Field) int {
	n := 0
	for _, f := range fields {
		n += len(f.Entrants)
	}
	if n == 0 {
		n = 1
	}
	return n * 8
}
