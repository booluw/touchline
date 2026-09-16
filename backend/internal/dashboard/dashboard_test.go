package dashboard

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/touchline/backend/pkg/realtime"
)

func TestOrdinal(t *testing.T) {
	cases := map[int]string{
		1: "1st", 2: "2nd", 3: "3rd", 4: "4th",
		11: "11th", 12: "12th", 13: "13th",
		21: "21st", 22: "22nd", 23: "23rd",
	}
	for in, want := range cases {
		if got := ordinal(in); got != want {
			t.Errorf("ordinal(%d) = %q, want %q", in, got, want)
		}
	}
}

func TestMoney(t *testing.T) {
	cases := map[int64]string{
		0:         "£0.00",
		100:       "£1.00",
		12345:     "£123.45",
		100000000: "£1000000.00",
		-1050:     "£-10.50",
	}
	for in, want := range cases {
		if got := money(in); got != want {
			t.Errorf("money(%d) = %q, want %q", in, got, want)
		}
	}
}

func TestFinalScore(t *testing.T) {
	h, a := 2, 1
	if got := finalScore(&h, &a); got != "2–1" {
		t.Errorf("finalScore = %q, want 2–1", got)
	}
	if got := finalScore(nil, nil); got != "0–0" {
		t.Errorf("finalScore(nil,nil) = %q, want 0–0", got)
	}
}

func TestLimitItems(t *testing.T) {
	items := []Item{{ID: "a"}, {ID: "b"}, {ID: "c"}}
	if got := limitItems(items, 2); len(got) != 2 || got[1].ID != "b" {
		t.Fatalf("limitItems = %v", got)
	}
	if got := limitItems(items, 5); len(got) != 3 {
		t.Fatalf("limitItems under cap = %d, want 3", len(got))
	}
}

func TestManageClubPrefersParticipant(t *testing.T) {
	home, away, other := uuid.New(), uuid.New(), uuid.New()
	if got := manageClub(home, away, []uuid.UUID{other, away}); got != away {
		t.Fatalf("manageClub = %s, want away club", got)
	}
	if got := manageClub(home, away, []uuid.UUID{other}); got != home {
		t.Fatalf("manageClub fallback = %s, want home", got)
	}
}

func TestCrisisLabel(t *testing.T) {
	if got := crisisLabel("administration_risk"); got != "administration risk" {
		t.Errorf("crisisLabel = %q", got)
	}
	if got := crisisLabel("warning"); got != "warning" {
		t.Errorf("crisisLabel pass-through = %q", got)
	}
}

// TestPushDedupesByItemID verifies the realtime push only sends items whose
// stable IDs have not been pushed for that feed yet.
func TestPushDedupesByItemID(t *testing.T) {
	broker := realtime.NewLocalBroker()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	got := make(chan realtime.Event, 8)
	go func() { _ = broker.Subscribe(ctx, func(ev realtime.Event) { got <- ev }) }()
	select {
	case <-broker.Ready():
	case <-time.After(time.Second):
		t.Fatal("broker not ready")
	}

	svc := &Service{broker: broker, pushedIDs: make(map[string]map[string]bool)}
	world, manager := uuid.New(), uuid.New()

	items := []Item{{ID: "a", Priority: PriorityUrgent}, {ID: "b", Priority: PriorityUrgent}}
	if err := svc.push(ctx, world, manager, PriorityUrgent, items); err != nil {
		t.Fatalf("push: %v", err)
	}
	first := recvEvent(t, got)
	if first.Type != realtime.EventDashboardUpdate {
		t.Fatalf("event type = %q", first.Type)
	}

	// Re-pushing the same set must not emit anything.
	if err := svc.push(ctx, world, manager, PriorityUrgent, items); err != nil {
		t.Fatalf("re-push: %v", err)
	}
	expectNoEvent(t, got)

	// A new item emits only that new item.
	if err := svc.push(ctx, world, manager, PriorityUrgent, append(items, Item{ID: "c", Priority: PriorityUrgent})); err != nil {
		t.Fatalf("push new: %v", err)
	}
	ev := recvEvent(t, got)
	var payload DashboardUpdatePayload
	if err := json.Unmarshal(ev.Payload, &payload); err != nil {
		t.Fatalf("payload: %v", err)
	}
	if len(payload.Items) != 1 || payload.Items[0].ID != "c" {
		t.Fatalf("payload items = %+v, want just c", payload.Items)
	}
}

func recvEvent(t *testing.T, ch <-chan realtime.Event) realtime.Event {
	t.Helper()
	select {
	case ev := <-ch:
		return ev
	case <-time.After(time.Second):
		t.Fatal("expected an event")
		return realtime.Event{}
	}
}

func expectNoEvent(t *testing.T, ch <-chan realtime.Event) {
	t.Helper()
	select {
	case ev := <-ch:
		t.Fatalf("unexpected event: %+v", ev)
	case <-time.After(50 * time.Millisecond):
	}
}
