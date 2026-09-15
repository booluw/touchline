package social

import "errors"

var (
	// ErrManagerNotFound is returned when the profile target manager does not
	// exist (or was deleted).
	ErrManagerNotFound = errors.New("manager not found")
	// ErrManagerNotInWorld guards the world boundary: profiles are strictly
	// world-scoped and never leak across worlds (OPD-15).
	ErrManagerNotInWorld = errors.New("manager is not in this world")
	// ErrClubNotFound is returned when a manager's club reference is stale.
	ErrClubNotFound = errors.New("club not found")

	// Messaging errors (S06-04b).
	// ErrSelfMessage rejects messaging your own manager row.
	ErrSelfMessage = errors.New("cannot message yourself")
	// ErrBotRecipient rejects policy-bot/AI managers, which hold no account to
	// read their inbox (messaging is strictly human↔human in S06-04b).
	ErrBotRecipient = errors.New("recipient is not a human manager")
	// ErrEmptyMessage rejects a body that sanitizes away to nothing.
	ErrEmptyMessage = errors.New("message body is empty")
	// ErrMessageTooLong rejects bodies past the post-sanitization character cap.
	ErrMessageTooLong = errors.New("message exceeds the maximum length")
	// ErrRateLimited rejects a sender over the per-minute or per-day cap.
	ErrRateLimited = errors.New("message rate limit exceeded")
	// ErrMessageNotFound guards reads/modifications of messages that do not
	// belong to the caller.
	ErrMessageNotFound = errors.New("message not found")
)
