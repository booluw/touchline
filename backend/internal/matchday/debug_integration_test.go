//go:build integration

package matchday

import (
	"context"
	"fmt"
	"testing"

	"github.com/touchline/backend/internal/form"
	"github.com/touchline/backend/internal/match"
	"github.com/touchline/backend/internal/squad"
)

func TestDebugWorldDate(t *testing.T) {
	pool, worldID, compSvc := runnerWorld(t)
	ctx := context.Background()
	matches := match.NewService(pool, nil, squad.NewStore(pool), form.NewStore(pool))
	r := NewRunner(pool, matches, compSvc)

	asOf, err := r.worldDate(ctx, worldID)
	if err != nil {
		t.Fatalf("worldDate: %v", err)
	}
	fmt.Printf("DEBUG asOf=%s\n", asOf)

	var (
		launched, created time.Time
		day               int64
		ref               string
	)
	if err := pool.QueryRow(ctx,
		`SELECT launched_at, created_at, current_day FROM world.worlds WHERE id = $1`, worldID).
		Scan(&launched, &created, &day); err != nil {
		t.Fatalf("world row: %v", err)
	}
	fmt.Printf("DEBUG launched=%s created=%s current_day=%d (TZ %s)\n", launched, created, day, launched.Location())
	if err := pool.QueryRow(ctx, `SELECT current_setting('TimeZone')`).Scan(&ref); err != nil {
		t.Fatalf("tz: %v", err)
	}
	fmt.Printf("DEBUG db TimeZone=%s now()=%s\n", ref, time.Now().Format(time.RFC3339))
	if err := pool.QueryRow(ctx, `SELECT now()`).Scan(&launched); err != nil {
		t.Fatalf("now: %v", err)
	}
	fmt.Printf("DEBUG db now()=%s (UTC %s)\n", launched, launched.UTC())

	var n int
	if err := pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM match.fixtures WHERE world_id = $1 AND status='scheduled'`, worldID).Scan(&n); err != nil {
		t.Fatalf("count: %v", err)
	}
	fmt.Printf("DEBUG scheduled fixtures=%d\n", n)

	var minDate, maxDate string
	if err := pool.QueryRow(ctx, `
		SELECT min(scheduled_at)::date::text, max(scheduled_at)::date::text
		FROM match.fixtures WHERE world_id = $1`, worldID).Scan(&minDate, &maxDate); err != nil {
		t.Fatalf("dates: %v", err)
	}
	fmt.Printf("DEBUG fixture dates min=%s max=%s\n", minDate, maxDate)

	if _, err := pool.Exec(ctx,
		`UPDATE world.worlds SET current_day = current_day + 1 WHERE id = $1`, worldID); err != nil {
		t.Fatalf("advance: %v", err)
	}
	asOf, err = r.worldDate(ctx, worldID)
	if err != nil {
		t.Fatalf("worldDate2: %v", err)
	}
	fmt.Printf("DEBUG asOf(day+1)=%s\n", asOf)

	md, err := r.dueMatchdays(ctx, worldID, asOf)
	if err != nil {
		t.Fatalf("due: %v", err)
	}
	fmt.Printf("DEBUG dueMatchdays=%v\n", md)
}
