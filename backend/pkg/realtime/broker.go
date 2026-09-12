package realtime

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"sync"

	"github.com/redis/go-redis/v9"
)

// DefaultChannel is the Redis pub/sub channel used to fan realtime events out
// across API pods. Events carry their own WorldID, so the hub applies world
// scoping on delivery and a single channel is sufficient.
const DefaultChannel = "touchline:realtime"

// Broker transports realtime events between the hub and other processes/pods.
// All delivery flows through the broker subscription (never a direct local
// send), so a single API pod and a fleet of pods behave identically and a
// publishing pod also sees its own events exactly once.
type Broker interface {
	// Publish broadcasts an event to every subscriber, including the caller.
	Publish(ctx context.Context, ev Event) error
	// Subscribe delivers events to sink until ctx is cancelled. It blocks, so
	// callers run it in a goroutine.
	Subscribe(ctx context.Context, sink func(Event)) error
	// Ready is closed once the broker can receive published events. This lets
	// callers (and tests) avoid the classic pub/sub race where a publish lands
	// before the subscription is armed.
	Ready() <-chan struct{}
	// Close releases any owned resources.
	Close() error
}

// LocalBroker is an in-process broker used when Redis is not configured
// (bare `go run`) and in unit tests. It has no cross-pod reach.
type LocalBroker struct {
	mu     sync.RWMutex
	sinks  []func(Event)
	closed bool

	ready     chan struct{}
	readyOnce sync.Once
}

// NewLocalBroker returns a broker that fans events out within one process.
func NewLocalBroker() *LocalBroker {
	return &LocalBroker{ready: make(chan struct{})}
}

// Publish delivers ev to every subscriber synchronously.
func (b *LocalBroker) Publish(_ context.Context, ev Event) error {
	b.mu.RLock()
	if b.closed {
		b.mu.RUnlock()
		return nil
	}
	sinks := append([]func(Event){}, b.sinks...)
	b.mu.RUnlock()

	for _, sink := range sinks {
		sink(ev)
	}
	return nil
}

// Subscribe registers sink and blocks until ctx is cancelled.
func (b *LocalBroker) Subscribe(ctx context.Context, sink func(Event)) error {
	b.mu.Lock()
	if b.closed {
		b.mu.Unlock()
		return nil
	}
	b.readyOnce.Do(func() { close(b.ready) })
	b.sinks = append(b.sinks, sink)
	b.mu.Unlock()

	<-ctx.Done()
	return ctx.Err()
}

// Ready is closed once a subscriber is registered.
func (b *LocalBroker) Ready() <-chan struct{} { return b.ready }

// Close marks the broker closed; subsequent publishes are dropped.
func (b *LocalBroker) Close() error {
	b.mu.Lock()
	b.closed = true
	b.sinks = nil
	b.mu.Unlock()
	return nil
}

// RedisBroker transports events over Redis pub/sub for multi-pod fan-out.
// Redis is used only as an ephemeral transport here: it never stores
// authoritative gameplay state, which lives in Postgres.
type RedisBroker struct {
	client  *redis.Client
	channel string
	owns    bool

	mu     sync.Mutex
	sinks  []func(Event)
	closed bool

	ready     chan struct{}
	readyOnce sync.Once
}

// NewRedisBroker connects to redisURL and verifies reachability with a ping.
func NewRedisBroker(ctx context.Context, redisURL string) (*RedisBroker, error) {
	opts, err := redis.ParseURL(redisURL)
	if err != nil {
		return nil, fmt.Errorf("parse REDIS_URL: %w", err)
	}
	client := redis.NewClient(opts)
	if err := client.Ping(ctx).Err(); err != nil {
		_ = client.Close()
		return nil, fmt.Errorf("cannot reach Redis: %w", err)
	}
	return &RedisBroker{client: client, channel: DefaultChannel, owns: true, ready: make(chan struct{})}, nil
}

// NewRedisBrokerWithClient wraps an existing client (used by tests with
// miniredis). The broker does not close a client it does not own.
func NewRedisBrokerWithClient(client *redis.Client, channel string) *RedisBroker {
	if channel == "" {
		channel = DefaultChannel
	}
	return &RedisBroker{client: client, channel: channel, ready: make(chan struct{})}
}

// ChannelName reports the Redis channel used for fan-out.
func (b *RedisBroker) ChannelName() string { return b.channel }

// Publish sends ev to the Redis channel.
func (b *RedisBroker) Publish(ctx context.Context, ev Event) error {
	data, err := json.Marshal(ev)
	if err != nil {
		return fmt.Errorf("marshal event: %w", err)
	}
	return b.client.Publish(ctx, b.channel, data).Err()
}

// Subscribe registers sink and pumps published messages into it until ctx is
// cancelled.
func (b *RedisBroker) Subscribe(ctx context.Context, sink func(Event)) error {
	b.mu.Lock()
	if b.closed {
		b.mu.Unlock()
		return nil
	}
	b.sinks = append(b.sinks, sink)
	b.mu.Unlock()

	pubsub := b.client.Subscribe(ctx, b.channel)
	defer pubsub.Close()
	// Wait for the SUBSCRIBE acknowledgement so callers can publish immediately
	// after Run starts without racing the subscription.
	if _, err := pubsub.Receive(ctx); err != nil {
		return err
	}
	b.readyOnce.Do(func() { close(b.ready) })
	messages := pubsub.Channel()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case msg, ok := <-messages:
			if !ok {
				return nil
			}
			var ev Event
			if err := json.Unmarshal([]byte(msg.Payload), &ev); err != nil {
				log.Printf("realtime: dropping unreadable Redis event: %v", err)
				continue
			}
			b.deliver(ev)
		}
	}
}

func (b *RedisBroker) deliver(ev Event) {
	b.mu.Lock()
	sinks := append([]func(Event){}, b.sinks...)
	b.mu.Unlock()
	for _, sink := range sinks {
		sink(ev)
	}
}

// Close stops delivery and releases the Redis client if the broker owns it.
func (b *RedisBroker) Close() error {
	b.mu.Lock()
	b.closed = true
	b.sinks = nil
	b.mu.Unlock()
	if b.owns {
		return b.client.Close()
	}
	return nil
}

// Ready is closed once the SUBSCRIBE acknowledgement has been received.
func (b *RedisBroker) Ready() <-chan struct{} { return b.ready }
