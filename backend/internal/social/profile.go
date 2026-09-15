package social

import (
	"time"

	"github.com/google/uuid"
)

// ClubRef is the compact club identity shown on a manager profile.
type ClubRef struct {
	ID      uuid.UUID `json:"id"`
	Name    string    `json:"name"`
	Country string    `json:"country"`
}

// CareerSummary aggregates the manager's managed matches (W-D-L + goals)
// across all their clubs, derived from manager_history × completed fixtures.
type CareerSummary struct {
	Matches      int `json:"matches"`
	Wins         int `json:"wins"`
	Draws        int `json:"draws"`
	Losses       int `json:"losses"`
	GoalsFor     int `json:"goals_for"`
	GoalsAgainst int `json:"goals_against"`
}

// Trophy is one club.club_history trophy attributed to a club the manager ran.
type Trophy struct {
	Season      int       `json:"season"`
	Description string    `json:"description"`
	ClubName    string    `json:"club_name"`
	WonAt       time.Time `json:"won_at"`
}

// H2HRecord is the head-to-head between the viewing manager's club and the
// profile manager's club over completed fixtures, framed from the viewer's side.
type H2HRecord struct {
	Matches      int `json:"matches"`
	Wins         int `json:"wins"`
	Draws        int `json:"draws"`
	Losses       int `json:"losses"`
	GoalsFor     int `json:"goals_for"`
	GoalsAgainst int `json:"goals_against"`
}

// RivalEdge is one relationship graph edge attached to the manager (manager↔
// manager) or their active club (club↔club), shown on the profile. Club edges
// are the full rivalry truth for AI-managed clubs; human managers additionally
// see their personal edges (S06-04c).
type RivalEdge struct {
	RelationshipType string    `json:"relationship_type"`
	EntityType       string    `json:"entity_type"`
	EntityID         uuid.UUID `json:"entity_id"`
	EntityName       string    `json:"entity_name"`
	Strength         int       `json:"strength"`
	Trust            int       `json:"trust"`
	Sentiment        int       `json:"sentiment"`
}

// Profile is the assembled manager profile page (S06-04a).
type Profile struct {
	ID          uuid.UUID     `json:"id"`
	Name        string        `json:"name"`
	Status      string        `json:"status"`
	IsPolicyBot bool          `json:"is_policy_bot"`
	ActiveClub  *ClubRef      `json:"active_club"`
	Career      CareerSummary `json:"career"`
	Trophies    []Trophy      `json:"trophies"`
	TrustScore  int           `json:"trust_score"`
	H2HVsViewer *H2HRecord    `json:"h2h_vs_viewer"`
	Rivalries   []RivalEdge   `json:"rivalries"`
}
