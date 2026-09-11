package eventbus

import (
	"context"
	"errors"
	"testing"
)

func TestEventDispatcher(t *testing.T) {
	d := NewEventDispatcher()

	if err := d.Subscribe("A", func(Event) error { return nil }); err != nil {
		t.Fatalf("subscribe: %v", err)
	}
	// Duplicate registration is rejected.
	if err := d.Subscribe("A", func(Event) error { return nil }); err == nil {
		t.Fatalf("expected duplicate subscribe to fail")
	}

	// Unsubscribed types are a no-op, not an error.
	if err := d.Dispatch(context.Background(), &Event{EventType: "unknown"}); err != nil {
		t.Fatalf("dispatch unknown: %v", err)
	}

	// Handler receives the event and its error propagates.
	var got Event
	wantErr := errors.New("boom")
	if err := d.Subscribe("B", func(ev Event) error {
		got = ev
		return wantErr
	}); err != nil {
		t.Fatalf("subscribe B: %v", err)
	}
	sent := Event{EventType: "B", WorldTick: 3, Payload: []byte("{}")}
	if err := d.Dispatch(context.Background(), &sent); err != wantErr {
		t.Fatalf("expected handler error to propagate, got %v", err)
	}
	if got.EventType != sent.EventType || got.WorldTick != sent.WorldTick || string(got.Payload) != string(sent.Payload) {
		t.Fatalf("handler received %+v, want %+v", got, sent)
	}
}

func TestPublishWithoutPool(t *testing.T) {
	var b RiverBus
	if err := b.Publish(context.Background(), &Event{}); err == nil {
		t.Fatalf("expected publish without a pool to error")
	}
}