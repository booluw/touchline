package form

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Store persists FormState rows (club.form_state) and reads the world tick.
// Get/Update are the whole surface: an upsert keeps first-time and existing
// clubs on one code path.
type Store struct {
	pool *pgxpool.Pool
}

// NewStore builds the form store.
func NewStore(pool *pgxpool.Pool) *Store {
	return &Store{pool: pool}
}

// Get loads a club's form state. ok is false when the club was never recorded
// (the caller treats that as form.Neutral). Only DB-level failures return an
// error.
func (s *Store) Get(ctx context.Context, clubID uuid.UUID) (FormState, bool, error) {
	var f FormState
	err := s.pool.QueryRow(ctx, `
		SELECT club_id, current_rating, last_updated_tick, COALESCE(form_string, '')
		FROM club.form_state WHERE club_id = $1`, clubID).
		Scan(&f.ClubID, &f.CurrentRating, &f.LastUpdatedTick, &f.FormString)
	if errors.Is(err, pgx.ErrNoRows) {
		return FormState{}, false, nil
	}
	if err != nil {
		return FormState{}, false, fmt.Errorf("get form state: %w", err)
	}
	return f, true, nil
}

// Update upserts a club's form state row.
func (s *Store) Update(ctx context.Context, f FormState) error {
	if _, err := s.pool.Exec(ctx, `
		INSERT INTO club.form_state (club_id, current_rating, last_updated_tick, form_string)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (club_id) DO UPDATE SET
			current_rating    = EXCLUDED.current_rating,
			last_updated_tick = EXCLUDED.last_updated_tick,
			form_string       = EXCLUDED.form_string`,
		f.ClubID, f.CurrentRating, f.LastUpdatedTick, f.FormString); err != nil {
		return fmt.Errorf("upsert form state: %w", err)
	}
	return nil
}

// WorldTick returns a world's current_tick for LastUpdatedTick stamping, so a
// saved world always replays the identical form timeline.
func (s *Store) WorldTick(ctx context.Context, worldID uuid.UUID) (int64, error) {
	var tick int64
	if err := s.pool.QueryRow(ctx,
		`SELECT current_tick FROM world.worlds WHERE id = $1`, worldID).Scan(&tick); err != nil {
		return 0, fmt.Errorf("read world tick: %w", err)
	}
	return tick, nil
}