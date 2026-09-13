package eventbus

import (
	"context"
	"fmt"
	"sync"
)

// EventDispatcher routes delivered events to the handler registered for their
// event type. It deliberately does not deduplicate: dedupe belongs in handlers
// (keyed on event.ID), so river retries re-run handlers against the same event
// to keep the at-least-once contract.
type EventDispatcher struct {
	mu       sync.RWMutex
	handlers map[string]EventHandler
}

// NewEventDispatcher returns an empty dispatcher.
func NewEventDispatcher() *EventDispatcher {
	return &EventDispatcher{handlers: make(map[string]EventHandler)}
}

// Subscribe registers a handler for an event type. Registering twice for the
// same type is an error to avoid ambiguous parallel handling.
func (d *EventDispatcher) Subscribe(eventType string, handler EventHandler) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if _, ok := d.handlers[eventType]; ok {
		return fmt.Errorf("eventbus: handler already registered for event type %q", eventType)
	}
	d.handlers[eventType] = handler
	return nil
}

// Dispatch delivers a single event to the handler registered for its type.
// Unsubscribed event types are no-ops (a process may publish without consuming).
func (d *EventDispatcher) Dispatch(ctx context.Context, event *Event) error {
	d.mu.RLock()
	handler, ok := d.handlers[event.EventType]
	d.mu.RUnlock()
	if !ok {
		return nil
	}
	return handler(*event)
}
