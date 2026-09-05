package main

import (
	"context"
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strconv"
	"strings"

	"github.com/dhkimxx/crowsnest/internal/adapter/database/sqlite"
	"github.com/dhkimxx/crowsnest/internal/domain"
	"github.com/dhkimxx/crowsnest/internal/platform/config"
)

func importUsers(logger *slog.Logger) error {
	path, err := requiredFileArgument("import-users")
	if err != nil {
		return err
	}
	settings := config.Load()
	store, err := sqlite.Open(settings.DBPath, sqlite.DefaultConfig())
	if err != nil {
		return err
	}
	defer store.Close()

	file, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("open users CSV: %w", err)
	}
	defer file.Close()
	reader := csv.NewReader(file)
	header, err := reader.Read()
	if err != nil {
		return fmt.Errorf("read users CSV header: %w", err)
	}
	columns := columnIndexes(header)
	for rowNumber := 2; ; rowNumber++ {
		row, readErr := reader.Read()
		if errors.Is(readErr, io.EOF) {
			break
		}
		if readErr != nil {
			return fmt.Errorf("read users CSV row %d: %w", rowNumber, readErr)
		}
		identity := domain.Identity{
			Provider:   domain.ProviderGitLab,
			ProviderID: rowValue(row, columns, "gitlab_user_id"),
			Username:   strings.ToLower(rowValue(row, columns, "gitlab_username")),
			Email:      rowValue(row, columns, "email"),
			Name:       rowValue(row, columns, "display_name"),
		}
		if identity.ProviderID == "" && identity.Username == "" {
			return fmt.Errorf("users CSV row %d has neither gitlab_user_id nor gitlab_username", rowNumber)
		}
		enabled := parseBoolDefault(rowValue(row, columns, "enabled"), true)
		if err := store.Upsert(context.Background(), domain.ProviderGitLab, identity, enabled); err != nil {
			return fmt.Errorf("import users CSV row %d: %w", rowNumber, err)
		}
	}
	logger.Info("user mappings imported", "file", path)
	return nil
}

func importPreferences(logger *slog.Logger) error {
	path, err := requiredFileArgument("import-preferences")
	if err != nil {
		return err
	}
	settings := config.Load()
	store, err := sqlite.Open(settings.DBPath, sqlite.DefaultConfig())
	if err != nil {
		return err
	}
	defer store.Close()

	file, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("open preferences CSV: %w", err)
	}
	defer file.Close()
	reader := csv.NewReader(file)
	header, err := reader.Read()
	if err != nil {
		return fmt.Errorf("read preferences CSV header: %w", err)
	}
	columns := columnIndexes(header)
	for rowNumber := 2; ; rowNumber++ {
		row, readErr := reader.Read()
		if errors.Is(readErr, io.EOF) {
			break
		}
		if readErr != nil {
			return fmt.Errorf("read preferences CSV row %d: %w", rowNumber, readErr)
		}
		preference := sqlite.Preference{
			Email:             rowValue(row, columns, "email"),
			Enabled:           parseBoolDefault(rowValue(row, columns, "enabled"), true),
			CIFailed:          parseBoolDefault(rowValue(row, columns, "ci_failed"), true),
			CIRecovered:       parseBoolDefault(rowValue(row, columns, "ci_recovered"), true),
			MRReviewRequested: parseBoolDefault(rowValue(row, columns, "mr_review_requested"), true),
			MRAssigned:        parseBoolDefault(rowValue(row, columns, "mr_assigned"), true),
			MRUpdated:         parseBoolDefault(rowValue(row, columns, "mr_updated"), true),
			MRApproved:        parseBoolDefault(rowValue(row, columns, "mr_approved"), true),
			MRStateChanged:    parseBoolDefault(rowValue(row, columns, "mr_state_changed"), true),
			MRComment:         parseBoolDefault(rowValue(row, columns, "mr_comment"), true),
			Mention:           parseBoolDefault(rowValue(row, columns, "mention"), true),
			IssueAssigned:     parseBoolDefault(rowValue(row, columns, "issue_assigned"), true),
			IssueUpdated:      parseBoolDefault(rowValue(row, columns, "issue_updated"), false),
			ProjectInclude:    rowValue(row, columns, "project_include"),
			ProjectExclude:    rowValue(row, columns, "project_exclude"),
		}
		if err := store.UpsertPreference(context.Background(), preference); err != nil {
			return fmt.Errorf("import preferences CSV row %d: %w", rowNumber, err)
		}
	}
	logger.Info("notification preferences imported", "file", path)
	return nil
}

func requiredFileArgument(command string) (string, error) {
	for index, argument := range os.Args {
		if argument == "--file" && index+1 < len(os.Args) && strings.TrimSpace(os.Args[index+1]) != "" {
			return os.Args[index+1], nil
		}
	}
	return "", fmt.Errorf("usage: crowsnest %s --file path.csv", command)
}

func columnIndexes(header []string) map[string]int {
	indexes := make(map[string]int, len(header))
	for index, column := range header {
		indexes[strings.TrimSpace(column)] = index
	}
	return indexes
}

func rowValue(row []string, indexes map[string]int, column string) string {
	index, ok := indexes[column]
	if !ok || index >= len(row) {
		return ""
	}
	return strings.TrimSpace(row[index])
}

func parseBoolDefault(value string, fallback bool) bool {
	if value == "" {
		return fallback
	}
	parsed, err := strconv.ParseBool(value)
	if err != nil {
		return fallback
	}
	return parsed
}
