package sqlite

import (
	"context"
	"fmt"
	"time"
)

const timestampFormat = "2006-01-02T15:04:05.000Z07:00"

func formatTimestamp(value time.Time) string {
	return value.UTC().Format(timestampFormat)
}

func nowTimestamp() string {
	return formatTimestamp(time.Now())
}

var timestampColumns = []struct {
	table  string
	column string
}{
	{"events", "received_at"},
	{"events", "occurred_at"},
	{"events", "created_at"},
	{"deliveries", "next_attempt_at"},
	{"deliveries", "lease_until"},
	{"deliveries", "created_at"},
	{"deliveries", "updated_at"},
	{"deliveries", "delivered_at"},
	{"identities", "updated_at"},
	{"notification_preferences", "updated_at"},
	{"pipeline_states", "updated_at"},
	{"unresolved_identities", "first_seen"},
	{"unresolved_identities", "last_seen"},
	{"interaction_events", "created_at"},
	{"interaction_events", "updated_at"},
}

func (s *Store) normalizeTimestamps(ctx context.Context) error {
	for _, item := range timestampColumns {
		query := fmt.Sprintf(`
			UPDATE %[1]s
			SET %[2]s = CASE
				WHEN instr(rtrim(%[2]s, 'Z'), '.') = 0 THEN rtrim(%[2]s, 'Z') || '.000Z'
				ELSE substr(rtrim(%[2]s, 'Z'), 1, instr(rtrim(%[2]s, 'Z'), '.') - 1)
					|| '.' || substr(substr(rtrim(%[2]s, 'Z'), instr(rtrim(%[2]s, 'Z'), '.') + 1) || '000', 1, 3)
					|| 'Z'
			END
			WHERE %[2]s IS NOT NULL AND %[2]s <> '' AND substr(%[2]s, -1) = 'Z'`,
			item.table, item.column)
		if _, err := s.db.ExecContext(ctx, query); err != nil {
			return fmt.Errorf("normalize %s.%s timestamps: %w", item.table, item.column, err)
		}
	}
	return nil
}
