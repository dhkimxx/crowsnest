package application

import (
	"fmt"

	"github.com/dhkimxx/crowsnest/internal/domain"
)

var toggleableReasons = map[string]struct{}{
	ReasonCIFailed:          {},
	ReasonCIRecovered:       {},
	ReasonMRReviewRequested: {},
	ReasonMRAssigned:        {},
	ReasonMRUpdated:         {},
	ReasonMRApproved:        {},
	ReasonMRStateChanged:    {},
	ReasonMRComment:         {},
	ReasonMention:           {},
	ReasonIssueAssigned:     {},
	ReasonIssueUpdated:      {},
}

func notificationActions(event domain.CanonicalEvent, reasons []domain.NotificationReason) []domain.NotificationAction {
	actions := make([]domain.NotificationAction, 0, len(reasons))
	for _, reason := range reasons {
		if !toggleableReason(reason.Code) {
			continue
		}
		actions = append(actions, toggleReasonAction(reason.Code, notificationReasonTitle(event, reason.Code), true))
	}
	return actions
}

func toggleableReason(reasonCode string) bool {
	_, ok := toggleableReasons[reasonCode]
	return ok
}

func toggleReasonAction(reasonCode, reasonTitle string, enabled bool) domain.NotificationAction {
	action := domain.ActionMuteReason
	label := fmt.Sprintf("Mute %q", reasonTitle)
	if !enabled {
		action = domain.ActionUnmuteReason
		label = fmt.Sprintf("Unmute %q", reasonTitle)
	}
	return domain.NotificationAction{
		Action: action,
		Label:  label,
		Value:  map[string]string{"reason": reasonCode, "title": reasonTitle},
	}
}
