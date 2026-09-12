package club

import "github.com/google/uuid"

type Club struct {
	ID        uuid.UUID `json:"id"`
	WorldID   uuid.UUID `json:"world_id"`
	Name      string    `json:"name"`
	ShortName string    `json:"short_name"`
}

type ClubDNA struct {
	ID                    uuid.UUID `json:"id"`
	ClubID                uuid.UUID `json:"club_id"`
	CompetitiveAmbition   int       `json:"competitive_ambition"`
	FinancialPhilosophy   string    `json:"financial_philosophy"`
	RecruitmentPhilosophy string    `json:"recruitment_philosophy"`
	AcademyImportance     int       `json:"academy_importance"`
	Patience              int       `json:"patience"`
	ManagerialControl     int       `json:"managerial_control"`
	StarPowerPreference   int       `json:"star_power_preference"`
	WageTolerance         int       `json:"wage_tolerance"`
	SellingPhilosophy     string    `json:"selling_philosophy"`
	TacticalIdentity      string    `json:"tactical_identity"`
	CulturalIdentity      string    `json:"cultural_identity"`
}

type Board struct {
	ID      uuid.UUID `json:"id"`
	ClubID  uuid.UUID `json:"club_id"`
	WorldID uuid.UUID `json:"world_id"`
}

type BoardMandate struct {
	ID          uuid.UUID `json:"id"`
	BoardID     uuid.UUID `json:"board_id"`
	Category    string    `json:"category"` // primary, secondary, strategic, financial
	Description string    `json:"description"`
	Status      string    `json:"status"` // pending, agreed, met, broken
}
