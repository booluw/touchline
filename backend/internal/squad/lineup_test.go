package squad

import (
	"testing"

	"github.com/google/uuid"
)

// mkPlayers builds a 24-man squad with the same position spread the Phase-0
// generator produces: 2 GK, 7 DEF, 7 MID, 6 FWD, 2 flexible outfielders.
func mkPlayers(t *testing.T) []LoadedPlayer {
	t.Helper()
	spread := []string{
		"GK", "GK",
		"CB", "CB", "CB", "LB", "LB", "RB", "RB",
		"CM", "CM", "CM", "CM", "CM", "DM", "AM",
		"ST", "ST", "LW", "LW", "RW", "RW",
		"CM", "AM",
	}
	out := make([]LoadedPlayer, 0, len(spread))
	for i, pos := range spread {
		out = append(out, LoadedPlayer{
			PlayerID:         uuid.New(),
			Position:         pos,
			Status:           "active",
			Available:        true,
			CurrentSentiment: 0,
			Attributes: AttributeSnapshot{
				Technical: 60 + (i % 30), Physical: 60 + (i % 20),
				Mental: 55 + (i % 25), Tactical: 58 + (i % 20),
				Goalkeeping: 50, Positional: 60,
			},
		})
	}
	return out
}

func idSet(ms []SquadMember) map[uuid.UUID]bool {
	out := make(map[uuid.UUID]bool, len(ms))
	for _, m := range ms {
		out[m.PlayerID] = true
	}
	return out
}

func TestSelectStartersDeterministicAndLegal(t *testing.T) {
	players := mkPlayers(t)

	a, err := SelectStarters(players, DefaultFormation())
	if err != nil {
		t.Fatalf("select: %v", err)
	}
	b, err := SelectStarters(players, DefaultFormation())
	if err != nil {
		t.Fatalf("select 2: %v", err)
	}
	if len(a) != len(DefaultFormation()) {
		t.Fatalf("XI size = %d, want %d", len(a), len(DefaultFormation()))
	}
	for i := range a {
		if a[i].PlayerID != b[i].PlayerID {
			t.Fatalf("not deterministic: slot %d differs", i)
		}
	}
	if len(idSet(a)) != len(a) {
		t.Fatal("XI picks a player twice")
	}
	if a[0].Position != "GK" {
		t.Fatalf("slot 0 must be the GK, got %s", a[0].Position)
	}
}

func TestSelectStartersSkipsUnavailable(t *testing.T) {
	players := mkPlayers(t)
	// Nail every GK with an open injury: the selector must complete with the
	// desperate-floor GK while never skipping an available, better outfielder.
	for i := range players {
		if players[i].Position == "GK" {
			players[i].Available = false
		}
	}
	xi, err := SelectStarters(players, DefaultFormation())
	if err != nil {
		t.Fatalf("select without GKs should still complete: %v", err)
	}
	for _, m := range xi {
		if !availableByID(players, m.PlayerID) {
			t.Fatalf("unavailable player %s started", m.PlayerID)
		}
	}
}

func availableByID(players []LoadedPlayer, id uuid.UUID) bool {
	for _, p := range players {
		if p.PlayerID == id {
			return p.Available
		}
	}
	return false
}

func TestSelectStartersForAIDeterministicSeed(t *testing.T) {
	players := mkPlayers(t)
	club := uuid.New()

	xiA := SelectStartersForAI(players, 42, club, DefaultFormation())
	xiB := SelectStartersForAI(players, 42, club, DefaultFormation())
	if len(xiA) != len(DefaultFormation()) {
		t.Fatalf("AI XI size = %d, want %d", len(xiA), len(DefaultFormation()))
	}
	for i := range xiA {
		if xiA[i].PlayerID != xiB[i].PlayerID {
			t.Fatalf("same seed must replay the same XI (slot %d)", i)
		}
	}
	for _, m := range xiA {
		if !availableByID(players, m.PlayerID) {
			t.Fatalf("AI XI contains unavailable player %s", m.PlayerID)
		}
	}
	if len(idSet(xiA)) != len(xiA) {
		t.Fatal("AI XI picks a player twice")
	}
}

func TestSelectStartersForAIChangesWithSeed(t *testing.T) {
	players := mkPlayers(t)
	club := uuid.New()

	// A different fixture seed must generally draw a different XI, though a
	// dominant few can survive the redraw — require at least two distinct
	// XIs across seven consecutive seeds.
	seen := map[string]bool{}
	distinct := 0
	for seed := int64(0); seed < 7; seed++ {
		key := ""
		for _, m := range SelectStartersForAI(players, seed, club, DefaultFormation()) {
			key += m.PlayerID.String() + ","
		}
		if !seen[key] {
			seen[key] = true
			distinct++
		}
	}
	if distinct < 2 {
		t.Fatalf("seeded AI selection is not seed-sensitive (only %d distinct XIs)", distinct)
	}
}

func TestSelectStartersForAIExcludesUnavailable(t *testing.T) {
	players := mkPlayers(t)
	star := -1
	for i := range players {
		if players[i].Position == "ST" && star == -1 {
			star = i
		}
	}
	players[star].Available = false

	for seed := int64(0); seed < 8; seed++ {
		xi := SelectStartersForAI(players, seed, uuid.New(), DefaultFormation())
		for _, m := range xi {
			if m.PlayerID == players[star].PlayerID {
				t.Fatalf("injured striker %s started under seed %d", m.PlayerID, seed)
			}
		}
	}
}

func TestSelectStartersForAIUnavailableGKNeverPlaysOutfield(t *testing.T) {
	players := mkPlayers(t)
	for i := range players {
		if players[i].Position == "GK" {
			players[i].Available = false
		}
	}
	xi := SelectStartersForAI(players, 7, uuid.New(), DefaultFormation())
	if xi[0].Position == "GK" {
		t.Fatal("AI selector must not slot an outfielder into goal")
	}
}

// lineupFor builds a legal 4-3-3 lineup from the squad, picking a DISTINCT
// player per slot even when a formation repeats a position (CB×2, CM×3).
func lineupFor(players []LoadedPlayer) map[int]uuid.UUID {
	byPos := map[string][]uuid.UUID{}
	for _, p := range players {
		byPos[p.Position] = append(byPos[p.Position], p.PlayerID)
	}
	counts := map[string]int{}
	lineup := map[int]uuid.UUID{}
	for slot, pos := range DefaultFormation() {
		ids := byPos[pos]
		lineup[slot] = ids[counts[pos]]
		counts[pos]++
	}
	return lineup
}

func TestSelectStartersWithLineupHonoursSlots(t *testing.T) {
	players := mkPlayers(t)
	lineup := lineupFor(players)

	xi, err := SelectStartersWithLineup(players, lineup, DefaultFormation())
	if err != nil {
		t.Fatalf("select with lineup: %v", err)
	}
	for slot, pos := range DefaultFormation() {
		if xi[slot].PlayerID != lineup[slot] || xi[slot].Position != pos {
			t.Fatalf("slot %d (%s) = %s, want lineup %s", slot, pos, xi[slot].PlayerID, lineup[slot])
		}
	}
}

func TestSelectStartersWithLineupFallsBackWhenMissing(t *testing.T) {
	players := mkPlayers(t)
	lineup := lineupFor(players)
	delete(lineup, 0) // no manager's GK pick: GK slot must fall back

	xi, err := SelectStartersWithLineup(players, lineup, DefaultFormation())
	if err != nil {
		t.Fatalf("select with partial lineup: %v", err)
	}
	if xi[0].Position != "GK" {
		t.Fatalf("GK slot must fall back to a real GK, got %s", xi[0].Position)
	}
	if !availableByID(players, xi[0].PlayerID) {
		t.Fatalf("fallback GK %s is unavailable", xi[0].PlayerID)
	}
	for slot := 1; slot < len(DefaultFormation()); slot++ {
		if xi[slot].PlayerID != lineup[slot] {
			t.Fatalf("slot %d must keep the manager's pick", slot)
		}
	}
}
