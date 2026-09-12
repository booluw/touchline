//go:build integration

package world

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"

	"github.com/touchline/backend/internal/testdb"
)

func newTestService(t *testing.T) *Service {
	t.Helper()
	return NewService(testdb.New(t), nil)
}

func TestCreateWorld_StartsProvisioningAndEmitsEvent(t *testing.T) {
	svc := newTestService(t)
	ctx := context.Background()

	w, err := svc.CreateWorld(ctx, "World One")
	if err != nil {
		t.Fatalf("create world: %v", err)
	}
	if w.Status != "provisioning" {
		t.Fatalf("new world status = %q, want provisioning", w.Status)
	}

	got, err := svc.GetWorld(ctx, w.ID)
	if err != nil {
		t.Fatalf("get world: %v", err)
	}
	if got.Name != "World One" || got.Status != "provisioning" {
		t.Fatalf("get world = %+v", got)
	}

	var eventType string
	err = svc.pool.QueryRow(ctx, `
		SELECT event_type FROM world.events WHERE world_id = $1 ORDER BY occurred_at`, w.ID).Scan(&eventType)
	if err != nil {
		t.Fatalf("read creation event: %v", err)
	}
	if eventType != "WORLD_CREATED" {
		t.Fatalf("world creation event = %q, want WORLD_CREATED", eventType)
	}
}

func TestCreateWorld_DuplicateNameRejected(t *testing.T) {
	svc := newTestService(t)
	ctx := context.Background()

	if _, err := svc.CreateWorld(ctx, "Clash"); err != nil {
		t.Fatalf("create: %v", err)
	}
	if _, err := svc.CreateWorld(ctx, "Clash"); !errors.Is(err, ErrNameCollision) {
		t.Fatalf("duplicate name err = %v, want ErrNameCollision", err)
	}
}

func TestWorldLifecycle_TransitionsMatchContract(t *testing.T) {
	svc := newTestService(t)
	ctx := context.Background()

	// Create -> launch -> pause -> resume
	w, err := svc.CreateWorld(ctx, "Lifecycle")
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	w, err = svc.SetStatus(ctx, w.ID, "active")
	if err != nil {
		t.Fatalf("launch: %v", err)
	}
	if !Playable(w.Status) {
		t.Fatalf("active not marked playable")
	}
	got, _ := svc.GetWorld(ctx, w.ID)
	if got.Status != "active" {
		t.Fatalf("after launch status = %q", got.Status)
	}
	// config seeded on launch
	var n int
	if err := svc.pool.QueryRow(ctx,
		`SELECT count(*) FROM world.world_config WHERE world_id = $1`, w.ID).Scan(&n); err != nil {
		t.Fatalf("config count: %v", err)
	}
	if n != len(defaultConfigKeys) {
		t.Fatalf("seeded %d config keys, want %d", n, len(defaultConfigKeys))
	}

	if _, err = svc.SetStatus(ctx, w.ID, "paused"); err != nil {
		t.Fatalf("pause: %v", err)
	}
	if _, err = svc.SetStatus(ctx, w.ID, "active"); err != nil {
		t.Fatalf("resume: %v", err)
	}

	// Invalid transitions are rejected, world stays active.
	if _, err = svc.SetStatus(ctx, w.ID, "provisioning"); !errors.Is(err, ErrInvalidTransition) {
		t.Fatalf("provisioning after active err = %v, want ErrInvalidTransition", err)
	}
	if _, err = svc.SetStatus(ctx, w.ID, "open_beta"); !errors.Is(err, ErrInvalidTransition) {
		t.Fatalf("open_beta after active err = %v, want ErrInvalidTransition", err)
	}

	// Archive is terminal: nothing may leave it.
	if _, err = svc.SetStatus(ctx, w.ID, "archived"); err != nil {
		t.Fatalf("archive: %v", err)
	}
	if _, err = svc.SetStatus(ctx, w.ID, "active"); !errors.Is(err, ErrInvalidTransition) {
		t.Fatalf("active after archived err = %v, want ErrInvalidTransition", err)
	}
}

func TestWorldLifecycle_EventLogTracksTransitions(t *testing.T) {
	svc := newTestService(t)
	ctx := context.Background()

	w, err := svc.CreateWorld(ctx, "Log")
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	for _, to := range []string{"active", "paused", "active", "archived"} {
		if _, err := svc.SetStatus(ctx, w.ID, to); err != nil {
			t.Fatalf("transition to %s: %v", to, err)
		}
	}

	rows, err := svc.pool.Query(ctx, `
		SELECT event_type FROM world.events WHERE world_id = $1 ORDER BY occurred_at`, w.ID)
	if err != nil {
		t.Fatalf("events: %v", err)
	}
	defer rows.Close()

	var got []string
	for rows.Next() {
		var et string
		if err := rows.Scan(&et); err != nil {
			t.Fatalf("scan event: %v", err)
		}
		got = append(got, et)
	}
	want := []string{"WORLD_CREATED", "WORLD_ACTIVE", "WORLD_PAUSED", "WORLD_ACTIVE", "WORLD_ARCHIVED"}
	if len(got) != len(want) {
		t.Fatalf("events = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("events = %v, want %v", got, want)
		}
	}
}

func TestGetWorld_UnknownIsNotFound(t *testing.T) {
	svc := newTestService(t)
	_, err := svc.GetWorld(context.Background(), uuid.New())
	if !errors.Is(err, ErrWorldNotFound) {
		t.Fatalf("err = %v, want ErrWorldNotFound", err)
	}
}
