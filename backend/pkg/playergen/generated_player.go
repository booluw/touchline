package playergen

// ValidPositions are the primary positions accepted by
// player.players.primary_position (see migrations/0006_player.up.sql).
var ValidPositions = []string{
	"GK", "CB", "LB", "RB", "DM", "CM", "AM", "LM", "RM", "LW", "RW", "ST",
}

// ageRange is the generated player age span in years.
const (
	minAge = 17
	maxAge = 33
)

// GeneratedPlayer is the pure, persistence-free output of player generation.
//
// The team-creation / first-season flow — not this package — is responsible
// for persisting a GeneratedPlayer as person.people + player.players rows.
// Age is expressed in years rather than a date because the creating layer
// derives date_of_birth from the world's season reference date.
type GeneratedPlayer struct {
	FirstName       string
	LastName        string
	DisplayName     string // shirt name; surname by default for first_last cultures
	NationalityCode string
	Age             int
	PrimaryPosition string
}
