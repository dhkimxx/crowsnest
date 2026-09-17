package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/dhkimxx/crowsnest/internal/domain"
	"github.com/dhkimxx/crowsnest/internal/ports"
)

var preferenceReasonColumns = map[string]string{
	"ci_failed":           "ci_failed",
	"ci_recovered":        "ci_recovered",
	"mr_review_requested": "mr_review_requested",
	"mr_assigned":         "mr_assigned",
	"mr_updated":          "mr_updated",
	"mr_approved":         "mr_approved",
	"mr_state_changed":    "mr_state_changed",
	"mr_comment":          "mr_comment",
	"mention":             "mention",
	"issue_assigned":      "issue_assigned",
	"issue_updated":       "issue_updated",
}

func (s *Store) SetMute(ctx context.Context, address domain.RecipientAddress, until *time.Time) error {
	email := usableEmail(address.Value)
	if email == "" {
		return errors.New("a usable preference email is required")
	}
	enabled := true
	mutedUntil := ""
	if until != nil {
		enabled = false
		mutedUntil = formatTimestamp(*until)
	}
	now := nowTimestamp()
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO notification_preferences(email, enabled, muted_until, updated_at) VALUES(?, ?, ?, ?)
		ON CONFLICT(email) DO UPDATE SET
			enabled = excluded.enabled,
			muted_until = excluded.muted_until,
			updated_at = excluded.updated_at`,
		email, boolInt(enabled), mutedUntil, now)
	if err != nil {
		return fmt.Errorf("write notification preference: %w", err)
	}
	return nil
}

func (s *Store) BeginInteraction(ctx context.Context, record ports.InteractionRecord) (bool, error) {
	if record.EventID == "" {
		return false, errors.New("interaction event id is required")
	}
	now := nowTimestamp()
	result, err := s.db.ExecContext(ctx, `
		INSERT OR IGNORE INTO interaction_events(
			event_id, message_id, actor_id, action, result, created_at, updated_at
		) VALUES (?, ?, ?, ?, 'processing', ?, ?)`,
		record.EventID, record.MessageID, record.ActorID, record.Action, now, now)
	if err != nil {
		return false, fmt.Errorf("record interaction: %w", err)
	}
	count, err := result.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("count interaction: %w", err)
	}
	return count == 0, nil
}

func (s *Store) FinishInteraction(ctx context.Context, eventID, result string) error {
	if eventID == "" {
		return nil
	}
	if _, err := s.db.ExecContext(ctx, `
		UPDATE interaction_events SET result = ?, updated_at = ? WHERE event_id = ?`,
		result, nowTimestamp(), eventID); err != nil {
		return fmt.Errorf("finish interaction: %w", err)
	}
	return nil
}

func (s *Store) DeliveryByMessageID(ctx context.Context, messageID string) (*ports.RecordedDelivery, error) {
	if messageID == "" {
		return nil, nil
	}
	var key string
	var payload []byte
	err := s.db.QueryRowContext(ctx, `
		SELECT delivery_key, notification_json FROM deliveries
		WHERE provider_message_id = ?
		ORDER BY updated_at DESC
		LIMIT 1`, messageID).Scan(&key, &payload)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("lookup delivery by message: %w", err)
	}
	var notification domain.Notification
	if err := json.Unmarshal(payload, &notification); err != nil {
		return nil, fmt.Errorf("decode delivery notification: %w", err)
	}
	return &ports.RecordedDelivery{Key: key, Notification: notification}, nil
}
