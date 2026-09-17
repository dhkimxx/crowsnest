package application

import (
	"time"

	"github.com/dhkimxx/crowsnest/internal/domain"
)

const muteDuration = 30 * 24 * time.Hour

func notificationActions() []domain.NotificationAction {
	return []domain.NotificationAction{settingsAction()}
}

func settingsAction() domain.NotificationAction {
	return domain.NotificationAction{Action: domain.ActionOpenSettings, Label: "⚙ Settings"}
}
