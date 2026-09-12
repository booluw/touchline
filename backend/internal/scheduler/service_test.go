package scheduler

import (
	"context"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/robfig/cron/v3"
)

func TestGranularityFromKey(t *testing.T) {
	tests := []struct {
		key    string
		want   string
		wantOK bool
	}{
		{"tick.hourly_cadence", "hourly", true},
		{"tick.daily_cadence", "daily", true},
		{"tick.weekly_cadence", "weekly", true},
		{"tick.monthly_cadence", "monthly", true},
		{"tick.seasonal_cadence", "seasonal", true},
		{"tick.match_cadence", "", false}, // live match ticks belong to the match engine
		{"tick.daily", "", false},
		{"daily_cadence", "", false},
		{"tick.daily_cadence.extra", "", false},
		{"feature.x", "", false},
		{"", "", false},
	}
	for _, tt := range tests {
		got, ok := granularityFromKey(tt.key)
		if got != tt.want || ok != tt.wantOK {
			t.Errorf("granularityFromKey(%q) = (%q, %v), want (%q, %v)", tt.key, got, ok, tt.want, tt.wantOK)
		}
	}
}

// fakeScheduler captures registrations and can fire handlers on demand, so
// sync/reconcile timing is deterministic in tests.
type fakeScheduler struct {
	mu      sync.Mutex
	next    cron.EntryID
	entries map[cron.EntryID]fakeEntry
}

type fakeEntry struct {
	spec string
	cmd  func()
}

func newFakeScheduler() *fakeScheduler {
	return &fakeScheduler{entries: make(map[cron.EntryID]fakeEntry)}
}

func (f *fakeScheduler) AddFunc(spec string, cmd func()) (cron.EntryID, error) {
	if _, err := cron.ParseStandard(spec); err != nil {
		return 0, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.next++
	f.entries[f.next] = fakeEntry{spec: spec, cmd: cmd}
	return f.next, nil
}

func (f *fakeScheduler) Remove(id cron.EntryID) {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.entries, id)
}

func (f *fakeScheduler) Start() {}

func (f *fakeScheduler) Stop() context.Context { return context.Background() }

// specs returns the registered spec per cron entry, sorted by entry id.
func (f *fakeScheduler) specs() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []string
	for id := cron.EntryID(1); id <= f.next; id++ {
		if e, ok := f.entries[id]; ok {
			out = append(out, e.spec)
		}
	}
	return out
}

// run executes the registered handler for an entry, matching how cron calls it.
func (f *fakeScheduler) run(id cron.EntryID) {
	f.mu.Lock()
	e, ok := f.entries[id]
	f.mu.Unlock()
	if ok {
		e.cmd()
	}
}

func TestReconcileRegistersUpdatesAndRemoves(t *testing.T) {
	worldA := uuid.New()
	worldB := uuid.New()
	fake := newFakeScheduler()
	svc := newServiceWith(nil, nil, fake)

	desired := map[ref]string{
		{worldID: worldA, granularity: "daily"}:  "0 0 * * *",
		{worldID: worldA, granularity: "weekly"}: "0 0 * * 0",
		{worldID: worldB, granularity: "hourly"}: "0 * * * *",
	}
	if err := svc.reconcile(desired); err != nil {
		t.Fatalf("reconcile add: %v", err)
	}
	if got, want := len(fake.specs()), 3; got != want {
		t.Fatalf("registered %d entries, want %d", got, want)
	}
	if len(svc.entries) != 3 || len(svc.specs) != 3 {
		t.Fatalf("service registry not populated after reconcile: entries=%d specs=%d", len(svc.entries), len(svc.specs))
	}

	// Idempotent reconcile with no changes must not re-register anything.
	if err := svc.reconcile(desired); err != nil {
		t.Fatalf("reconcile no-op: %v", err)
	}
	if got, want := len(fake.specs()), 3; got != want {
		t.Fatalf("idempotent reconcile re-registered entries: got %d want %d", got, want)
	}

	// A changed spec replaces the entry.
	desired = map[ref]string{
		{worldID: worldA, granularity: "daily"}:  "30 0 * * *",
		{worldID: worldA, granularity: "weekly"}: "0 0 * * 0",
		{worldID: worldB, granularity: "hourly"}: "0 * * * *",
	}
	if err := svc.reconcile(desired); err != nil {
		t.Fatalf("reconcile update: %v", err)
	}
	if got := len(fake.specs()); got != 3 {
		t.Fatalf("spec update leaked entries: got %d entries %v", got, fake.specs())
	}
	if v, ok := svc.specs[ref{worldID: worldA, granularity: "daily"}]; !ok || v != "30 0 * * *" {
		t.Fatalf("daily spec not updated: ok=%v value=%v", ok, v)
	}

	// A removed cadence unregisters its entry.
	desired = map[ref]string{
		{worldID: worldA, granularity: "weekly"}: "0 0 * * 0",
		{worldID: worldB, granularity: "hourly"}: "0 * * * *",
	}
	if err := svc.reconcile(desired); err != nil {
		t.Fatalf("reconcile remove: %v", err)
	}
	if got, want := len(fake.specs()), 2; got != want {
		t.Fatalf("expected %d entries after removal, got %d (%v)", want, got, fake.specs())
	}
	if len(svc.entries) != 2 || len(svc.specs) != 2 {
		t.Fatalf("service registry not pruned after reconcile: entries=%d specs=%d", len(svc.entries), len(svc.specs))
	}
}

func TestReconcileSkipsInvalidSpec(t *testing.T) {
	fake := newFakeScheduler()
	svc := newServiceWith(nil, nil, fake)
	worldID := uuid.New()

	desired := map[ref]string{
		{worldID: worldID, granularity: "daily"}: "not-a-cron", // must fail to parse
	}
	if err := svc.reconcile(desired); err != nil {
		t.Fatalf("reconcile with invalid spec: %v", err)
	}
	if got := len(fake.specs()); got != 0 {
		t.Fatalf("invalid spec was registered: %v", fake.specs())
	}
	if len(svc.entries) != 0 {
		t.Fatalf("invalid spec left in registry: %v", svc.entries)
	}
}
