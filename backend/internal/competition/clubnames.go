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
// fixture scheduling so matchdays land on whole days at KickoffHourUTC.
func daysTruncate(t time.Time) time.Time {
	y, m, d := t.Date()
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

func recordSeedEvent(ctx context.Context, tx pgx.Tx, ev *eventbus.Event) error {
	_, err := tx.Exec(ctx, `
		INSERT INTO world.events
			(world_id, world_tick, event_type, actor_type, payload, random_seed, occurred_at)
		VALUES ($1, 0, $2, 'system', $3, $4, now())`,
		ev.WorldID, ev.EventType, slice(ev.Payload), coalesceSeed(ev.RandomSeed))
	if err != nil {
		return fmt.Errorf("record %s event: %w", ev.EventType, err)
	}
	return nil
}

func slice(b []byte) []byte {
	if b == nil {
		return []byte("{}")
	}
	return b
}

func coalesceSeed(seed *int64) any {
	if seed == nil {
		return int64(0)
	}
	return *seed
}
