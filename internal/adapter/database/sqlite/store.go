package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/mail"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/dhkimxx/crowsnest/internal/domain"
	_ "modernc.org/sqlite"
)

const (
	defaultLeaseDuration = 5 * time.Minute
	defaultMaxAttempts   = 5
	defaultRetryDelay    = time.Second
)

type Config struct {
	LeaseDuration time.Duration
	MaxAttempts   int
	RetryDelay    time.Duration
}

type Preference struct {
	Email             string
	Enabled           bool
	CIFailed          bool
	CIRecovered       bool
	MRReviewRequested bool
	MRAssigned        bool
	MRUpdated         bool
	MRApproved        bool
	MRStateChanged    bool
	MRComment         bool
	Mention           bool
	IssueAssigned     bool
	IssueUpdated      bool
	ProjectInclude    string
	ProjectExclude    string
}

func DefaultConfig() Config {
	return Config{
		LeaseDuration: defaultLeaseDuration,
		MaxAttempts:   defaultMaxAttempts,
		RetryDelay:    defaultRetryDelay,
	}
}

type Store struct {
	db     *sql.DB
	config Config
}

func Open(path string, config Config) (*Store, error) {
	if path == "" {
		return nil, errors.New("SQLite path is required")
	}
	if config.LeaseDuration <= 0 {
		config.LeaseDuration = defaultLeaseDuration
	}
	if config.MaxAttempts <= 0 {
		config.MaxAttempts = defaultMaxAttempts
	}
	if config.RetryDelay <= 0 {
		config.RetryDelay = defaultRetryDelay
	}
	if path != ":memory:" && !strings.HasPrefix(path, "file:") {
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			return nil, fmt.Errorf("create SQLite directory: %w", err)
		}
	}

	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("open SQLite: %w", err)
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	store := &Store{db: db, config: config}
	if err := store.configure(context.Background()); err != nil {
		_ = db.Close()
		return nil, err
	}
	if err := store.migrate(context.Background()); err != nil {
		_ = db.Close()
		return nil, err
	}
	return store, nil
}

func (s *Store) Close() error {
	return s.db.Close()
}

func (s *Store) configure(ctx context.Context) error {
	statements := []string{
		"PRAGMA busy_timeout = 5000",
		"PRAGMA foreign_keys = ON",
		"PRAGMA journal_mode = WAL",
	}
	for _, statement := range statements {
		if _, err := s.db.ExecContext(ctx, statement); err != nil {
			return fmt.Errorf("configure SQLite: %s: %w", statement, err)
		}
	}
	return nil
}

func (s *Store) migrate(ctx context.Context) error {
	statements := []string{
		`CREATE TABLE IF NOT EXISTS schema_migrations (
			version INTEGER PRIMARY KEY,
			applied_at TEXT NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS events (
			event_key TEXT PRIMARY KEY,
			source TEXT NOT NULL,
			source_version TEXT NOT NULL,
			source_event TEXT NOT NULL,
			kind TEXT NOT NULL,
			action TEXT NOT NULL,
			status TEXT NOT NULL,
			event_json TEXT NOT NULL,
			project_id TEXT,
			received_at TEXT NOT NULL,
			occurred_at TEXT,
			created_at TEXT NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS deliveries (
			delivery_key TEXT PRIMARY KEY,
			event_key TEXT NOT NULL REFERENCES events(event_key),
			status TEXT NOT NULL CHECK (status IN ('pending', 'processing', 'delivered', 'failed')),
			notification_json TEXT NOT NULL,
			attempts INTEGER NOT NULL DEFAULT 0,
			retryable INTEGER NOT NULL DEFAULT 1,
			next_attempt_at TEXT NOT NULL,
			lease_until TEXT,
			provider_message_id TEXT,
			last_error_class TEXT,
			last_error_message TEXT,
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL,
			delivered_at TEXT
		)`,
		`CREATE INDEX IF NOT EXISTS deliveries_ready_idx
			ON deliveries (status, next_attempt_at, lease_until)`,
		`CREATE TABLE IF NOT EXISTS identities (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			provider TEXT NOT NULL,
			provider_id TEXT NOT NULL DEFAULT '',
			username TEXT NOT NULL DEFAULT '',
			email TEXT NOT NULL DEFAULT '',
			display_name TEXT NOT NULL DEFAULT '',
			enabled INTEGER NOT NULL DEFAULT 1,
			updated_at TEXT NOT NULL
		)`,
		`CREATE UNIQUE INDEX IF NOT EXISTS identities_provider_id_idx
			ON identities (provider, provider_id) WHERE provider_id <> ''`,
		`CREATE UNIQUE INDEX IF NOT EXISTS identities_username_idx
			ON identities (provider, username) WHERE username <> ''`,
		`CREATE INDEX IF NOT EXISTS identities_email_idx ON identities (email)`,
		`CREATE TABLE IF NOT EXISTS notification_preferences (
			email TEXT PRIMARY KEY,
			enabled INTEGER NOT NULL DEFAULT 1,
			ci_failed INTEGER NOT NULL DEFAULT 1,
			ci_recovered INTEGER NOT NULL DEFAULT 1,
			mr_review_requested INTEGER NOT NULL DEFAULT 1,
			mr_assigned INTEGER NOT NULL DEFAULT 1,
			mr_updated INTEGER NOT NULL DEFAULT 1,
			mr_approved INTEGER NOT NULL DEFAULT 1,
			mr_state_changed INTEGER NOT NULL DEFAULT 1,
			mr_comment INTEGER NOT NULL DEFAULT 1,
			mention INTEGER NOT NULL DEFAULT 1,
			issue_assigned INTEGER NOT NULL DEFAULT 1,
			issue_updated INTEGER NOT NULL DEFAULT 0,
			project_include TEXT NOT NULL DEFAULT '',
			project_exclude TEXT NOT NULL DEFAULT '',
			updated_at TEXT NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS pipeline_states (
			correlation_key TEXT PRIMARY KEY,
			status TEXT NOT NULL,
			recipients_json TEXT NOT NULL,
			updated_at TEXT NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS hook_states (
			scope TEXT NOT NULL,
			external_id TEXT NOT NULL,
			url TEXT NOT NULL,
			config_json TEXT NOT NULL,
			updated_at TEXT NOT NULL,
			PRIMARY KEY (scope, external_id)
		)`,
		`CREATE TABLE IF NOT EXISTS unresolved_identities (
			event_key TEXT NOT NULL,
			provider TEXT NOT NULL,
			provider_id TEXT NOT NULL DEFAULT '',
			username TEXT NOT NULL DEFAULT '',
			first_seen TEXT NOT NULL,
			last_seen TEXT NOT NULL,
			occurrences INTEGER NOT NULL DEFAULT 1,
			PRIMARY KEY (event_key, provider, provider_id, username)
		)`,
	}
	for _, statement := range statements {
		if _, err := s.db.ExecContext(ctx, statement); err != nil {
			return fmt.Errorf("migrate SQLite: %w", err)
		}
	}
	if err := s.ensureColumn(ctx, "deliveries", "retryable", "INTEGER NOT NULL DEFAULT 1"); err != nil {
		return err
	}
	if _, err := s.db.ExecContext(ctx, `INSERT OR IGNORE INTO schema_migrations(version, applied_at) VALUES (1, ?)`, time.Now().UTC().Format(time.RFC3339Nano)); err != nil {
		return fmt.Errorf("record SQLite migration: %w", err)
	}
	if _, err := s.db.ExecContext(ctx, `INSERT OR IGNORE INTO schema_migrations(version, applied_at) VALUES (2, ?)`, time.Now().UTC().Format(time.RFC3339Nano)); err != nil {
		return fmt.Errorf("record SQLite retryable migration: %w", err)
	}
	return nil
}

func (s *Store) ensureColumn(ctx context.Context, table, column, definition string) error {
	rows, err := s.db.QueryContext(ctx, `PRAGMA table_info(`+table+`)`)
	if err != nil {
		return fmt.Errorf("inspect SQLite table %s: %w", table, err)
	}
	defer rows.Close()
	var present bool
	for rows.Next() {
		var cid int
		var name, columnType string
		var notNull, primaryKey int
		var defaultValue sql.NullString
		if err := rows.Scan(&cid, &name, &columnType, &notNull, &defaultValue, &primaryKey); err != nil {
			return fmt.Errorf("scan SQLite table %s: %w", table, err)
		}
		if name == column {
			present = true
			break
		}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate SQLite table %s: %w", table, err)
	}
	if present {
		return nil
	}
	if _, err := s.db.ExecContext(ctx, `ALTER TABLE `+table+` ADD COLUMN `+column+` `+definition); err != nil {
		return fmt.Errorf("add SQLite column %s.%s: %w", table, column, err)
	}
	return nil
}

func (s *Store) Enqueue(ctx context.Context, event domain.CanonicalEvent, deliveries []domain.Delivery) error {
	return s.enqueue(ctx, event, deliveries, nil)
}

func (s *Store) RecordIgnored(ctx context.Context, eventKey, sourceVersion, sourceEvent string) error {
	if eventKey == "" {
		return errors.New("event key is required")
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	_, err := s.db.ExecContext(ctx, `
		INSERT OR IGNORE INTO events(
			event_key, source, source_version, source_event, kind, action, status,
			event_json, received_at, created_at
		) VALUES (?, 'gitlab', ?, ?, 'ignored', '', 'ignored', '{}', ?, ?)`,
		eventKey, sourceVersion, sourceEvent, now, now)
	if err != nil {
		return fmt.Errorf("record ignored event: %w", err)
	}
	return nil
}

func (s *Store) EnqueueWithPipelineState(ctx context.Context, event domain.CanonicalEvent, deliveries []domain.Delivery, state *domain.PipelineState) error {
	return s.enqueue(ctx, event, deliveries, state)
}

func (s *Store) enqueue(ctx context.Context, event domain.CanonicalEvent, deliveries []domain.Delivery, state *domain.PipelineState) error {
	if event.EventKey == "" {
		return errors.New("event key is required")
	}
	eventJSON, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("marshal event: %w", err)
	}
	now := time.Now().UTC()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin enqueue transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	_, err = tx.ExecContext(ctx, `
		INSERT OR IGNORE INTO events(
			event_key, source, source_version, source_event, kind, action, status,
			event_json, project_id, received_at, occurred_at, created_at
		) VALUES (?, ?, ?, ?, ?, ?, 'accepted', ?, ?, ?, ?, ?)`,
		event.EventKey,
		event.Source,
		event.SourceVersion,
		event.SourceEvent,
		event.Kind,
		event.Action,
		eventJSON,
		event.Project.ID,
		event.ReceivedAt.UTC().Format(time.RFC3339Nano),
		formatOptionalTime(event.OccurredAt),
		now.Format(time.RFC3339Nano),
	)
	if err != nil {
		return fmt.Errorf("insert event: %w", err)
	}

	for _, delivery := range deliveries {
		if delivery.Key == "" || delivery.Notification.Recipient.Value == "" {
			continue
		}
		notificationJSON, marshalErr := json.Marshal(delivery.Notification)
		if marshalErr != nil {
			return fmt.Errorf("marshal delivery %s: %w", delivery.Key, marshalErr)
		}
		_, err = tx.ExecContext(ctx, `
			INSERT OR IGNORE INTO deliveries(
				delivery_key, event_key, status, notification_json, attempts,
				next_attempt_at, created_at, updated_at
			) VALUES (?, ?, 'pending', ?, 0, ?, ?, ?)`,
			delivery.Key,
			event.EventKey,
			notificationJSON,
			now.Format(time.RFC3339Nano),
			now.Format(time.RFC3339Nano),
			now.Format(time.RFC3339Nano),
		)
		if err != nil {
			return fmt.Errorf("insert delivery %s: %w", delivery.Key, err)
		}
	}
	if state != nil {
		recipientsJSON, marshalErr := json.Marshal(state.Recipients)
		if marshalErr != nil {
			return fmt.Errorf("marshal pipeline state: %w", marshalErr)
		}
		updatedAt := state.UpdatedAt
		if updatedAt.IsZero() {
			updatedAt = now
		}
		_, err = tx.ExecContext(ctx, `
			INSERT INTO pipeline_states(correlation_key, status, recipients_json, updated_at)
			VALUES (?, ?, ?, ?)
			ON CONFLICT(correlation_key) DO UPDATE SET status = excluded.status, recipients_json = excluded.recipients_json, updated_at = excluded.updated_at`,
			state.CorrelationKey, state.Status, recipientsJSON, updatedAt.UTC().Format(time.RFC3339Nano))
		if err != nil {
			return fmt.Errorf("write pipeline state in enqueue transaction: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit enqueue transaction: %w", err)
	}
	return nil
}

func (s *Store) Claim(ctx context.Context, workerID string, limit int) ([]domain.Delivery, error) {
	if limit <= 0 {
		limit = 10
	}
	now := time.Now().UTC()
	leaseUntil := now.Add(s.config.LeaseDuration)
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin claim transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	rows, err := tx.QueryContext(ctx, `
		SELECT delivery_key, notification_json
		FROM deliveries
		WHERE (
			(status = 'pending' AND next_attempt_at <= ? AND attempts < ?)
			OR (status = 'failed' AND retryable = 1 AND next_attempt_at <= ? AND attempts < ?)
			OR (status = 'processing' AND lease_until IS NOT NULL AND lease_until <= ? AND attempts < ?)
		)
		ORDER BY next_attempt_at, created_at
		LIMIT ?`,
		now.Format(time.RFC3339Nano), s.config.MaxAttempts, now.Format(time.RFC3339Nano), s.config.MaxAttempts, now.Format(time.RFC3339Nano), s.config.MaxAttempts, limit)
	if err != nil {
		return nil, fmt.Errorf("query outbox: %w", err)
	}
	defer rows.Close()

	type claimed struct {
		key  string
		data []byte
	}
	var selected []claimed
	for rows.Next() {
		var item claimed
		if err := rows.Scan(&item.key, &item.data); err != nil {
			return nil, fmt.Errorf("scan outbox: %w", err)
		}
		selected = append(selected, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate outbox: %w", err)
	}

	claimedDeliveries := make([]domain.Delivery, 0, len(selected))
	for _, item := range selected {
		result, updateErr := tx.ExecContext(ctx, `
			UPDATE deliveries
			SET status = 'processing', attempts = attempts + 1, lease_until = ?, updated_at = ?
			WHERE delivery_key = ? AND (
				(status = 'pending' AND next_attempt_at <= ? AND attempts < ?)
				OR (status = 'failed' AND retryable = 1 AND next_attempt_at <= ? AND attempts < ?)
				OR (status = 'processing' AND lease_until IS NOT NULL AND lease_until <= ? AND attempts < ?)
			)`,
			leaseUntil.Format(time.RFC3339Nano), now.Format(time.RFC3339Nano), item.key,
			now.Format(time.RFC3339Nano), s.config.MaxAttempts, now.Format(time.RFC3339Nano), s.config.MaxAttempts, now.Format(time.RFC3339Nano), s.config.MaxAttempts)
		if updateErr != nil {
			return nil, fmt.Errorf("claim delivery %s: %w", item.key, updateErr)
		}
		count, countErr := result.RowsAffected()
		if countErr != nil || count == 0 {
			continue
		}
		var notification domain.Notification
		if err := json.Unmarshal(item.data, &notification); err != nil {
			return nil, fmt.Errorf("decode delivery %s: %w", item.key, err)
		}
		claimedDeliveries = append(claimedDeliveries, domain.Delivery{Key: item.key, Notification: notification})
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit claim transaction for %s: %w", workerID, err)
	}
	return claimedDeliveries, nil
}

func (s *Store) MarkDelivered(ctx context.Context, key string, receipt domain.DeliveryReceipt) error {
	if key == "" {
		return errors.New("delivery key is required")
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	result, err := s.db.ExecContext(ctx, `
		UPDATE deliveries
		SET status = 'delivered', lease_until = NULL, provider_message_id = ?,
			last_error_class = NULL, last_error_message = NULL, delivered_at = ?, updated_at = ?
		WHERE delivery_key = ?`, receipt.ProviderMessageID, now, now, key)
	if err != nil {
		return fmt.Errorf("mark delivery delivered: %w", err)
	}
	if count, err := result.RowsAffected(); err == nil && count == 0 {
		return fmt.Errorf("delivery not found: %s", key)
	}
	return nil
}

func (s *Store) MarkFailed(ctx context.Context, key string, failure domain.DeliveryFailure) error {
	if key == "" {
		return errors.New("delivery key is required")
	}
	var attempts int
	if err := s.db.QueryRowContext(ctx, `SELECT attempts FROM deliveries WHERE delivery_key = ?`, key).Scan(&attempts); err != nil {
		return fmt.Errorf("read delivery attempts: %w", err)
	}
	now := time.Now().UTC()
	status := "failed"
	nextAttempt := now
	if failure.Retryable && attempts < s.config.MaxAttempts {
		status = "pending"
		backoff := s.config.RetryDelay
		for step := 1; step < attempts; step++ {
			backoff *= 2
			if backoff >= time.Hour {
				backoff = time.Hour
				break
			}
		}
		nextAttempt = now.Add(backoff)
	}
	_, err := s.db.ExecContext(ctx, `
		UPDATE deliveries
		SET status = ?, retryable = ?, next_attempt_at = ?, lease_until = NULL,
			last_error_class = ?, last_error_message = ?, updated_at = ?
		WHERE delivery_key = ?`,
		status, boolInt(failure.Retryable && attempts < s.config.MaxAttempts), nextAttempt.Format(time.RFC3339Nano), failure.Class, truncateError(failure.Message), now.Format(time.RFC3339Nano), key)
	if err != nil {
		return fmt.Errorf("mark delivery failed: %w", err)
	}
	return nil
}

func (s *Store) Resolve(ctx context.Context, identities []domain.Identity) ([]domain.Identity, error) {
	resolved := make([]domain.Identity, 0, len(identities))
	for _, identity := range identities {
		if usableEmail(identity.Email) != "" {
			identity.Email = usableEmail(identity.Email)
			resolved = append(resolved, identity)
			continue
		}
		var mapped domain.Identity
		var enabled int
		query := `SELECT provider_id, username, email, display_name, enabled FROM identities WHERE provider = ? AND enabled = 1 AND `
		var args []any
		provider := identity.Provider
		if provider == "" {
			provider = domain.ProviderGitLab
		}
		if identity.ProviderID != "" {
			query += `provider_id = ? ORDER BY updated_at DESC LIMIT 1`
			args = []any{string(provider), identity.ProviderID}
		} else if identity.Username != "" {
			query += `username = ? ORDER BY updated_at DESC LIMIT 1`
			args = []any{string(provider), strings.ToLower(identity.Username)}
		} else {
			resolved = append(resolved, identity)
			continue
		}
		if err := s.db.QueryRowContext(ctx, query, args...).Scan(&mapped.ProviderID, &mapped.Username, &mapped.Email, &mapped.Name, &enabled); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				resolved = append(resolved, identity)
				continue
			}
			return nil, fmt.Errorf("resolve identity: %w", err)
		}
		if enabled == 1 {
			mapped.Provider = provider
			if identity.ProviderID == "" {
				mapped.ProviderID = identity.ProviderID
			}
			if identity.Username == "" {
				mapped.Username = identity.Username
			}
			resolved = append(resolved, mapped)
		}
	}
	return resolved, nil
}

func (s *Store) Upsert(ctx context.Context, provider domain.Provider, identity domain.Identity, enabled bool) error {
	if usableEmail(identity.Email) == "" {
		return errors.New("a usable identity email is required")
	}
	identity.Email = usableEmail(identity.Email)
	identity.Username = strings.ToLower(strings.TrimSpace(identity.Username))
	now := time.Now().UTC().Format(time.RFC3339Nano)
	var idByProviderID, idByUsername sql.NullInt64
	var usernameProviderID string
	if identity.ProviderID != "" {
		if err := s.db.QueryRowContext(ctx, `SELECT id FROM identities WHERE provider = ? AND provider_id = ? LIMIT 1`, provider, identity.ProviderID).Scan(&idByProviderID); err != nil && !errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("find identity by provider ID: %w", err)
		}
	}
	if identity.Username != "" {
		if err := s.db.QueryRowContext(ctx, `SELECT id, provider_id FROM identities WHERE provider = ? AND username = ? LIMIT 1`, provider, identity.Username).Scan(&idByUsername, &usernameProviderID); err != nil && !errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("find identity by username: %w", err)
		}
	}
	if idByUsername.Valid && identity.ProviderID != "" && usernameProviderID != "" && identity.ProviderID != usernameProviderID {
		return fmt.Errorf("identity mapping conflict for provider %s and username %s", provider, identity.Username)
	}
	if idByProviderID.Valid && idByUsername.Valid && idByProviderID.Int64 != idByUsername.Int64 {
		return fmt.Errorf("identity mapping conflict for provider %s and username %s", provider, identity.Username)
	}
	var id int64
	if idByProviderID.Valid {
		id = idByProviderID.Int64
	} else if idByUsername.Valid {
		id = idByUsername.Int64
	}
	if id > 0 {
		_, err := s.db.ExecContext(ctx, `UPDATE identities SET provider_id = ?, username = ?, email = ?, display_name = ?, enabled = ?, updated_at = ? WHERE id = ?`, identity.ProviderID, identity.Username, identity.Email, identity.Name, boolInt(enabled), now, id)
		if err != nil {
			return fmt.Errorf("update identity: %w", err)
		}
		return nil
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO identities(provider, provider_id, username, email, display_name, enabled, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?)`, provider, identity.ProviderID, identity.Username, identity.Email, identity.Name, boolInt(enabled), now)
	if err != nil {
		return fmt.Errorf("insert identity: %w", err)
	}
	return nil
}

func (s *Store) DisableExcept(ctx context.Context, provider domain.Provider, enabledProviderIDs []string) (int, error) {
	now := time.Now().UTC().Format(time.RFC3339Nano)
	query := `UPDATE identities SET enabled = 0, updated_at = ? WHERE provider = ?`
	args := []any{now, provider}
	if len(enabledProviderIDs) > 0 {
		placeholders := make([]string, len(enabledProviderIDs))
		for index, providerID := range enabledProviderIDs {
			placeholders[index] = "?"
			args = append(args, providerID)
		}
		query += ` AND provider_id NOT IN (` + strings.Join(placeholders, ",") + `)`
	}
	result, err := s.db.ExecContext(ctx, query, args...)
	if err != nil {
		return 0, fmt.Errorf("disable stale identities: %w", err)
	}
	count, err := result.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("count disabled identities: %w", err)
	}
	return int(count), nil
}

func (s *Store) UpsertPreference(ctx context.Context, preference Preference) error {
	email := usableEmail(preference.Email)
	if email == "" {
		return errors.New("a usable preference email is required")
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO notification_preferences(
			email, enabled, ci_failed, ci_recovered, mr_review_requested, mr_assigned,
			mr_updated, mr_approved, mr_state_changed, mr_comment, mention,
			issue_assigned, issue_updated, project_include, project_exclude, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(email) DO UPDATE SET
			enabled = excluded.enabled,
			ci_failed = excluded.ci_failed,
			ci_recovered = excluded.ci_recovered,
			mr_review_requested = excluded.mr_review_requested,
			mr_assigned = excluded.mr_assigned,
			mr_updated = excluded.mr_updated,
			mr_approved = excluded.mr_approved,
			mr_state_changed = excluded.mr_state_changed,
			mr_comment = excluded.mr_comment,
			mention = excluded.mention,
			issue_assigned = excluded.issue_assigned,
			issue_updated = excluded.issue_updated,
			project_include = excluded.project_include,
			project_exclude = excluded.project_exclude,
			updated_at = excluded.updated_at`,
		email,
		boolInt(preference.Enabled),
		boolInt(preference.CIFailed),
		boolInt(preference.CIRecovered),
		boolInt(preference.MRReviewRequested),
		boolInt(preference.MRAssigned),
		boolInt(preference.MRUpdated),
		boolInt(preference.MRApproved),
		boolInt(preference.MRStateChanged),
		boolInt(preference.MRComment),
		boolInt(preference.Mention),
		boolInt(preference.IssueAssigned),
		boolInt(preference.IssueUpdated),
		preference.ProjectInclude,
		preference.ProjectExclude,
		now,
	)
	if err != nil {
		return fmt.Errorf("upsert notification preference: %w", err)
	}
	return nil
}

func (s *Store) Record(ctx context.Context, eventKey string, identities []domain.Identity) error {
	if eventKey == "" {
		return errors.New("event key is required")
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	for _, identity := range identities {
		provider := identity.Provider
		if provider == "" {
			provider = domain.ProviderGitLab
		}
		if identity.ProviderID == "" && identity.Username == "" {
			continue
		}
		_, err := s.db.ExecContext(ctx, `
			INSERT INTO unresolved_identities(event_key, provider, provider_id, username, first_seen, last_seen, occurrences)
			VALUES (?, ?, ?, ?, ?, ?, 1)
			ON CONFLICT(event_key, provider, provider_id, username) DO UPDATE SET last_seen = excluded.last_seen, occurrences = unresolved_identities.occurrences + 1`,
			eventKey, provider, identity.ProviderID, strings.ToLower(identity.Username), now, now)
		if err != nil {
			return fmt.Errorf("record unresolved identity: %w", err)
		}
	}
	return nil
}

func (s *Store) Enabled(ctx context.Context, address domain.RecipientAddress, kind domain.EventKind, reasonCode, projectPath string) (bool, error) {
	email := usableEmail(address.Value)
	if email == "" {
		return false, nil
	}
	columns := map[string]string{
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
	column := columns[reasonCode]
	if column == "" {
		return true, nil
	}
	query := fmt.Sprintf(`SELECT enabled, %s, project_include, project_exclude FROM notification_preferences WHERE email = ?`, column)
	var enabled, reasonEnabled int
	var include, exclude string
	if err := s.db.QueryRowContext(ctx, query, email).Scan(&enabled, &reasonEnabled, &include, &exclude); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return reasonCode != "issue_updated", nil
		}
		return false, fmt.Errorf("read notification preference: %w", err)
	}
	if enabled == 0 || reasonEnabled == 0 {
		return false, nil
	}
	if !projectAllowed(projectPath, include, exclude) {
		return false, nil
	}
	return true, nil
}

func (s *Store) Get(ctx context.Context, key string) (*domain.PipelineState, error) {
	var state domain.PipelineState
	var recipientsJSON string
	var updatedAt string
	if err := s.db.QueryRowContext(ctx, `SELECT correlation_key, status, recipients_json, updated_at FROM pipeline_states WHERE correlation_key = ?`, key).Scan(&state.CorrelationKey, &state.Status, &recipientsJSON, &updatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("read pipeline state: %w", err)
	}
	if err := json.Unmarshal([]byte(recipientsJSON), &state.Recipients); err != nil {
		return nil, fmt.Errorf("decode pipeline state: %w", err)
	}
	state.UpdatedAt, _ = time.Parse(time.RFC3339Nano, updatedAt)
	return &state, nil
}

func (s *Store) Put(ctx context.Context, state domain.PipelineState) error {
	recipientsJSON, err := json.Marshal(state.Recipients)
	if err != nil {
		return fmt.Errorf("marshal pipeline state: %w", err)
	}
	if state.UpdatedAt.IsZero() {
		state.UpdatedAt = time.Now().UTC()
	}
	_, err = s.db.ExecContext(ctx, `
		INSERT INTO pipeline_states(correlation_key, status, recipients_json, updated_at)
		VALUES (?, ?, ?, ?)
		ON CONFLICT(correlation_key) DO UPDATE SET status = excluded.status, recipients_json = excluded.recipients_json, updated_at = excluded.updated_at`,
		state.CorrelationKey, state.Status, recipientsJSON, state.UpdatedAt.UTC().Format(time.RFC3339Nano))
	if err != nil {
		return fmt.Errorf("write pipeline state: %w", err)
	}
	return nil
}

func (s *Store) SaveHookState(ctx context.Context, state domain.HookState) error {
	if state.Scope == "" || state.ExternalID == "" || state.URL == "" {
		return errors.New("hook state scope, external ID, and URL are required")
	}
	configJSON, err := json.Marshal(state.Config)
	if err != nil {
		return fmt.Errorf("marshal hook state: %w", err)
	}
	if state.UpdatedAt.IsZero() {
		state.UpdatedAt = time.Now().UTC()
	}
	_, err = s.db.ExecContext(ctx, `
		INSERT INTO hook_states(scope, external_id, url, config_json, updated_at)
		VALUES (?, ?, ?, ?, ?)
		ON CONFLICT(scope, external_id) DO UPDATE SET url = excluded.url, config_json = excluded.config_json, updated_at = excluded.updated_at`,
		state.Scope, state.ExternalID, state.URL, configJSON, state.UpdatedAt.UTC().Format(time.RFC3339Nano))
	if err != nil {
		return fmt.Errorf("save hook state: %w", err)
	}
	return nil
}

func usableEmail(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	if value == "" || value == "[redacted]" || strings.Contains(value, "noreply") {
		return ""
	}
	parsed, err := mail.ParseAddress(value)
	if err != nil || parsed.Address != value || !strings.Contains(value, "@") {
		return ""
	}
	return value
}

func projectAllowed(projectPath, include, exclude string) bool {
	projectPath = strings.TrimSpace(projectPath)
	if projectPath == "" {
		return true
	}
	if values := splitProjectList(include); len(values) > 0 && !matchesProject(projectPath, values) {
		return false
	}
	if values := splitProjectList(exclude); len(values) > 0 && matchesProject(projectPath, values) {
		return false
	}
	return true
}

func splitProjectList(value string) []string {
	var raw []string
	if strings.HasPrefix(strings.TrimSpace(value), "[") {
		_ = json.Unmarshal([]byte(value), &raw)
	} else {
		raw = strings.Split(value, ",")
	}
	result := make([]string, 0, len(raw))
	for _, item := range raw {
		if item = strings.TrimSpace(item); item != "" {
			result = append(result, item)
		}
	}
	return result
}

func matchesProject(project string, patterns []string) bool {
	for _, pattern := range patterns {
		pattern = strings.TrimSpace(pattern)
		if pattern == project || strings.HasPrefix(project, strings.TrimRight(pattern, "/")+"/") {
			return true
		}
	}
	return false
}

func formatOptionalTime(value *time.Time) any {
	if value == nil {
		return nil
	}
	return value.UTC().Format(time.RFC3339Nano)
}

func boolInt(value bool) int {
	if value {
		return 1
	}
	return 0
}

func truncateError(value string) string {
	runes := []rune(value)
	if len(runes) <= 1000 {
		return value
	}
	return string(runes[:1000]) + "…"
}
