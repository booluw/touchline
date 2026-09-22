package social

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/touchline/backend/pkg/apiref"
	"github.com/touchline/backend/pkg/eventbus"
	"github.com/touchline/backend/pkg/realtime"
)

// Direct messaging numbers (S06-04b). Proposal values awaiting PM tuning sign-off —
// see docs/design/social-numerics.md; recalibration is a constant-only change.
const (
	// MaxMessageRunes is the post-sanitization length cap.
	MaxMessageRunes = 2000
	// MinuteMessageLimit and DailyMessageLimit bound per-sender volume.
	// Both counts ride the idx_messages_sender index (sender_id, sent_at DESC).
	MinuteMessageLimit = 30
	DailyMessageLimit  = 200
	// MessageInboxLimit bounds the inbox page returned to a client.
	MessageInboxLimit = 100

	// EventMessageSent is the world.events outbox event for one sent message.
	EventMessageSent = "MESSAGE_SENT"
)

// MessagePush is the payload shared by the MESSAGE_SENT outbox event and the
// realtime EventSocialMessage envelope: the persisted message plus nested
// sender/recipient identities for immediate render.
type MessagePush struct {
	Message Message `json:"message"`
}

// InboxMessage is one inbox row with resolved identities.
type InboxMessage struct {
	Message
}

// SendMessage delivers a direct manager→manager message within worldID. It
// validates the recipient (a human manager in the same world), sanitizes the
// body (HTML stripped, whitespace trimmed, ≤ MaxMessageRunes), enforces the
// per-sender rate limits, and records the row + MESSAGE_SENT outbox event in
// one transaction. A best-effort realtime push is published after commit when
// a broker is wired.
func (s *Service) SendMessage(ctx context.Context, worldID, senderID, recipientID uuid.UUID, rawBody string) (*Message, error) {
	body, err := sanitizeMessage(rawBody)
	if err != nil {
		return nil, err
	}
	if senderID == recipientID {
		return nil, ErrSelfMessage
	}

	sender, err := s.loadManagerBase(ctx, senderID)
	if err != nil {
		return nil, err
	}
	if sender.worldID != worldID {
		return nil, ErrManagerNotInWorld
	}

	recipient, err := s.loadManagerBase(ctx, recipientID)
	if err != nil {
		return nil, err
	}
	if recipient.worldID != worldID {
		return nil, ErrManagerNotInWorld
	}
	if recipient.isBot {
		return nil, ErrBotRecipient
	}

	if err := s.checkMessageRateLimit(ctx, senderID); err != nil {
		return nil, err
	}

	msg := &Message{
		WorldID:       worldID,
		SenderID:      senderID,
		SenderType:    "manager",
		Sender:        &apiref.EntityRef{ID: senderID, Name: sender.name, Type: "manager"},
		RecipientID:   recipientID,
		RecipientType: "manager",
		Recipient:     &apiref.EntityRef{ID: recipientID, Name: recipient.name, Type: "manager"},
		Body:          body,
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin send: %w", err)
	}
	defer tx.Rollback(ctx)

	err = tx.QueryRow(ctx, `
		INSERT INTO social.messages (world_id, sender_id, sender_type, recipient_id, recipient_type, subject, body, sent_at)
		VALUES ($1, $2, 'manager', $3, 'manager', NULL, $4, now())
		RETURNING id, sent_at`,
		worldID, senderID, recipientID, body,
	).Scan(&msg.ID, &msg.SentAt)
	if err != nil {
		return nil, fmt.Errorf("insert message: %w", err)
	}

	payload, err := json.Marshal(MessagePush{Message: *msg})
	if err != nil {
		return nil, fmt.Errorf("marshal MESSAGE_SENT payload: %w", err)
	}
	actorType := "manager"
	e := eventbus.Event{
		WorldID:   worldID,
		EventType: EventMessageSent,
		ActorType: &actorType,
		ActorID:   &senderID,
		Payload:   payload,
	}
	if err := eventbus.WriteTx(ctx, s.bus, tx, &e); err != nil {
		return nil, fmt.Errorf("record %s: %w", EventMessageSent, err)
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit send: %w", err)
	}

	s.pushMessage(ctx, worldID, msg)
	return msg, nil
}

// ListInbox returns the manager's inbound direct messages (newest first,
// bounded by MessageInboxLimit) plus their unread count.
func (s *Service) ListInbox(ctx context.Context, worldID, managerID uuid.UUID) ([]InboxMessage, int, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT m.id, m.sender_id, m.sender_type, m.recipient_id, m.recipient_type,
		       m.subject, m.body, m.sent_at, m.read_at,
		       COALESCE(p.first_name || COALESCE(' ' || p.last_name, ''), ''),
		       COALESCE(pp.first_name || COALESCE(' ' || pp.last_name, ''), '')
		FROM social.messages m
		LEFT JOIN manager.managers mg ON mg.id = m.sender_id
		LEFT JOIN person.people p ON p.id = mg.person_id
		LEFT JOIN manager.managers mg2 ON mg2.id = m.recipient_id
		LEFT JOIN person.people pp ON pp.id = mg2.person_id
		WHERE m.world_id = $1 AND m.recipient_id = $2 AND m.recipient_type = 'manager'
		ORDER BY m.sent_at DESC
		LIMIT $3`, worldID, managerID, MessageInboxLimit)
	if err != nil {
		return nil, 0, fmt.Errorf("list inbox: %w", err)
	}
	defer rows.Close()

	out := []InboxMessage{}
	for rows.Next() {
		var item InboxMessage
		var senderName, recipientName string
		if err := rows.Scan(&item.ID, &item.SenderID, &item.SenderType, &item.RecipientID, &item.RecipientType,
			&item.Subject, &item.Body, &item.SentAt, &item.ReadAt,
			&senderName, &recipientName); err != nil {
			return nil, 0, fmt.Errorf("scan inbox row: %w", err)
		}
		item.Sender = &apiref.EntityRef{ID: item.SenderID, Name: senderName, Type: item.SenderType}
		item.Recipient = &apiref.EntityRef{ID: item.RecipientID, Name: recipientName, Type: item.RecipientType}
		out = append(out, item)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("inbox rows: %w", err)
	}

	unread, err := s.UnreadCount(ctx, worldID, managerID)
	if err != nil {
		return nil, 0, err
	}
	return out, unread, nil
}

// MarkRead stamps read_at (idempotently) on a message addressed to the caller
// and returns the updated row. Messages that are not the caller's — or do not
// exist — read as ErrMessageNotFound.
func (s *Service) MarkRead(ctx context.Context, worldID, managerID, messageID uuid.UUID) (*Message, error) {
	var m Message
	var senderName, recipientName string
	err := s.pool.QueryRow(ctx, `
		UPDATE social.messages
		SET read_at = COALESCE(read_at, now())
		WHERE id = $1 AND world_id = $2 AND recipient_id = $3 AND recipient_type = 'manager'
		RETURNING id, world_id, sender_id, sender_type, recipient_id, recipient_type, subject, body, sent_at, read_at,
		          (SELECT COALESCE(p.first_name || COALESCE(' ' || p.last_name, ''), '')
		           FROM manager.managers mg LEFT JOIN person.people p ON p.id = mg.person_id
		           WHERE mg.id = social.messages.sender_id),
		          (SELECT COALESCE(p.first_name || COALESCE(' ' || p.last_name, ''), '')
		           FROM manager.managers mg LEFT JOIN person.people p ON p.id = mg.person_id
		           WHERE mg.id = social.messages.recipient_id)`,
		messageID, worldID, managerID,
	).Scan(&m.ID, &m.WorldID, &m.SenderID, &m.SenderType, &m.RecipientID, &m.RecipientType,
		&m.Subject, &m.Body, &m.SentAt, &m.ReadAt, &senderName, &recipientName)
	if err == nil {
		m.Sender = &apiref.EntityRef{ID: m.SenderID, Name: senderName, Type: m.SenderType}
		m.Recipient = &apiref.EntityRef{ID: m.RecipientID, Name: recipientName, Type: m.RecipientType}
		return &m, nil
	}
	if err == pgx.ErrNoRows {
		return nil, ErrMessageNotFound
	}
	return nil, fmt.Errorf("mark read: %w", err)
}

// UnreadCount counts inbound messages not yet read.
func (s *Service) UnreadCount(ctx context.Context, worldID, managerID uuid.UUID) (int, error) {
	var n int
	err := s.pool.QueryRow(ctx, `
		SELECT COUNT(*)::int FROM social.messages
		WHERE world_id = $1 AND recipient_id = $2 AND recipient_type = 'manager' AND read_at IS NULL`,
		worldID, managerID).Scan(&n)
	if err != nil {
		return 0, fmt.Errorf("unread count: %w", err)
	}
	return n, nil
}

// pushMessage is the best-effort realtime fan-out after a message commits. A
// missing or failing broker never fails the send: the inbox read stays the
// source of truth.
func (s *Service) pushMessage(ctx context.Context, worldID uuid.UUID, msg *Message) {
	if s.rt == nil {
		return
	}
	ev, err := realtime.NewEvent(realtime.EventSocialMessage, worldID, MessagePush{Message: *msg})
	if err != nil {
		return
	}
	_ = s.rt.Publish(ctx, ev)
}

// checkMessageRateLimit enforces the per-sender per-minute and per-day caps.
// Both windows are read off social.messages (which only ever grows for a
// sender), so limits are approximate under concurrency — acceptable for a
// social inbox, never a hard accounting boundary.
func (s *Service) checkMessageRateLimit(ctx context.Context, senderID uuid.UUID) error {
	var minuteCount, dailyCount int
	err := s.pool.QueryRow(ctx, `
		SELECT
			(SELECT COUNT(*)::int FROM social.messages WHERE sender_id = $1 AND sent_at > now() - interval '1 minute'),
			(SELECT COUNT(*)::int FROM social.messages WHERE sender_id = $1 AND sent_at >= date_trunc('day', now()))`,
		senderID,
	).Scan(&minuteCount, &dailyCount)
	if err != nil {
		return fmt.Errorf("message rate check: %w", err)
	}
	if minuteCount >= MinuteMessageLimit {
		return ErrRateLimited
	}
	if dailyCount >= DailyMessageLimit {
		return ErrRateLimited
	}
	return nil
}

// sanitizeMessage strips HTML tags (basic sanitization), trims surrounding
// whitespace, and enforces the post-sanitization length cap. Over-long bodies
// are rejected outright rather than silently truncated.
func sanitizeMessage(raw string) (string, error) {
	body := strings.TrimSpace(stripHTMLTags(raw))
	if body == "" {
		return "", ErrEmptyMessage
	}
	if utf8.RuneCountInString(body) > MaxMessageRunes {
		return "", ErrMessageTooLong
	}
	return body, nil
}

// stripHTMLTags removes <...> spans entirely (basic sanitization, no profanity
// filtering). Treats raw '<'/'>' as tag delimiters only.
func stripHTMLTags(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	inTag := false
	for _, r := range s {
		switch {
		case inTag && r == '>':
			inTag = false
		case inTag:
		case r == '<':
			inTag = true
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}
