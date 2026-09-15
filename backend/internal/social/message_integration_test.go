//go:build integration

package social_test

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/touchline/backend/internal/social"
	"github.com/touchline/backend/internal/testdb"
	"github.com/touchline/backend/internal/transfertest"
	"github.com/touchline/backend/pkg/realtime"
)

// newMessagingFixture provisions one human manager (w.HumanMgr, w.HumanClub)
// and a second human manager + club in the same world, attached person rows so
// the inbox resolves a display name.
func newMessagingFixture(t *testing.T, name string) (*social.Service, *pgxpool.Pool, transfertest.World, uuid.UUID) {
	t.Helper()
	pool := testdb.New(t)
	w := transfertest.Provision(t, pool, name, name+"-owner@example.com")

	secondUser := testdb.CreateUser(t, pool, name+"-second@example.com", "s3cret",
		[]testdb.Join{{WorldID: w.WorldID, Employed: true}})
	var secondMgr, secondClub uuid.UUID
	ctx := context.Background()
	if err := pool.QueryRow(ctx,
		`SELECT id, current_club_id FROM manager.managers WHERE user_id = $1`, secondUser).Scan(&secondMgr, &secondClub); err != nil {
		t.Fatalf("second manager: %v", err)
	}

	attachPerson(t, pool, w.WorldID, w.HumanMgr, "Ada", "Lovelace", "Ada Lovelace")
	attachPerson(t, pool, w.WorldID, secondMgr, "Grace", "Hopper", "Grace Hopper")

	svc := social.NewService(pool, nil)
	return svc, pool, w, secondMgr
}

func TestSendMessageInboxLifecycle(t *testing.T) {
	svc, pool, w, secondMgr := newMessagingFixture(t, "msg-lifecycle")
	ctx := context.Background()

	msg, err := svc.SendMessage(ctx, w.WorldID, w.HumanMgr, secondMgr, "<b>Nice</b> game last week")
	if err != nil {
		t.Fatalf("send: %v", err)
	}
	if msg.ID == uuid.Nil || msg.SentAt.IsZero() {
		t.Fatalf("message = %+v, want id + sent_at", msg)
	}
	if msg.Body != "Nice game last week" {
		t.Errorf("body = %q, want sanitized", msg.Body)
	}
	if msg.SenderID != w.HumanMgr || msg.RecipientID != secondMgr {
		t.Errorf("message = %+v", msg)
	}

	// The outbox row exists (bus is nil here, so log-only writing still runs).
	var eventType string
	if err := pool.QueryRow(ctx, `
		SELECT event_type FROM world.events WHERE world_id = $1 AND event_type = 'MESSAGE_SENT' ORDER BY occurred_at DESC LIMIT 1`,
		w.WorldID).Scan(&eventType); err != nil {
		t.Fatalf("MESSAGE_SENT event row: %v", err)
	}

	// Recipient inbox shows it, newest first, with the sender name + unread.
	inbox, unread, err := svc.ListInbox(ctx, w.WorldID, secondMgr)
	if err != nil {
		t.Fatalf("inbox: %v", err)
	}
	if unread != 1 {
		t.Errorf("unread = %d, want 1", unread)
	}
	if len(inbox) != 1 {
		t.Fatalf("inbox len = %d, want 1", len(inbox))
	}
	if inbox[0].ID != msg.ID || inbox[0].SenderName != "Ada Lovelace" || inbox[0].Body != "Nice game last week" {
		t.Errorf("inbox row = %+v", inbox[0])
	}

	// Marking read is idempotent and flips the unread count.
	read, err := svc.MarkRead(ctx, w.WorldID, secondMgr, msg.ID)
	if err != nil {
		t.Fatalf("mark read: %v", err)
	}
	if read.ReadAt == nil {
		t.Fatal("read_at not stamped")
	}
	if _, unread, err = svc.ListInbox(ctx, w.WorldID, secondMgr); err != nil {
		t.Fatalf("inbox after read: %v", err)
	} else if unread != 0 {
		t.Errorf("unread after read = %d, want 0", unread)
	}
	readAgain, err := svc.MarkRead(ctx, w.WorldID, secondMgr, msg.ID)
	if err != nil {
		t.Fatalf("mark read again: %v", err)
	}
	if readAgain.ReadAt.Unix() != read.ReadAt.Unix() {
		t.Errorf("idempotent read_at drifted: %v → %v", read.ReadAt, readAgain.ReadAt)
	}

	// The sender's own read of someone else's message is not found.
	if _, err := svc.MarkRead(ctx, w.WorldID, w.HumanMgr, msg.ID); !errors.Is(err, social.ErrMessageNotFound) {
		t.Fatalf("cross-recipient mark read = %v, want ErrMessageNotFound", err)
	}
	// Unknown message id likewise.
	if _, err := svc.MarkRead(ctx, w.WorldID, secondMgr, uuid.New()); !errors.Is(err, social.ErrMessageNotFound) {
		t.Fatalf("unknown mark read = %v, want ErrMessageNotFound", err)
	}
}

func TestSendMessageRejectsInvalidRecipients(t *testing.T) {
	svc, pool, w, secondMgr := newMessagingFixture(t, "msg-invalid")
	ctx := context.Background()
	body := "hello"

	if _, err := svc.SendMessage(ctx, w.WorldID, w.HumanMgr, w.HumanMgr, body); !errors.Is(err, social.ErrSelfMessage) {
		t.Fatalf("self = %v, want ErrSelfMessage", err)
	}
	if _, err := svc.SendMessage(ctx, w.WorldID, w.HumanMgr, w.AIOneMgr, body); !errors.Is(err, social.ErrBotRecipient) {
		t.Fatalf("bot = %v, want ErrBotRecipient", err)
	}
	if _, err := svc.SendMessage(ctx, w.WorldID, w.HumanMgr, uuid.New(), body); !errors.Is(err, social.ErrManagerNotFound) {
		t.Fatalf("unknown = %v, want ErrManagerNotFound", err)
	}

	// A manager from a sibling world is outside the boundary.
	other := testdb.CreateWorld(t, pool, "W-MSG-OTHER")
	otherUser := testdb.CreateUser(t, pool, "msg-other@example.com", "s3cret",
		[]testdb.Join{{WorldID: other, Employed: true}})
	var otherMgr uuid.UUID
	if err := pool.QueryRow(ctx,
		`SELECT id FROM manager.managers WHERE user_id = $1`, otherUser).Scan(&otherMgr); err != nil {
		t.Fatalf("other manager: %v", err)
	}
	if _, err := svc.SendMessage(ctx, w.WorldID, w.HumanMgr, otherMgr, body); !errors.Is(err, social.ErrManagerNotInWorld) {
		t.Fatalf("cross-world = %v, want ErrManagerNotInWorld", err)
	}

	// The second human manager is a valid recipient, sanity check the path.
	if _, err := svc.SendMessage(ctx, w.WorldID, w.HumanMgr, secondMgr, body); err != nil {
		t.Fatalf("valid recipient: %v", err)
	}
}

func TestSendMessageSanitizationAndLength(t *testing.T) {
	svc, _, w, secondMgr := newMessagingFixture(t, "msg-sanitize")
	ctx := context.Background()

	// HTML tags are stripped; surrounding whitespace trimmed.
	msg, err := svc.SendMessage(ctx, w.WorldID, w.HumanMgr, secondMgr, "  <script>alert(1)</script>Hello<b>!</b>\n  ")
	if err != nil {
		t.Fatalf("send: %v", err)
	}
	if msg.Body != "Hello!" {
		t.Errorf("body = %q, want %q", msg.Body, "Hello!")
	}

	// A body that sanitizes to empty is rejected.
	if _, err := svc.SendMessage(ctx, w.WorldID, w.HumanMgr, secondMgr, "<b>   </b>"); !errors.Is(err, social.ErrEmptyMessage) {
		t.Fatalf("empty = %v, want ErrEmptyMessage", err)
	}

	// Exactly at the cap is fine; one rune over is rejected (post-sanitization).
	if _, err := svc.SendMessage(ctx, w.WorldID, w.HumanMgr, secondMgr,
		strings.Repeat("a", social.MaxMessageRunes)); err != nil {
		t.Fatalf("at-cap send = %v, want ok", err)
	}
	if _, err := svc.SendMessage(ctx, w.WorldID, w.HumanMgr, secondMgr,
		strings.Repeat("a", social.MaxMessageRunes)+"b"); !errors.Is(err, social.ErrMessageTooLong) {
		t.Fatalf("over-cap = %v, want ErrMessageTooLong", err)
	}
}

func TestSendMessageRateLimits(t *testing.T) {
	ctx := context.Background()

	t.Run("per minute", func(t *testing.T) {
		svc, pool, w, secondMgr := newMessagingFixture(t, "msg-limit-minute")
		seedMessages(t, pool, w.WorldID, w.HumanMgr, secondMgr, social.MinuteMessageLimit, "now()")
		if _, err := svc.SendMessage(ctx, w.WorldID, w.HumanMgr, secondMgr, "nope"); !errors.Is(err, social.ErrRateLimited) {
			t.Fatalf("minute-limit send = %v, want ErrRateLimited", err)
		}
	})

	t.Run("per day", func(t *testing.T) {
		svc, pool, w, secondMgr := newMessagingFixture(t, "msg-limit-day")
		// 200 rows outside the rolling minute window (10 min ago) but inside
		// today hits the daily cap while the minute counter stays clean.
		seedMessages(t, pool, w.WorldID, w.HumanMgr, secondMgr, social.DailyMessageLimit, "now() - interval '10 minutes'")
		if _, err := svc.SendMessage(ctx, w.WorldID, w.HumanMgr, secondMgr, "nope"); !errors.Is(err, social.ErrRateLimited) {
			t.Fatalf("daily-limit send = %v, want ErrRateLimited", err)
		}
	})

	t.Run("under limit sent for real", func(t *testing.T) {
		svc, _, w, secondMgr := newMessagingFixture(t, "msg-limit-ok")
		if _, err := svc.SendMessage(ctx, w.WorldID, w.HumanMgr, secondMgr, "fine"); err != nil {
			t.Fatalf("under-limit send = %v, want ok", err)
		}
	})
}

func TestSendMessagePublishesRealtime(t *testing.T) {
	svc, _, w, secondMgr := newMessagingFixture(t, "msg-realtime")
	broker := realtime.NewLocalBroker()
	svc.WithRealtime(broker)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	got := make(chan realtime.Event, 4)
	go func() { _ = broker.Subscribe(ctx, func(ev realtime.Event) { got <- ev }) }()
	<-broker.Ready()

	if _, err := svc.SendMessage(ctx, w.WorldID, w.HumanMgr, secondMgr, "over the wire"); err != nil {
		t.Fatalf("send: %v", err)
	}

	select {
	case ev := <-got:
		if ev.Type != realtime.EventSocialMessage {
			t.Fatalf("event type = %q, want %q", ev.Type, realtime.EventSocialMessage)
		}
		if ev.WorldID == nil || *ev.WorldID != w.WorldID {
			t.Fatalf("event world = %v, want %s", ev.WorldID, w.WorldID)
		}
		var push social.MessagePush
		if err := json.Unmarshal(ev.Payload, &push); err != nil {
			t.Fatalf("decoding push payload: %v", err)
		}
		if push.Message.Body != "over the wire" || push.SenderName != "Ada Lovelace" {
			t.Errorf("push = %+v", push)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("no social_message event received")
	}
}

// seedMessages bulk-inserts n messages from senderID to recipientID with the
// given sent_at expression so rate-limit windows can be filled cheaply.
func seedMessages(t *testing.T, pool *pgxpool.Pool, worldID, senderID, recipientID uuid.UUID, n int, sentAtExpr string) {
	t.Helper()
	if _, err := pool.Exec(context.Background(), `
		INSERT INTO social.messages (world_id, sender_id, sender_type, recipient_id, recipient_type, subject, body, sent_at)
		SELECT $1, $2, 'manager', $3, 'manager', NULL, 'bulk-' || g, `+sentAtExpr+`
		FROM generate_series(1, $4) g`,
		worldID, senderID, recipientID, n); err != nil {
		t.Fatalf("seed %d messages: %v", n, err)
	}
}
