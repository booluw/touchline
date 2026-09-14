package finance

import "math"

// Position-base weekly wage schedule (finance-numerics.md). The wage budget
// is calibrated to sum-of-squad for a 24-player first-team.
var positionBase = map[string]int64{
	"GK": 9000, "CB": 8000, "LB": 7000, "RB": 7000,
	"DM": 7500, "CM": 8500, "AM": 8000, "LM": 7500,
	"RM": 7500, "LW": 8000, "RW": 8000, "ST": 9500,
}

// WeeklyWage is the deterministic weekly wage of a player with the given
// position and attribute mean. It is monotonic in the attribute mean and
// stays within reasonable bounds for a generated squad.
func WeeklyWage(position string, attrMean int) int64 {
	base, ok := positionBase[position]
	if !ok {
		base = 7500 // fallback for unknown positions
	}
	var bump int64
	if attrMean > 50 {
		d := attrMean - 50
		bump = int64(math.Round(float64(d*d) / 20.0))
	}
	return base + bump
}

// MeanAttribute returns the arithmetic mean of all values in the attribute
// map, rounded to the nearest integer. It returns 0 if the map is empty.
func MeanAttribute(attrs map[string]int) int {
	if len(attrs) == 0 {
		return 0
	}
	sum := 0
	for _, v := range attrs {
		sum += v
	}
	return int(math.Round(float64(sum) / float64(len(attrs))))
}

// ContractSeasons returns the standard contract length in full seasons for
// a player of the given age.
func ContractSeasons(age int) int {
	switch {
	case age <= 22:
		return 4
	case age <= 28:
		return 3
	default:
		return 2
	}
}
