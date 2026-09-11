package playergen

import "github.com/google/uuid"

// NationalityPool defines weightings for nationality distribution.
// Weights reflect realistic football-producing populations.
type NationalityPool struct {
	Entries []NationalityWeight `json:"entries"`
}

type NationalityWeight struct {
	Nationality string  `json:"nationality"`
	Weight      float64 `json:"weight"`
}

// PlayerFactory creates players with generated names and nationality-weighted attributes.
type PlayerFactory struct {
	nameGen  PlayerNameGenerator
	natPool  *NationalityPool
}

func NewPlayerFactory(nameGen PlayerNameGenerator, natPool *NationalityPool) *PlayerFactory {
	return &PlayerFactory{
		nameGen: nameGen,
		natPool: natPool,
	}
}

func (f *PlayerFactory) CreatePlayer(worldID uuid.UUID) (firstName, lastName, nationality string) {
	nationality = f.natPool.WeightedRandom()
	firstName, lastName = f.nameGen.GenerateFullName(nationality)
	return
}

// WeightedRandom selects a nationality based on weights.
// Placeholder: needs weighted random implementation.
func (p *NationalityPool) WeightedRandom() string {
	if len(p.Entries) == 0 {
		return "unknown"
	}
	// TODO: implement weighted random selection
	return p.Entries[0].Nationality
}
