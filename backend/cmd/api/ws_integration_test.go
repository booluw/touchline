//go:build integration

package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/touchline/backend/internal/testdb"
	"github.com/touchline/backend/pkg/realtime"
)

// newRealtimeTestServer is like testHTTPServer but also returns the server so
// tests can reach the realtime hub (the setter is the seam the worker's tick
// bridge would use in production).
func newRealtimeTestServer(t *testing.T) (*httptest.Server, *pgxpool.Pool, *realtime.Hub) {
	t.Helper()
	s, pool := newTestServer(t)
	ts := httptest.NewServer(s.router())
	t.Cleanup(ts.Close)
	return ts, pool, s.hub
}

func dialWS(t *testing.T, ts *httptest.Server, cookieHeader string) *websocket.Conn {
	t.Helper()
	url := "ws" + strings.TrimPrefix(ts.URL, "http") + "/ws"
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	opts := &websocket.DialOptions{}
	if cookieHeader != "" {
		opts.HTTPHeader = http.Header{"Cookie": {cookieHeader}}
	}
	conn, resp, err := websocket.Dial(ctx, url, opts)
	t.Cleanup(func() {
		if conn != nil {
			_ = conn.Close(websocket.StatusNormalClosure, "")
		}
	})
	if err != nil {
		status := 0
		if resp != nil {
			status = resp.StatusCode
		}
		t.Fatalf("ws dial: %v (http %d)", err, status)
	}
	return conn
}

func wsConnectFor(t *testing.T, ts *httptest.Server, pool *pgxpool.Pool, email string, worldID uuid.UUID) (*websocket.Conn, uuid.UUID) {
	t.Helper()
	client := ts.Client()
	cookies := login(t, ts, client, email, "s3cret")
	return dialWS(t, ts, cookies), worldID
}

func readWSEvent(t *testing.T, conn *websocket.Conn, timeout time.Duration) (realtime.Event, error) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	typ, data, err := conn.Read(ctx)
	if err != nil {
		return realtime.Event{}, err
	}
	if typ != websocket.MessageText {
		t.Fatalf("got message type %v, want text", typ)
	}
	var ev realtime.Event
	if err := json.Unmarshal(data, &ev); err != nil {
		t.Fatalf("decode ws event: %v", err)
	}
	return ev, nil
}

func waitHubReady(t *testing.T, hub *realtime.Hub) {
	t.Helper()
	select {
	case <-hub.Ready():
	case <-time.After(3 * time.Second):
		t.Fatalf("hub never became ready")
	}
}

func TestWSRejectsUnauthenticatedHandshake(t *testing.T) {
	ts, _, _ := newRealtimeTestServer(t)
	url := "ws" + strings.TrimPrefix(ts.URL, "http") + "/ws"
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, resp, err := websocket.Dial(ctx, url, nil)
	if err == nil {
		t.Fatalf("expected handshake to fail without a session cookie")
	}
	if resp == nil || resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status = %v, want 401", resp)
	}
}

func TestWSAuthenticatedReceivesWorldEvents(t *testing.T) {
	ts, pool, hub := newRealtimeTestServer(t)
	waitHubReady(t, hub)

	world := testdb.CreateWorld(t, pool, "W-WS-A")
	testdb.CreateUser(t, pool, "ws-a@example.com", "s3cret", []testdb.Join{{WorldID: world}})
	conn, _ := wsConnectFor(t, ts, pool, "ws-a@example.com", world)

	ev := realtime.MustEvent(realtime.EventWorldTick, world, map[string]any{"tick": 42})
	if err := hub.Publish(context.Background(), ev); err != nil {
		t.Fatalf("publish: %v", err)
	}

	got, err := readWSEvent(t, conn, 3*time.Second)
	if err != nil {
		t.Fatalf("read ws event: %v", err)
	}
	if got.Type != realtime.EventWorldTick {
		t.Fatalf("got %q, want %q", got.Type, realtime.EventWorldTick)
	}
	if got.WorldID == nil || *got.WorldID != world {
		t.Fatalf("event world = %v, want %s", got.WorldID, world)
	}
}

func TestWSWorldIsolation(t *testing.T) {
	ts, pool, hub := newRealtimeTestServer(t)
	waitHubReady(t, hub)

	worldA := testdb.CreateWorld(t, pool, "W-WS-ISO-A")
	worldB := testdb.CreateWorld(t, pool, "W-WS-ISO-B")
	testdb.CreateUser(t, pool, "ws-iso-a@example.com", "s3cret", []testdb.Join{{WorldID: worldA}})
	testdb.CreateUser(t, pool, "ws-iso-b@example.com", "s3cret", []testdb.Join{{WorldID: worldB}})
	connA, _ := wsConnectFor(t, ts, pool, "ws-iso-a@example.com", worldA)
	connB, _ := wsConnectFor(t, ts, pool, "ws-iso-b@example.com", worldB)

	if err := hub.Publish(context.Background(),
		realtime.MustEvent("match_tick", worldA, map[string]any{"minute": 1})); err != nil {
		t.Fatalf("publish: %v", err)
	}

	gotA, err := readWSEvent(t, connA, 3*time.Second)
	if err != nil {
		t.Fatalf("world A read: %v", err)
	}
	if gotA.Type != "match_tick" {
		t.Fatalf("world A got %q", gotA.Type)
	}
	if _, err := readWSEvent(t, connB, 300*time.Millisecond); err == nil {
		t.Fatalf("world B client received another world's event")
	}
}
