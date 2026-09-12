package realtime

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/coder/websocket"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
)

func worldA() uuid.UUID { return uuid.MustParse("11111111-1111-1111-1111-111111111111") }
func worldB() uuid.UUID { return uuid.MustParse("22222222-2222-2222-2222-222222222222") }

func miniredisClient(t *testing.T) *redis.Client {
	t.Helper()
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatalf("start miniredis: %v", err)
	}
	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() {
		_ = client.Close()
		mr.Close()
	})
	return client
}

// sharedMiniredis runs one miniredis server for the whole test and returns its
// address; multiple redis clients (e.g. one per simulated API pod) must share a
// single server to fan events out across them.
func sharedMiniredis(t *testing.T) string {
	t.Helper()
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatalf("start miniredis: %v", err)
	}
	t.Cleanup(mr.Close)
	return mr.Addr()
}

func redisClientAt(t *testing.T, addr string) *redis.Client {
	t.Helper()
	client := redis.NewClient(&redis.Options{Addr: addr})
	t.Cleanup(func() { _ = client.Close() })
	return client
}

// startHub runs a hub and exposes it over an httptest server. The world id is
// taken from the `world` query param; a missing/invalid one yields uuid.Nil so
// HandleWS can exercise its no-world rejection.
func startHub(t *testing.T, broker Broker) (*Hub, *httptest.Server) {
	t.Helper()
	hub := NewHub(broker)
	ctx, cancel := context.WithCancel(context.Background())
	go func() { _ = hub.Run(ctx) }()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		info := ClientInfo{}
		if raw := r.URL.Query().Get("world"); raw != "" {
			info.WorldID = uuid.MustParse(raw)
		}
		hub.HandleWS(w, r, info)
	}))
	t.Cleanup(func() {
		cancel()
		srv.Close()
		_ = hub.Close()
	})
	return hub, srv
}

func dial(t *testing.T, srv *httptest.Server, world uuid.UUID) *websocket.Conn {
	t.Helper()
	url := "ws" + strings.TrimPrefix(srv.URL, "http") + "?world=" + world.String()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	conn, _, err := websocket.Dial(ctx, url, nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close(websocket.StatusNormalClosure, "") })
	return conn
}

func readEvent(t *testing.T, conn *websocket.Conn, timeout time.Duration) (Event, error) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	typ, data, err := conn.Read(ctx)
	if err != nil {
		return Event{}, err
	}
	if typ != websocket.MessageText {
		t.Fatalf("got message type %v, want text", typ)
	}
	var ev Event
	if err := json.Unmarshal(data, &ev); err != nil {
		t.Fatalf("decode event: %v", err)
	}
	return ev, nil
}

func TestNewEventRoundTrip(t *testing.T) {
	ev, err := NewEvent("match_tick", worldA(), map[string]int{"minute": 42})
	if err != nil {
		t.Fatalf("new event: %v", err)
	}
	if ev.WorldID == nil || *ev.WorldID != worldA() {
		t.Fatalf("world id not set: %+v", ev.WorldID)
	}
	if ev.TS.IsZero() {
		t.Fatalf("timestamp not set")
	}

	data, err := json.Marshal(ev)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var got Event
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.Type != "match_tick" || string(got.Payload) != `{"minute":42}` {
		t.Fatalf("round trip mismatch: %s", data)
	}
}

func TestHandleWSRejectsMissingWorld(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		NewHub(NewLocalBroker()).HandleWS(w, r, ClientInfo{})
	}))
	defer srv.Close()

	resp, err := http.Get(srv.URL)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", resp.StatusCode)
	}
}

func TestClientPingAndUnknownType(t *testing.T) {
	_, srv := startHub(t, NewLocalBroker())
	conn := dial(t, srv, worldA())

	if err := conn.Write(context.Background(), websocket.MessageText, []byte(`{"type":"ping"}`)); err != nil {
		t.Fatalf("write ping: %v", err)
	}
	pong, err := readEvent(t, conn, 3*time.Second)
	if err != nil {
		t.Fatalf("read pong: %v", err)
	}
	if pong.Type != EventPong {
		t.Fatalf("got %q, want pong", pong.Type)
	}

	if err := conn.Write(context.Background(), websocket.MessageText, []byte(`{"type":"teleport"}`)); err != nil {
		t.Fatalf("write unknown: %v", err)
	}
	unknown, err := readEvent(t, conn, 3*time.Second)
	if err != nil {
		t.Fatalf("read unknown response: %v", err)
	}
	if unknown.Type != EventError {
		t.Fatalf("got %q, want error", unknown.Type)
	}
	if !strings.Contains(string(unknown.Payload), "unknown_type") {
		t.Fatalf("error payload missing code: %s", unknown.Payload)
	}
}

func TestLocalBrokerFanOutAndWorldIsolation(t *testing.T) {
	hub, srv := startHub(t, NewLocalBroker())
	waitForReady(t, hub)
	clientA := dial(t, srv, worldA())
	clientB := dial(t, srv, worldB())

	waitForClients(t, hub, worldA(), 1)
	waitForClients(t, hub, worldB(), 1)

	if err := hub.Publish(context.Background(), MustEvent(EventWorldTick, worldA(), map[string]any{"tick": 1})); err != nil {
		t.Fatalf("publish: %v", err)
	}

	gotA, err := readEvent(t, clientA, 3*time.Second)
	if err != nil {
		t.Fatalf("client A read: %v", err)
	}
	if gotA.Type != EventWorldTick {
		t.Fatalf("client A got %q, want %s", gotA.Type, EventWorldTick)
	}

	// World B must not receive world A's event.
	if _, err := readEvent(t, clientB, 300*time.Millisecond); err == nil {
		t.Fatalf("client B received an event scoped to another world")
	}
}

func TestRedisBrokerFansOutAcrossHubs(t *testing.T) {
	addr := sharedMiniredis(t)
	hubA, srvA := startHub(t, NewRedisBrokerWithClient(redisClientAt(t, addr), DefaultChannel))
	hubB, srvB := startHub(t, NewRedisBrokerWithClient(redisClientAt(t, addr), DefaultChannel))

	waitForReady(t, hubA)
	waitForReady(t, hubB)

	clientA := dial(t, srvA, worldA())
	_ = dial(t, srvB, worldA())
	waitForClients(t, hubA, worldA(), 1)
	waitForClients(t, hubB, worldA(), 1)

	// Publish from hub B; the client on hub A must still receive it.
	ev := MustEvent(EventWorldTick, worldA(), map[string]any{"tick": 7})
	if err := hubB.Publish(context.Background(), ev); err != nil {
		t.Fatalf("publish: %v", err)
	}

	got, err := readEvent(t, clientA, 3*time.Second)
	if err != nil {
		t.Fatalf("client A read: %v", err)
	}
	if got.Type != EventWorldTick || string(got.Payload) != `{"tick":7}` {
		t.Fatalf("unexpected event on hub A: %s", got.Type)
	}
}

func waitForReady(t *testing.T, hub *Hub) {
	t.Helper()
	select {
	case <-hub.Ready():
	case <-time.After(3 * time.Second):
		t.Fatalf("hub broker never became ready")
	}
}

func waitForClients(t *testing.T, hub *Hub, world uuid.UUID, want int) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if hub.WorldClientCount(world) >= want {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %d client(s) in world %s", want, world)
}
