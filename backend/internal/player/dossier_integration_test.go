//go:build integration

package player_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/touchline/backend/internal/player"
)

// TestHiddenAttributesOwnClubOnly pins OPD-59: the caller's own player carries
// exact professionalism/temperament/adaptability on every player read, and a
// rival's player never does — even though the rival has a hidden-traits row.
func TestHiddenAttributesOwnClubOnly(t *testing.T) {
	svc, _, pool, tw := newPlayerFixture(t, "hidden-attributes")
	ctx := context.Background()
	own := pickPlayer(t, pool, tw.HumanClub)
	rival := pickPlayer(t, pool, tw.AIOneClub)
	setHiddenTraits(t, pool, own, 71, 42, 63)
	setHiddenTraits(t, pool, rival, 90, 90, 90)
	want := player.HiddenAttributes{Professionalism: 71, Temperament: 42, Adaptability: 63,
		Consistency: 50, InjurySusceptibility: 50, Ambition: 50, Loyalty: 50, PressureHandling: 50, LearningSpeed: 50}

	check := func(read string, got *player.HiddenAttributes) {
		t.Helper()
		if got == nil || *got != want {
			t.Fatalf("%s hidden_attributes = %+v, want %+v", read, got, want)
		}
	}

	card, err := svc.GetPlayerDetail(ctx, tw.WorldID, tw.HumanMgr, own)
	if err != nil {
		t.Fatalf("player detail: %v", err)
	}
	check("profile", card.Hidden)
	checkDossier(t, pool, "profile", own, card.Dossier, true, true)

	morale, err := svc.GetPlayerMoraleDetail(ctx, tw.WorldID, tw.HumanMgr, own)
	if err != nil {
		t.Fatalf("morale detail: %v", err)
	}
	check("morale detail", morale.Hidden)
	checkDossier(t, pool, "morale detail", own, morale.Dossier, true, true)

	dev, err := svc.GetPlayerDevelopmentDetail(ctx, tw.WorldID, tw.HumanMgr, own)
	if err != nil {
		t.Fatalf("development detail: %v", err)
	}
	check("development", dev.Hidden)
	checkDossier(t, pool, "development", own, dev.Dossier, true, true)

	roster, err := svc.ListSquadMorale(ctx, tw.WorldID, tw.HumanMgr)
	if err != nil {
		t.Fatalf("roster: %v", err)
	}
	found := false
	for _, r := range roster {
		if r.Player.ID == own {
			found = true
			check("roster", r.Hidden)
			checkDossier(t, pool, "roster", own, r.Dossier, false, false)
		}
	}
	if !found {
		t.Fatalf("own player %s missing from roster", own)
	}

	rivalCard, err := svc.GetPlayerDetail(ctx, tw.WorldID, tw.HumanMgr, rival)
	if err != nil {
		t.Fatalf("rival detail: %v", err)
	}
	if rivalCard.Hidden != nil {
		t.Fatalf("rival hidden_attributes = %+v; another club's traits must be withheld", rivalCard.Hidden)
	}
	checkDossier(t, pool, "rival profile", rival, rivalCard.Dossier, true, false)
}

// checkDossier asserts the OPD-60 shape: every player_attributes row is
// present with its stored value, history only on single-player reads, private
// only for the caller's own player, and no potential anywhere.
func checkDossier(t *testing.T, pool *pgxpool.Pool, read string, playerID uuid.UUID, d *player.PlayerDossier, wantHistory, wantPrivate bool) {
	t.Helper()
	if d == nil {
		t.Fatalf("%s: dossier missing", read)
	}
	rows, err := pool.Query(context.Background(),
		`SELECT attribute_category, attribute_key, value FROM player.player_attributes WHERE player_id = $1`, playerID)
	if err != nil {
		t.Fatalf("%s: read attributes: %v", read, err)
	}
	defer rows.Close()
	n := 0
	for rows.Next() {
		var cat, key string
		var val int
		if err := rows.Scan(&cat, &key, &val); err != nil {
			t.Fatalf("%s: scan attribute: %v", read, err)
		}
		n++
		if got := d.AttributeValues[cat][key]; got != val {
			t.Fatalf("%s: attribute_values[%s][%s] = %d, want %d", read, cat, key, got, val)
		}
	}
	if n == 0 {
		t.Fatalf("%s: fixture player has no attributes; the check proves nothing", read)
	}
	if (d.History != nil) != wantHistory {
		t.Fatalf("%s: history present = %v, want %v", read, d.History != nil, wantHistory)
	}
	if (d.Private != nil) != wantPrivate {
		t.Fatalf("%s: private present = %v, want %v", read, d.Private != nil, wantPrivate)
	}
	if wantPrivate && len(d.Private.Contracts) == 0 {
		t.Fatalf("%s: own player has a contract but private.contracts is empty", read)
	}
	raw, err := json.Marshal(d)
	if err != nil {
		t.Fatalf("%s: marshal dossier: %v", read, err)
	}
	if strings.Contains(string(raw), "potential") {
		t.Fatalf("%s: dossier leaks potential: %s", read, raw)
	}
}

func setHiddenTraits(t *testing.T, pool *pgxpool.Pool, playerID uuid.UUID, prof, temp, adapt int) {
	t.Helper()
	if _, err := pool.Exec(context.Background(), `
		INSERT INTO player.player_hidden_traits
			(player_id, potential, consistency, injury_susceptibility, adaptability,
			 professionalism, ambition, loyalty, temperament, pressure_handling, learning_speed)
		VALUES ($1, 50, 50, 50, $4, $2, 50, 50, $3, 50, 50)
		ON CONFLICT (player_id) DO UPDATE
		SET potential = 50, consistency = 50, injury_susceptibility = 50, adaptability = $4,
		    professionalism = $2, ambition = 50, loyalty = 50, temperament = $3,
		    pressure_handling = 50, learning_speed = 50`,
		playerID, prof, temp, adapt); err != nil {
		t.Fatalf("set hidden traits: %v", err)
	}
}
