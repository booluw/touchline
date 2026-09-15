// Package board owns the S06-02 board system: structured per-club mandates,
// the weekly confidence scoring that fills manager.job_security_snapshots with
// explainable factor breakdowns, manager-side negotiation of sporting targets,
// and the sacking guard that hands a failing human-managed club back to AI
// control via manager.Service (MANAGER_SACKED).
//
// All numbers are proposal constants in numerics.go (source of truth:
// docs/design/board-numerics.md); recalibration is a data-only change.
package board

import (
	"time"

	"github.com/google/uuid"
)

// Persona is the stored board personality (club.boards.personality_type).
// Weights and negotiation tolerance differ per persona (numerics.go).
type Persona string

const (
	PersonaPatientOwner          Persona = "patient_owner"
	PersonaDemandingOwner        Persona = "demanding_owner"
	PersonaFinancialConservative Persona = "financial_conservative"
	PersonaAcademyOwner          Persona = "academy_owner"
	PersonaPrestigeOwner         Persona = "prestige_owner"
	PersonaPoliticalBoard        Persona = "political_board"
)

// Mandate categories stored on club.board_mandates.
const (
	CatPrimary   = "primary"
	CatSecondary = "secondary"
	CatStrategic = "strategic"
	CatFinancial = "financial"
)

// Mandate states on club.board_mandates.
const (
	MandatePending = "pending"
	MandateAgreed  = "agreed"
	MandateMet     = "met"
	MandateBroken  = "broken"
)

// Mandate target types the scoring engine can actually evaluate this slice.
// Sporting targets are negotiable; financial structure is fixed (see the
// numerics doc for the limits of the MVP evaluation).
const (
	TargetLeagueFinish  = "league_finish"     // position (smaller = better)
	TargetPointsTarget  = "points_target"     // season points
	TargetWageStructure = "wage_structure"    // +percent points of wage budget tolerated
	TargetOperatingBank = "operating_balance" // 0 = break-even requirement
)

// Mandate is one structured board target row.
type Mandate struct {
	ID          uuid.UUID  `json:"id"`
	ClubID      uuid.UUID  `json:"club_id"`
	ManagerID   uuid.UUID  `json:"manager_id"`
	Season      int        `json:"season"`
	Category    string     `json:"category"`
	Description string     `json:"description"`
	TargetType  string     `json:"target_type"`
	TargetValue string     `json:"target_value"`
	Status      string     `json:"status"`
	CreatedAt   time.Time  `json:"created_at"`
	ResolvedAt  *time.Time `json:"resolved_at,omitempty"`
}

// FactorScores is one snapshotted factor breakdown, mirroring the
// manager.job_security_snapshots columns. All scores are 0-100.
type FactorScores struct {
	Performance        int `json:"performance_score"`
	Expectations       int `json:"expectations_score"`
	Financial          int `json:"financial_score"`
	BoardRelationship  int `json:"board_relationship_score"`
	ClubDNAAlignment   int `json:"club_dna_alignment_score"`
	SupporterSentiment int `json:"supporter_sentiment_score"`
	Alternatives       int `json:"alternatives_score"`
	Total              int `json:"total_score"`
}

// Snapshot is one job-security snapshot row plus its tick.
type Snapshot struct {
	ManagerID   uuid.UUID    `json:"manager_id"`
	ClubID      uuid.UUID    `json:"club_id"`
	WorldTick   int64        `json:"world_tick"`
	Scores      FactorScores `json:"scores"`
	Explanation []byte       `json:"-"`
}

// View is the manager-facing board read model (GET /api/managers/me/board):
// the current confidence number, its factor breakdown and explanation, and the
// open + recently resolved mandates.
type View struct {
	Confidence  int            `json:"confidence"`
	Snapshot    *Snapshot      `json:"snapshot"`
	Explanation map[string]any `json:"explanation"`
	Mandates    []Mandate      `json:"mandates"`
}

// Actor identifies the caller of a board mutation (mirrors finance.Actor).
type Actor struct {
	ManagerID   uuid.UUID
	IsPolicyBot bool
}

// NegotiateInput is the bounded sporting-target proposal.
type NegotiateInput struct {
	MandateID   uuid.UUID `json:"-"`
	TargetValue string    `json:"target_value"`
}
