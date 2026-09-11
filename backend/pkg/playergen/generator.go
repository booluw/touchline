package playergen

// PlayerNameGenerator generates player names based on nationality.
// Each nationality has curated first/last name lists stored as data files.
type PlayerNameGenerator interface {
	GenerateFirstName(nationality string) string
	GenerateLastName(nationality string) string
	GenerateFullName(nationality string) (firstName, lastName string)
}
