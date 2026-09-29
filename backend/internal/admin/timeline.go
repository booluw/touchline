package admin

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
)

// ---------------------------------------------------------------------------
// Timeline
// ---------------------------------------------------------------------------

// Timeline returns the country's recent events. Payload-relevant ids are
// matched against the country's clubs and player population (origin pool or
// current club), so a transfer between two foreign clubs of an event whose
// player is English-origin still lands in England's feed.
func (s *Service) Timeline(ctx context.Context, worldID, countryID uuid.UUID, days int, limit int) (*Timeline, error) {
	country, err := s.ResolveCountry(ctx, worldID, countryID)
	if err != nil {
		return nil, err
	}
	if days <= 0 {
		days = 14
	}
	if limit <= 0 || limit > 500 {
		limit = 200
	}
	since := time.Now().UTC().AddDate(0, 0, -days)

	clubIDs, err := s.CountryClubIDs(ctx, worldID, countryID)
	if err != nil {
		return nil, err
	}
	relevant := map[uuid.UUID]bool{countryID: true}
	for _, id := range clubIDs {
		relevant[id] = true
	}
	if err := s.markPlayerRelevance(ctx, worldID, countryID, clubIDs, relevant); err != nil {
		return nil, err
	}

	names := s.nameMaps(ctx, worldID, clubIDs, relevant)

	rows, err := s.pool.Query(ctx, `
		SELECT id, event_type, occurred_at, actor_type, payload
		FROM world.events
		WHERE world_id = $1
		  AND event_type IN ('TRANSFER_COMPLETED','BID_PLACED','BID_ACCEPTED','BID_COUNTERED',
		                         'PLAYER_SIGNED','PLAYER_RELEASED','PLAYER_CLAIMED_FROM_POOL',
		                         'COUNTRY_ACADEMY_INTAKE','PLAYER_RETIRED','PLAYER_LISTED',
		                         'PLAYER_LISTING_WITHDRAWN','CLUB_RENAMED')
		  AND occurred_at >= $2
		ORDER BY occurred_at DESC
		LIMIT $3`, worldID, since, limit)
	if err != nil {
		return nil, fmt.Errorf("admin: timeline: %w", err)
	}
	defer rows.Close()

	tl := &Timeline{Country: country, Since: since, Items: []TimelineItem{}}
	for rows.Next() {
		var it TimelineItem
		var actorType *string
		var raw []byte
		if err := rows.Scan(&it.ID, &it.EventType, &it.OccurredAt, &actorType, &raw); err != nil {
			return nil, fmt.Errorf("admin: scan timeline: %w", err)
		}
		it.ActorType = actorType
		_ = json.Unmarshal(raw, &it.Payload)
		if !referencesCountry(it.Payload, relevant) {
			continue
		}
		it.Title = headlineFor(it.EventType, it.Payload, names)
		tl.Items = append(tl.Items, it)
	}
	return tl, rows.Err()
}

func (s *Service) markPlayerRelevance(ctx context.Context, worldID, countryID uuid.UUID, clubIDs []uuid.UUID, relevant map[uuid.UUID]bool) error {
	rows, err := s.pool.Query(ctx, `
		SELECT id FROM player.players
		WHERE world_id = $1 AND (country_id = $2 OR club_id = ANY($3))`,
		worldID, countryID, clubIDs)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			return err
		}
		relevant[id] = true
	}
	return rows.Err()
}

type nameMaps struct {
	players map[uuid.UUID]string
	clubs   map[uuid.UUID]string
}

func (s *Service) nameMaps(ctx context.Context, worldID uuid.UUID, clubIDs []uuid.UUID, relevant map[uuid.UUID]bool) nameMaps {
	nm := nameMaps{players: map[uuid.UUID]string{}, clubs: map[uuid.UUID]string{}}
	rows, err := s.pool.Query(ctx, `SELECT id, short_name FROM club.clubs WHERE id = ANY($1)`, clubIDs)
	if err == nil {
		for rows.Next() {
			var id uuid.UUID
			var name string
			if err := rows.Scan(&id, &name); err == nil {
				nm.clubs[id] = name
			}
		}
		rows.Close()
	}
	rows, err = s.pool.Query(ctx, `
		SELECT p.id, pe.display_name
		FROM player.players p JOIN person.people pe ON pe.id = p.person_id
		WHERE p.world_id = $1 AND p.id = ANY($2)`, worldID, keysToSlice(relevant))
	if err == nil {
		defer rows.Close()
		for rows.Next() {
			var id uuid.UUID
			var name string
			if err := rows.Scan(&id, &name); err == nil {
				nm.players[id] = name
			}
		}
	}
	return nm
}

func keysToSlice(set map[uuid.UUID]bool) []uuid.UUID {
	out := make([]uuid.UUID, 0, len(set))
	for id := range set {
		out = append(out, id)
	}
	return out
}

// referencesCountry reports whether any uuid-looking payload value matches a
// country-relevant entity (club, player, or the country itself).
func referencesCountry(payload map[string]any, relevant map[uuid.UUID]bool) bool {
	for _, v := range payload {
		switch t := v.(type) {
		case string:
			if id, err := uuid.Parse(t); err == nil && relevant[id] {
				return true
			}
		case []any:
			for _, e := range t {
				if s, ok := e.(string); ok {
					if id, err := uuid.Parse(s); err == nil && relevant[id] {
						return true
					}
				}
			}
		}
	}
	return false
}

// headlineFor renders a readable one-liner from the payload and known names.
func headlineFor(eventType string, payload map[string]any, names nameMaps) string {
	first := func(keys ...string) string {
		for _, k := range keys {
			if raw, ok := payload[k]; ok {
				if s, ok := raw.(string); ok {
					if id, err := uuid.Parse(s); err == nil {
						if n, ok := names.players[id]; ok {
							return n
						}
						if n, ok := names.clubs[id]; ok {
							return n
						}
					}
				}
			}
		}
		return ""
	}
	str := func(key string) string {
		if raw, ok := payload[key]; ok {
			if s, ok := raw.(string); ok {
				return s
			}
		}
		return ""
	}
	players := first("player_id", "player_ids")
	clubs := first("to_club_id", "from_club_id", "club_id", "selling_club_id", "bidding_club_id", "listing_club_id")

	switch eventType {
	case "TRANSFER_COMPLETED":
		return joinNames(players, clubs)
	case "CLUB_RENAMED":
		oldN := str("old_name")
		newN := str("new_name")
		if oldN != "" && newN != "" {
			return fmt.Sprintf("%s renamed to %s", oldN, newN)
		}
		return eventType
	case "PLAYER_SIGNED", "BID_PLACED", "BID_ACCEPTED", "BID_COUNTERED", "PLAYER_RELEASED",
		"PLAYER_CLAIMED_FROM_POOL", "PLAYER_LISTED", "PLAYER_LISTING_WITHDRAWN":
		return fmt.Sprintf("%s: %s", eventType, joinNames(players, clubs))
	default:
		if players != "" {
			return fmt.Sprintf("%s: %s", eventType, players)
		}
		return eventType
	}
}

func joinNames(a, b string) string {
	switch {
	case a == "" && b == "":
		return ""
	case a == "":
		return b
	case b == "":
		return a
	default:
		return a + " / " + b
	}
}
