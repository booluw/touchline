//go:build integration

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/touchline/backend/internal/testdb"
	"github.com/touchline/backend/pkg/realtime"
)

func TestHTTPMessagesLifecycle(t *testing.T) {
	ts, pool, viewerMgr, targetMgr := newSocialHTTPServer(t)
	client := ts.Client()
	viewerCookies := loginManager(t, ts, pool, "viewer@example.com")
	targetCookies := loginManager(t, ts, pool, "target@example.com")

	// 401 without a session.
	if resp := post(t, ts, client, "/api/messages",
		fmt.Sprintf(`{"recipient_id":%q,"body":"hi"}`, targetMgr), ""); resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("anonymous send = %d, want 401", resp.StatusCode)
	}

	// 400 malformed payloads.
	if resp := post(t, ts, client, "/api/messages", `{}`, viewerCookies); resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("empty payload = %d, want 400", resp.StatusCode)
	}
	if resp := post(t, ts, client, "/api/messages", `{"recipient_id":"not-a-uuid","body":"hi"}`, viewerCookies); resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("bad recipient = %d, want 400", resp.StatusCode)
	}

	// 400 self-messaging.
	if resp := post(t, ts, client, "/api/messages",
		fmt.Sprintf(`{"recipient_id":%q,"body":"hi"}`, viewerMgr), viewerCookies); resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("self-send = %d, want 400", resp.StatusCode)
	}

	// 400 to an AI-managed club's policy bot.
	botClub, botMgr := testdb.CreateClubWithAIManager(t, pool, mustWorldOf(t, pool, viewerMgr))
	if resp := post(t, ts, client, "/api/messages",
		fmt.Sprintf(`{"recipient_id":%q,"body":"hi"}`, botMgr), viewerCookies); resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("bot-send = %d, want 400", resp.StatusCode)
	}
	_ = botClub

	// 404 to an unknown manager.
	if resp := post(t, ts, client, "/api/messages",
		fmt.Sprintf(`{"recipient_id":%q,"body":"hi"}`, uuid.New()), viewerCookies); resp.StatusCode != http.StatusNotFound {
		t.Fatalf("unknown send = %d, want 404", resp.StatusCode)
	}

	// 413 over the character cap.
	if resp := post(t, ts, client, "/api/messages",
		fmt.Sprintf(`{"recipient_id":%q,"body":%q}`, targetMgr, strings.Repeat("a", 2001)), viewerCookies); resp.StatusCode != http.StatusRequestEntityTooLarge {
		t.Fatalf("oversized send = %d, want 413", resp.StatusCode)
	}

	// 201 happy path; HTML sanitized server-side.
	resp := post(t, ts, client, "/api/messages",
		fmt.Sprintf(`{"recipient_id":%q,"body":"<p>Hello <b>rival</b></p>"}`, targetMgr), viewerCookies)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("send = %d, want 201", resp.StatusCode)
	}
	var sent map[string]any
	if err := decodeJSON(t, resp, &sent); err != nil {
		t.Fatalf("decode sent: %v", err)
	}
	if sent["body"] != "Hello rival" {
		t.Errorf("sent body = %v, want sanitized", sent["body"])
	}
	messageID, _ := sent["id"].(string)
	if messageID == "" {
		t.Fatalf("sent message = %v", sent)
	}

	// Recipient inbox shows it with the sender + unread count.
	resp = get(t, ts, client, "/api/messages", targetCookies)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("inbox = %d, want 200", resp.StatusCode)
	}
	var inbox map[string]any
	if err := decodeJSON(t, resp, &inbox); err != nil {
		t.Fatalf("decode inbox: %v", err)
	}
	if inbox["unread"].(float64) != 1 {
		t.Errorf("unread = %v, want 1", inbox["unread"])
	}
	items, _ := inbox["messages"].([]any)
	if len(items) != 1 {
		t.Fatalf("inbox items = %d, want 1", len(items))
	}
	first, _ := items[0].(map[string]any)
	if first["id"] != messageID || first["sender_id"] != viewerMgr.String() {
		t.Errorf("inbox first = %v", first)
	}

	// The sender cannot mark the recipient's message read (404), then the
	// recipient can (200) and unread drops to 0.
	if resp := post(t, ts, client, "/api/messages/"+messageID+"/read", "", viewerCookies); resp.StatusCode != http.StatusNotFound {
		t.Fatalf("foreign read = %d, want 404", resp.StatusCode)
	}
	if resp := post(t, ts, client, "/api/messages/"+messageID+"/read", "", targetCookies); resp.StatusCode != http.StatusOK {
		t.Fatalf("read = %d, want 200", resp.StatusCode)
	}
	resp = get(t, ts, client, "/api/messages", targetCookies)
	if err := decodeJSON(t, resp, &inbox); err != nil {
		t.Fatalf("decode inbox after read: %v", err)
	}
	if unread := inbox["unread"].(float64); unread != 0 {
		t.Errorf("unread after read = %v, want 0", unread)
	}
	// Reading the same message again is idempotent (200, not 404).
	if resp := post(t, ts, client, "/api/messages/"+messageID+"/read", "", targetCookies); resp.StatusCode != http.StatusOK {
		t.Fatalf("re-read = %d, want 200", resp.StatusCode)
	}
	if resp := post(t, ts, client, "/api/messages/"+uuid.New().String()+"/read", "", targetCookies); resp.StatusCode != http.StatusNotFound {
		t.Fatalf("unknown read = %d, want 404", resp.StatusCode)
	}

	// After 30 messages inside the rolling minute (1 real + 30 seeded), the
	// next send is a 429 with Retry-After.
	seedSenderBurst(t, pool, viewerMgr, targetMgr, 30)
	resp = post(t, ts, client, "/api/messages",
		fmt.Sprintf(`{"recipient_id":%q,"body":"nope"}`, targetMgr), viewerCookies)
	if resp.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("rate-limited send = %d, want 429", resp.StatusCode)
	}
	if resp.Header.Get("Retry-After") == "" {
		t.Error("429 response missing Retry-After header")
	}
}

func TestHTTPMessagesWorldIsolation(t *testing.T) {
	ts, pool, _, targetMgr := newSocialHTTPServer(t)
	client := ts.Client()

	sibling := testdb.CreateWorld(t, pool, "W-MSG-HTTP-OTHER")
	testdb.CreateUser(t, pool, "msg-sibling@example.com", "s3cret",
		[]testdb.Join{{WorldID: sibling, Employed: true}})
	siblingCookies := loginManager(t, ts, pool, "msg-sibling@example.com")

	// A manager from another world cannot message targetMgr: reads as 404,
	// the same shape as a profile cross-world read.
	resp := post(t, ts, client, "/api/messages",
		fmt.Sprintf(`{"recipient_id":%q,"body":"hi"}`, targetMgr), siblingCookies)
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("cross-world send = %d, want 404", resp.StatusCode)
	}

	// The sibling's own inbox works in their own world (empty but 200).
	if resp := get(t, ts, client, "/api/messages", siblingCookies); resp.StatusCode != http.StatusOK {
		t.Fatalf("sibling inbox = %d, want 200", resp.StatusCode)
	}
}

// TestWSMessageDelivery proves a sent message reaches the recipient's open
// socket as a social_message envelope (world-scoped, best-effort push).
func TestWSMessageDelivery(t *testing.T) {
	ts, pool, hub := newRealtimeTestServer(t)
	waitHubReady(t, hub)
	ctx := context.Background()

	w := testdb.CreateWorld(t, pool, "W-WS-MSG")
	testdb.CreateUser(t, pool, "ws-sender@example.com", "s3cret",
		[]testdb.Join{{WorldID: w, Employed: true}})
	testdb.CreateUser(t, pool, "ws-recipient@example.com", "s3cret",
		[]testdb.Join{{WorldID: w, Employed: true}})
	var recipientMgr uuid.UUID
	if err := pool.QueryRow(ctx,
		`SELECT id FROM manager.managers WHERE user_id = (SELECT id FROM auth.users WHERE email = 'ws-recipient@example.com')`,
	).Scan(&recipientMgr); err != nil {
		t.Fatalf("recipient manager: %v", err)
	}

	// The recipient holds the socket open; the sender posts a message.
	recipientConn, _ := wsConnectFor(t, ts, pool, "ws-recipient@example.com", w)
	senderCookies := loginManager(t, ts, pool, "ws-sender@example.com")

	resp := post(t, ts, ts.Client(), "/api/messages",
		fmt.Sprintf(`{"recipient_id":%q,"body":"live pass"}`, recipientMgr), senderCookies)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("send = %d, want 201", resp.StatusCode)
	}

	ev, err := readWSEvent(t, recipientConn, 3*time.Second)
	if err != nil {
		t.Fatalf("read ws event: %v", err)
	}
	if ev.Type != realtime.EventSocialMessage {
		t.Fatalf("event type = %q, want %q", ev.Type, realtime.EventSocialMessage)
	}
	var push struct {
		Message struct {
			Body string `json:"body"`
		} `json:"message"`
	}
	if err := json.Unmarshal(ev.Payload, &push); err != nil {
		t.Fatalf("decode payload: %v", err)
	}
	if push.Message.Body != "live pass" {
		t.Errorf("body = %q, want %q", push.Message.Body, "live pass")
	}
}

// seedSenderBurst inserts n extra messages (sender → recipient) timestamped
// now() so the post-sanitization per-minute window is already saturated.
func seedSenderBurst(t *testing.T, pool *pgxpool.Pool, senderID, recipientID uuid.UUID, n int) {
	t.Helper()
	if _, err := pool.Exec(context.Background(), `
		INSERT INTO social.messages (world_id, sender_id, sender_type, recipient_id, recipient_type, subject, body, sent_at)
		SELECT m.world_id, $1, 'manager', $2, 'manager', NULL, 'burst-' || g, now()
		FROM manager.managers m, generate_series(1, $3) g
		WHERE m.id = $1`,
		senderID, recipientID, n); err != nil {
		t.Fatalf("seed burst: %v", err)
	}
}

// mustWorldOf resolves a manager's world id.
func mustWorldOf(t *testing.T, pool *pgxpool.Pool, managerID uuid.UUID) uuid.UUID {
	t.Helper()
	var worldID uuid.UUID
	if err := pool.QueryRow(context.Background(),
		`SELECT world_id FROM manager.managers WHERE id = $1`, managerID).Scan(&worldID); err != nil {
		t.Fatalf("manager world: %v", err)
	}
	return worldID
}
