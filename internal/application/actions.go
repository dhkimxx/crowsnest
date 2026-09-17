package application

import (
	"time"

	"github.com/dhkimxx/crowsnest/internal/domain"
)

const muteDuration = 30 * 24 * time.Hour

func notificationActions() []domain.NotificationAction {
	return []domain.NotificationAction{muteAction(false)}
}

func muteAction(muted bool) domain.NotificationAction {
	if muted {
		return domain.NotificationAction{Action: domain.ActionUnmuteAll, Label: "Unmute"}
	}
	return domain.NotificationAction{Action: domain.ActionMuteAll, Label: "Mute 30d"}
}
