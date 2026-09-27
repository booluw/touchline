// World clock: the fixed-scale continuous clock (IM16). The world's current
// moment is a pure function of real wall-clock time and the world's
// tick.day_length scale: epochMidnight (the UTC midnight of the world's
// launch/creation date) plus real elapsed time stretched to tick.day_length
// real seconds per game-day. At the default 1:1 scale the world clock tracks
// UTC exactly, so a fixture timed "20:00" simulates at 20:00 UTC — the
// kickoff time is real, not cosmetic. The scheduler converges the world's day
// counter to this scale (TargetDay) and the matchday runner gates kickoffs on
// the continuous moment (ScaleNow), so matches play at their scheduled time on
// the day their calendar reaches them.
package world

import (
	"context"
	"encoding/json"
	"time"

	"github.com/jackc/pgx/v5"
)

// DefaultDayLength is the world clock's default fixed scale: 86400 real
// seconds per game-day, i.e. one game-day per real day. At this scale
// ScaleNow(now) == now (UTC) and kickoff hours are literal UTC hours.
const DefaultDayLength = 24 * time.Hour

// rowQueryer is the subset of *pgxpool.Pool / pgx.Tx the clock basis needs.
type rowQueryer interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}

// LoadScale reads a world's fixed-clock basis: whether it is currently
// playable, its epoch (the UTC midnight of COALESCE(launched_at, created_at)),
// and its tick.day_length scale in real seconds per game-day (default
// DefaultDayLength when the config row is absent or non-numeric). A world that
// does not exist is reported as a pgx.ErrNoRows error.
func LoadScale(ctx context.Context, q rowQueryer, worldID any) (playable bool, epoch time.Time, dayLength time.Duration, err error) {
	var status string
	var raw []byte
	err = q.QueryRow(ctx, `
		SELECT w.status,
		       date_trunc('day', COALESCE(w.launched_at, w.created_at)),
		       (SELECT c.config_value FROM world.world_config c
		        WHERE c.world_id = w.id AND c.config_key = 'tick.day_length')
		FROM world.worlds w WHERE w.id = $1`, worldID).Scan(&status, &epoch, &raw)
	if err != nil {
		return false, time.Time{}, 0, err
	}
	dayLength = DefaultDayLength
	if len(raw) > 0 {
		var secs int
		if json.Unmarshal(raw, &secs) == nil && secs > 0 {
			dayLength = time.Duration(secs) * time.Second
		}
	}
	return status == "active" || status == "open_beta", epoch, dayLength, nil
}

// gameDays returns the game-days (integer plus fraction) elapsed since the
// world's epoch midnight at the fixed scale. It is the single place the scale
// is applied, so ScaleNow, TargetDay and every caller agree on the mapping.
func gameDays(realNow, epochMidnight time.Time, dayLength time.Duration) float64 {
	if dayLength <= 0 {
		dayLength = DefaultDayLength
	}
	realDays := realNow.Sub(epochMidnight).Seconds() / (24 * 3600)
	if realDays < 0 {
		return 0
	}
	return realDays * (24 * 3600) / dayLength.Seconds()
}

// ScaleNow maps a real wall-clock instant to the world's continuous current
// moment (IM16). With the default tick.day_length it equals real UTC time.
func ScaleNow(realNow, epochMidnight time.Time, dayLength time.Duration) time.Time {
	return epochMidnight.Add(time.Duration(gameDays(realNow, epochMidnight, dayLength)*86400) * time.Second)
}

// TargetDay is the game-day the world clock has reached at the fixed scale:
// floor of the scaled days elapsed since the world's epoch midnight. The
// scheduler converges world.worlds.current_day toward TargetDay, so the
// integer calendar and the continuous clock never drift apart.
func TargetDay(realNow, epochMidnight time.Time, dayLength time.Duration) int64 {
	return int64(gameDays(realNow, epochMidnight, dayLength))
}
