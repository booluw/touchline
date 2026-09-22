package club

import (
	"context"
	"errors"
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
}

// Service reads club state. Reads only — club creation is owned by the
// S03-01 bootstrap orchestrator (internal/bootstrap).
type Service struct {
	pool *pgxpool.Pool
}

// NewService builds the club read service.
func NewService(pool *pgxpool.Pool) *Service {
	return &Service{pool: pool}
}

// GetClub fetches a single club.
func (s *Service) GetClub(ctx context.Context, id uuid.UUID) (*Club, error) {
	var c Club
	err := s.pool.QueryRow(ctx, `
		SELECT id, world_id, name, short_name FROM club.clubs WHERE id = $1`, id,
	).Scan(&c.ID, &c.WorldID, &c.Name, &c.ShortName)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrClubNotFound
	}
	if err != nil {
		return nil, err
	}
	return &c, nil
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
		countryCD string
	)
	err := s.pool.QueryRow(ctx, `
		SELECT c.id, c.world_id, c.name, c.short_name, c.country, c.is_ai_controlled,
		       wc.id, wc.code
		FROM club.clubs c
		LEFT JOIN world.countries wc ON wc.world_id = c.world_id AND lower(wc.name) = lower(c.country)
		WHERE c.id = $1`, clubID,
	).Scan(&d.ID, &d.WorldID, &d.Name, &d.ShortName, &d.Country, &d.IsAIControlled,
		&countryID, &countryCD)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrClubNotFound
	}
	if err != nil {
		return nil, err
	}
	if countryID != uuid.Nil {
		d.CountryRef = &apiref.CountryRef{ID: countryID, Name: d.Country, Code: countryCD}
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
	return &d, nil
}

// GetClubSquad returns the club's players joined to their person + nationality.
func (s *Service) GetClubSquad(ctx context.Context, clubID uuid.UUID) ([]SquadPlayer, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT p.id, p.person_id, pe.first_name, pe.last_name, pe.display_name,
		       pe.nationality_code, n.name,
		       pe.date_of_birth,
		       p.primary_position, p.squad_number,
		       wc.id, wc.code
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
		var countryCD string
		if err := rows.Scan(&sp.ID, &sp.PersonID, &sp.FirstName, &sp.LastName, &sp.DisplayName,
			&sp.NationalityCode, &sp.NationalityName, &dob, &sp.PrimaryPosition, &sp.SquadNumber,
			&countryID, &countryCD); err != nil {
			return nil, err
		}
		sp.DateOfBirth = dob
		sp.Age = ageAt(dob, time.Now())
		if sp.NationalityCode != "" {
			sp.Nationality = &apiref.CountryRef{Code: sp.NationalityCode, Name: sp.NationalityName}
			if countryID != uuid.Nil {
				sp.Nationality.ID = countryID
				sp.Nationality.Code = countryCD
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

// GetClubDNA fetches a club's DNA row (empty when unmodelled).
func (s *Service) GetClubDNA(ctx context.Context, clubID uuid.UUID) (*ClubDNA, error) {
	return &ClubDNA{ClubID: clubID}, nil
}

// GetBoardMandates lists the club's mandates (targets land with S06-02).
func (s *Service) GetBoardMandates(ctx context.Context, boardID uuid.UUID) ([]*BoardMandate, error) {
	return nil, nil
}
