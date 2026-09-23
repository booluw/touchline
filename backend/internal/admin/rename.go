package admin

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/touchline/backend/pkg/eventbus"
)

// Sentinels for the admin club-rename surface.
var (
	ErrClubNameRequired   = errors.New("a new club name is required")
	ErrRenameNewsRequired = errors.New("news headline and body are required for a club rename")
	ErrClubNameTaken      = errors.New("another club in this world already uses that name")
	ErrClubNotFound       = errors.New("club not found in world")
)

// RenameInput is the admin's club-rename declaration: the new name plus the
// news story that announces it (the change is never silent — it is published
// as a world news story the admin authors).
type RenameInput struct {
	NewName   string     `json:"name"`
	ShortName string     `json:"short_name,omitempty"` // '' derives the 3-letter abbreviation
	News      RenameNews `json:"news"`
}

// RenameNews is the admin-authored news story that announces a rename.
type RenameNews struct {
	Headline string `json:"headline"`
	Body     string `json:"body"`
}

// RenameResult reports what the transaction materialized.
type RenameResult struct {
	ClubID      uuid.UUID `json:"club_id"`
	OldName     string    `json:"old_name"`
	NewName     string    `json:"new_name"`
	ShortName   string    `json:"short_name"`
	NewsStoryID uuid.UUID `json:"news_story_id"`
}

// NewsStoryRow is one world.news_stories row as surfaced to the admin country
// page and the manager news feed.
type NewsStoryRow struct {
	ID             uuid.UUID  `json:"id"`
	WorldID        uuid.UUID  `json:"world_id"`
	Headline       string     `json:"headline"`
	Body           string     `json:"body"`
	Category       string     `json:"category"`
	RelatedEventID *uuid.UUID `json:"related_event_id,omitempty"`
	PublishedAt    time.Time  `json:"published_at"`
}

// RenameClub renames a country's club inside one transaction: the new identity
// is written, the auditable CLUB_RENAMED event is logged, and the admin's news
// story is published (all-or-nothing, so the news can never disagree with the
// state). The new name must be unique within the world.
func (s *Service) RenameClub(ctx context.Context, worldID, countryID, clubID uuid.UUID, in RenameInput) (*RenameResult, error) {
	in.NewName = strings.TrimSpace(in.NewName)
	in.News.Headline = strings.TrimSpace(in.News.Headline)
	in.News.Body = strings.TrimSpace(in.News.Body)
	if in.NewName == "" {
		return nil, ErrClubNameRequired
	}
	if in.News.Headline == "" || in.News.Body == "" {
		return nil, ErrRenameNewsRequired
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("admin: begin rename tx: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	var (
		oldName     string
		oldShort    string
		clubCountry string
		tick        int64
	)
	err = tx.QueryRow(ctx, `
		SELECT c.name, c.short_name, c.country, (SELECT current_tick FROM world.worlds WHERE id = c.world_id)
		FROM club.clubs c
		WHERE c.id = $1 AND c.world_id = $2
		FOR UPDATE`, clubID, worldID).Scan(&oldName, &oldShort, &clubCountry, &tick)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrClubNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("admin: load club for rename: %w", err)
	}

	var countryName string
	err = tx.QueryRow(ctx, `
		SELECT name FROM world.countries WHERE id = $1 AND world_id = $2`, countryID, worldID).Scan(&countryName)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrCountryNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("admin: load country for rename: %w", err)
	}
	if !strings.EqualFold(strings.TrimSpace(clubCountry), strings.TrimSpace(countryName)) {
		return nil, ErrClubNotFound // the club isn't in this country
	}

	var taken bool
	err = tx.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM club.clubs
			WHERE world_id = $1 AND lower(name) = lower($2) AND id <> $3
		)`, worldID, in.NewName, clubID).Scan(&taken)
	if err != nil {
		return nil, fmt.Errorf("admin: check club name uniqueness: %w", err)
	}
	if taken {
		return nil, ErrClubNameTaken
	}

	newShort := strings.TrimSpace(in.ShortName)
	if newShort == "" {
		newShort = abbreviation(in.NewName)
	}

	if _, err := tx.Exec(ctx,
		`UPDATE club.clubs SET name = $2, short_name = $3 WHERE id = $1`,
		clubID, in.NewName, newShort); err != nil {
		return nil, fmt.Errorf("admin: rename club: %w", err)
	}

	payload, err := json.Marshal(map[string]any{
		"club_id":        clubID,
		"old_name":       oldName,
		"new_name":       in.NewName,
		"old_short_name": oldShort,
		"new_short_name": newShort,
		"news_headline":  in.News.Headline,
	})
	if err != nil {
		return nil, fmt.Errorf("admin: marshal rename payload: %w", err)
	}
	ev := &eventbus.Event{
		WorldID:   worldID,
		WorldTick: tick,
		EventType: "CLUB_RENAMED",
		Payload:   payload,
	}
	if err := eventbus.WriteTx(ctx, s.pub, tx, ev); err != nil {
		return nil, fmt.Errorf("admin: record CLUB_RENAMED event: %w", err)
	}

	var newsID uuid.UUID
	err = tx.QueryRow(ctx, `
		INSERT INTO world.news_stories (world_id, headline, body, category, related_event_id)
		VALUES ($1, $2, $3, 'general', $4)
		RETURNING id`,
		worldID, in.News.Headline, in.News.Body, ev.ID).Scan(&newsID)
	if err != nil {
		return nil, fmt.Errorf("admin: publish news story: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("admin: commit rename: %w", err)
	}

	return &RenameResult{
		ClubID:      clubID,
		OldName:     oldName,
		NewName:     in.NewName,
		ShortName:   newShort,
		NewsStoryID: newsID,
	}, nil
}

// News lists a world's news stories, newest first. limit is capped by the
// caller (defaults to 30). A non-nil countryID restricts the feed to
// world-wide stories (NULL country_id) plus that country's own stories — the
// manager-feed scoping (IM05). A nil countryID returns everything.
func (s *Service) News(ctx context.Context, worldID uuid.UUID, countryID *uuid.UUID, limit int) ([]NewsStoryRow, error) {
	query := `
		SELECT id, world_id, headline, body, category, related_event_id, published_at
		FROM world.news_stories
		WHERE world_id = $1
		ORDER BY published_at DESC
		LIMIT ` + strconv.Itoa(limit)
	args := []any{worldID}
	if countryID != nil {
		query = `
			SELECT id, world_id, headline, body, category, related_event_id, published_at
			FROM world.news_stories
			WHERE world_id = $1 AND (country_id IS NULL OR country_id = $2)
			ORDER BY published_at DESC
			LIMIT ` + strconv.Itoa(limit)
		args = []any{worldID, *countryID}
	}
	rows, err := s.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("admin: news: %w", err)
	}
	defer rows.Close()

	stories := []NewsStoryRow{}
	for rows.Next() {
		var n NewsStoryRow
		if err := rows.Scan(&n.ID, &n.WorldID, &n.Headline, &n.Body, &n.Category, &n.RelatedEventID, &n.PublishedAt); err != nil {
			return nil, fmt.Errorf("admin: scan news: %w", err)
		}
		stories = append(stories, n)
	}
	return stories, rows.Err()
}

// abbreviation derives a default 3-letter display tag from a club name
// (mirrors the competition.short convention, kept local to avoid coupling).
func abbreviation(name string) string {
	clean := strings.TrimSpace(name)
	for _, token := range strings.Fields(clean) {
		switch token {
		case "FC", "SC", "AC", "United", "City", "CF", "CD", "NK", "HNK":
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
