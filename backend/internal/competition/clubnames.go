package competition

import (
	"context"
	"errors"
	"fmt"
	"math/rand"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/touchline/backend/pkg/eventbus"
)

const maxClubNameAttempts = 200

// nextClubName deterministically picks a human-plausible, unique club name
// from the given pools (stems + suffixes, sourced from ref.club_name_parts).
// Names already used in this seeding run are skipped; the pools are large
// enough that retries are bounded. Callers must have already ensured both
// pools are non-empty via bootstrap.LoadClubNameParts.
func nextClubName(rng *rand.Rand, stems, suffixes []string, used map[string]bool) string {
	for attempt := 0; attempt < maxClubNameAttempts; attempt++ {
		stem := stems[rng.Intn(len(stems))]
		suffix := suffixes[rng.Intn(len(suffixes))]
		name := fmt.Sprintf("%s %s", stem, suffix)
		if !used[name] {
			return name
		}
	}
	return fmt.Sprintf("New Athletic %d", rng.Int63n(100000))
}

// short converts a club name to its abbreviated 3-letter display form
// (e.g. "Arsenal FC" → "ARS").
func short(name string) string {
	clean := strings.TrimSpace(name)
	for _, token := range strings.Fields(clean) {
		switch token {
		case "FC", "SC", "AC", "United", "City":
			continue
		}
		if len(token) >= 3 {
			return strings.ToUpper(token[:3])
		}
	}
	for _, token := range strings.Fields(clean) {
		if len(token) > 0 {
			return strings.ToUpper(token[:3])
		}
	}
	return "AAA"
}

// roundRobin computes the canonical double round-robin schedule for n teams:
// the first leg is the standard circle method, the second leg mirrors it with
// venues swapped so every pair meets home-and-away exactly once. Returns one
// round per matchday; each round holds native team indices as [home, away].
func roundRobin(n int) [][][]int {
	if n%2 != 0 {
		n++
	}
	var schedule [][][]int
	teams := make([]int, n)
	for i := range teams {
		teams[i] = i
	}
	for leg := 0; leg < 2; leg++ {
		for round := 0; round < n-1; round++ {
			pairs := make([][]int, 0, n/2)
			for i := 0; i < n/2; i++ {
				home := teams[i]
				away := teams[n-1-i]
				if leg == 1 {
					home, away = away, home
				}
				pairs = append(pairs, []int{home, away})
			}
			schedule = append(schedule, pairs)

			first := teams[1]
			copy(teams[1:], teams[2:])
			teams[n-1] = first
		}
	}
	return schedule
}

// daysTruncate returns the calendar day (midnight UTC) of t — the anchor for
// fixture scheduling so matchdays land on whole days at KickoffHourUTC. The
// UTC calendar day (not the local one) is used so the anchor is
// wall-clock/timezone-independent and matches the world-day counter mapping in
// matchday.worldDate.
func daysTruncate(t time.Time) time.Time {
	y, m, d := t.UTC().Date()
	return time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
}

// seasonLabel renders a season's human label: "YYYY" for season 1 and
// "YYYY/YY" from season 2 onward, the year advancing one per season.
func seasonLabel(ref time.Time, number int) string {
	start := ref.Year() + (number - 1)
	if number <= 1 {
		return fmt.Sprintf("%d", start)
	}
	return fmt.Sprintf("%d/%02d", start, (start+1)%100)
}

// isUniqueViolation reports whether err is a PostgreSQL unique-violation,
// which callers translate to ErrNameCollision / ErrAdjacencyMismatch.
func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		return pgErr.Code == "23505"
	}
	return false
}

// recordSeedEvent appends one world.events row inside the caller's transaction
// and, when a bus is wired, enqueues its dispatch job in the same tx (the
// transactional outbox, OPD-23). It stamps the world's real current_tick
// (competition events carry the progression tick the seeding/rollover happens
// on, not a hardcoded 0), so replay and downstream consumers see a truthful
// timeline. Errors abort the enclosing tx — a swallowed write here previously
// hid broken dispatch for an entire pyramid.
func (s *Service) recordSeedEvent(ctx context.Context, tx pgx.Tx, ev *eventbus.Event) error {
	var tick int64
	if err := tx.QueryRow(ctx,
		`SELECT current_tick FROM world.worlds WHERE id = $1`, ev.WorldID).Scan(&tick); err != nil {
		return fmt.Errorf("load world tick for %s event: %w", ev.EventType, err)
	}
	if ev.ActorType == nil {
		actor := "system"
		ev.ActorType = &actor
	}
	ev.WorldTick = tick
	return eventbus.WriteTx(ctx, s.bus, tx, ev)
}
