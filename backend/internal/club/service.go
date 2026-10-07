package club

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/touchline/backend/pkg/apiref"
)

// Sentinel errors. Handlers map these to HTTP status codes.
var ErrClubNotFound = errors.New("club not found")

// SquadPlayer is a squad member as served to an authorized client (S03-01).
// The nationality code+name stay internal; the wire carries a country ref.
type SquadPlayer struct {
	ID              uuid.UUID          `json:"id"`
	PersonID        uuid.UUID          `json:"person_id"`
	FirstName       string             `json:"first_name"`
	LastName        string             `json:"last_name"`
	DisplayName     string             `json:"display_name"`
	NationalityCode string             `json:"-"`
	NationalityName string             `json:"-"`
	Nationality     *apiref.CountryRef `json:"nationality"`
	DateOfBirth     time.Time          `json:"date_of_birth"`
	Age             int                `json:"age"`
	PrimaryPosition string             `json:"primary_position"`
	SquadNumber     *int               `json:"squad_number"`
}

// ManagerRef names the club's current manager without leaking account data.
type ManagerRef struct {
	ID          uuid.UUID `json:"id"`
	IsPolicyBot bool      `json:"is_policy_bot"`
}

// ClubDetail is a club with its current manager and full squad. The country
// string (a name stored on club.clubs) stays internal; the wire carries a
// world.countries ref when one is name-matchable, else a name-only ref.
type ClubDetail struct {
	ID             uuid.UUID          `json:"id"`
	WorldID        uuid.UUID          `json:"world_id"`
	Name           string             `json:"name"`
	ShortName      string             `json:"short_name"`
	Country        string             `json:"-"`
	CountryRef     *apiref.CountryRef `json:"country"`
	IsAIControlled bool               `json:"is_ai_controlled"`
	Manager        *ManagerRef        `json:"manager,omitempty"`
	Squad          []SquadPlayer      `json:"squad"`

	// Profile fields (IM41). Nullable columns stay null when unseeded.
	FoundedYear    *int              `json:"founded_year"`
	City           *string           `json:"city"`
	Tier           int               `json:"tier"`
	Reputation     int               `json:"reputation"`
	PrimaryColor   *string           `json:"primary_color"`
	SecondaryColor *string           `json:"secondary_color"`
	Stadium        ClubStadium       `json:"stadium"`
	Facilities     []ClubFacility    `json:"facilities"`
	History        []ClubHistoryItem `json:"history"`
}

// ClubStadium is the club's ground (IM41).
type ClubStadium struct {
	Name     *string `json:"name"`
	Capacity *int    `json:"capacity"`
}

// ClubFacility is one club.facilities row (IM41).
type ClubFacility struct {
	Type       string    `json:"type"`
	Level      int       `json:"level"`
	UpgradedAt time.Time `json:"upgraded_at"`
}

// ClubHistoryItem is one club.club_history row (IM41).
type ClubHistoryItem struct {
	Season      int       `json:"season"`
	EventType   string    `json:"event_type"`
	Description string    `json:"description"`
	OccurredAt  time.Time `json:"occurred_at"`
}

// clubHistoryLimit caps the history list on the club page (IM41).
const clubHistoryLimit = 10

// Service reads club state. Reads only — club creation is owned by the
// S03-01 bootstrap orchestrator (internal/bootstrap).
type Service struct {
	pool *pgxpool.Pool
}

// NewService builds the club read service.
func NewService(pool *pgxpool.Pool) *Service {
	return &Service{pool: pool}
}

// ListClubs returns the clubs in a world, alphabetically.
func (s *Service) ListClubs(ctx context.Context, worldID uuid.UUID) ([]*Club, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, world_id, name, short_name FROM club.clubs
		WHERE world_id = $1 ORDER BY name`, worldID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []*Club
	for rows.Next() {
		var c Club
		if err := rows.Scan(&c.ID, &c.WorldID, &c.Name, &c.ShortName); err != nil {
			return nil, err
		}
		out = append(out, &c)
	}
	return out, rows.Err()
}

// GetClubDetail returns a club with its current manager and squad.
func (s *Service) GetClubDetail(ctx context.Context, clubID uuid.UUID) (*ClubDetail, error) {
	var d ClubDetail
	var (
		countryID uuid.UUID
		countryCD *string // NULL when the club's country isn't a world country
	)
	err := s.pool.QueryRow(ctx, `
		SELECT c.id, c.world_id, c.name, c.short_name, c.country, c.is_ai_controlled,
		       wc.id, wc.code,
		       c.founded_year, c.city, c.tier, c.reputation, c.primary_color, c.secondary_color,
		       c.stadium_name, c.stadium_capacity
		FROM club.clubs c
		LEFT JOIN world.countries wc ON wc.world_id = c.world_id AND lower(wc.name) = lower(c.country)
		WHERE c.id = $1`, clubID,
	).Scan(&d.ID, &d.WorldID, &d.Name, &d.ShortName, &d.Country, &d.IsAIControlled,
		&countryID, &countryCD,
		&d.FoundedYear, &d.City, &d.Tier, &d.Reputation, &d.PrimaryColor, &d.SecondaryColor,
		&d.Stadium.Name, &d.Stadium.Capacity)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrClubNotFound
	}
	if err != nil {
		return nil, err
	}
	if countryID != uuid.Nil && countryCD != nil {
		d.CountryRef = &apiref.CountryRef{ID: countryID, Name: d.Country, Code: *countryCD}
	} else {
		d.CountryRef = &apiref.CountryRef{Name: d.Country}
	}

	var (
		managerID uuid.UUID
		isBot     bool
	)
	err = s.pool.QueryRow(ctx, `
		SELECT m.id, m.is_policy_bot
		FROM manager.managers m
		JOIN club.clubs c ON c.current_manager_id = m.id
		WHERE c.id = $1`, clubID,
	).Scan(&managerID, &isBot)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		// No current manager (unusual; clubs always have one) — leave nil.
	case err != nil:
		return nil, err
	default:
		d.Manager = &ManagerRef{ID: managerID, IsPolicyBot: isBot}
	}

	squad, err := s.GetClubSquad(ctx, clubID)
	if err != nil {
		return nil, err
	}
	d.Squad = squad

	if d.Facilities, err = s.clubFacilities(ctx, clubID); err != nil {
		return nil, err
	}
	if d.History, err = s.clubHistory(ctx, clubID); err != nil {
		return nil, err
	}
	return &d, nil
}

func (s *Service) clubFacilities(ctx context.Context, clubID uuid.UUID) ([]ClubFacility, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT facility_type, level, upgraded_at
		FROM club.facilities WHERE club_id = $1
		ORDER BY facility_type`, clubID)
	if err != nil {
		return nil, fmt.Errorf("club facilities: %w", err)
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (ClubFacility, error) {
		var f ClubFacility
		err := r.Scan(&f.Type, &f.Level, &f.UpgradedAt)
		return f, err
	})
}

func (s *Service) clubHistory(ctx context.Context, clubID uuid.UUID) ([]ClubHistoryItem, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT season, event_type, description, occurred_at
		FROM club.club_history WHERE club_id = $1
		ORDER BY occurred_at DESC, id DESC
		LIMIT $2`, clubID, clubHistoryLimit)
	if err != nil {
		return nil, fmt.Errorf("club history: %w", err)
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (ClubHistoryItem, error) {
		var h ClubHistoryItem
		err := r.Scan(&h.Season, &h.EventType, &h.Description, &h.OccurredAt)
		return h, err
	})
}

// GetClubSquad returns the club's players joined to their person + nationality.
func (s *Service) GetClubSquad(ctx context.Context, clubID uuid.UUID) ([]SquadPlayer, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT p.id, p.person_id, pe.first_name, pe.last_name, pe.display_name,
		       pe.nationality_code, n.name,
		       pe.date_of_birth,
		       p.primary_position, p.squad_number,
		       wc.id, wc.code, world.world_date(c.world_id)
		FROM player.players p
		JOIN person.people pe ON pe.id = p.person_id
		LEFT JOIN ref.nationalities n ON n.code = pe.nationality_code
		JOIN club.clubs c ON c.id = p.club_id
		LEFT JOIN world.countries wc ON wc.world_id = c.world_id AND lower(wc.name) = lower(n.name)
		WHERE p.club_id = $1
		ORDER BY p.squad_number NULLS LAST, pe.display_name`, clubID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []SquadPlayer
	for rows.Next() {
		var sp SquadPlayer
		var dob time.Time
		var countryID uuid.UUID
		var countryCD *string // NULL for nationalities with no world country
		var worldDate time.Time
		if err := rows.Scan(&sp.ID, &sp.PersonID, &sp.FirstName, &sp.LastName, &sp.DisplayName,
			&sp.NationalityCode, &sp.NationalityName, &dob, &sp.PrimaryPosition, &sp.SquadNumber,
			&countryID, &countryCD, &worldDate); err != nil {
			return nil, err
		}
		sp.DateOfBirth = dob
		sp.Age = ageAt(dob, worldDate) // the world's calendar, not the server's (IM25)
		if sp.NationalityCode != "" {
			sp.Nationality = &apiref.CountryRef{Code: sp.NationalityCode, Name: sp.NationalityName}
			if countryID != uuid.Nil && countryCD != nil {
				sp.Nationality.ID = countryID
				sp.Nationality.Code = *countryCD
			}
		}
		out = append(out, sp)
	}
	return out, rows.Err()
}

func ageAt(dob, at time.Time) int {
	years := at.Year() - dob.Year()
	if after := dob.AddDate(years, 0, 0); after.After(at) {
		years--
	}
	if years < 0 {
		years = 0
	}
	return years
}
