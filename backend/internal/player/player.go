package player

import "github.com/google/uuid"

type Player struct {
	ID              uuid.UUID  `json:"id"`
	WorldID         uuid.UUID  `json:"world_id"`
	ClubID          *uuid.UUID `json:"club_id"`
	PersonID        uuid.UUID  `json:"person_id"`
	FirstName       string     `json:"first_name"`
	LastName        string     `json:"last_name"`
	DisplayName     string     `json:"display_name"`
	Nationality     string     `json:"nationality"`
	DateOfBirth     string     `json:"date_of_birth"`
	PrimaryPosition string     `json:"primary_position"`
	SquadNumber     *int       `json:"squad_number"`
}

type PlayerAttributes struct {
	ID       uuid.UUID `json:"id"`
	PlayerID uuid.UUID `json:"player_id"`
}

type PlayerPersonality struct {
	ID                  uuid.UUID `json:"id"`
	PlayerID            uuid.UUID `json:"player_id"`
	Professionalism     int       `json:"professionalism"`
	Ambition            int       `json:"ambition"`
	Loyalty             int       `json:"loyalty"`
	Ego                 int       `json:"ego"`
	Sociability         int       `json:"sociability"`
	Adaptability        int       `json:"adaptability"`
	Patience            int       `json:"patience"`
	Leadership          int       `json:"leadership"`
	EmotionalVolatility int       `json:"emotional_volatility"`
}

type PlayerHiddenTraits struct {
	ID                   uuid.UUID `json:"id"`
	PlayerID             uuid.UUID `json:"player_id"`
	Potential            int       `json:"potential"`
	Consistency          int       `json:"consistency"`
	InjurySusceptibility int       `json:"injury_susceptibility"`
	Adaptability         int       `json:"adaptability"`
	Professionalism      int       `json:"professionalism"`
	Ambition             int       `json:"ambition"`
	Loyalty              int       `json:"loyalty"`
	Temperament          int       `json:"temperament"`
	PressureHandling     int       `json:"pressure_handling"`
	LearningSpeed        int       `json:"learning_speed"`
}

type EmotionalState struct {
	PlayerID uuid.UUID `json:"player_id"`
	State    string    `json:"state"` // happy, content, motivated, frustrated, anxious, angry, homesick, excited, betrayed, ambitious, confident, isolated
	Cause    string    `json:"cause"`
}
