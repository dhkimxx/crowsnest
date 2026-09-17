package domain

import "time"

const (
	InteractionApplied   = "applied"
	InteractionDuplicate = "duplicate"
	InteractionRejected  = "rejected"
	InteractionIgnored   = "ignored"
)

type Interaction struct {
	EventID   string
	MessageID string
	ActorID   string
	Action    string
	Value     map[string]string
}

type PreferenceState struct {
	Muted      bool       `json:"muted"`
	MutedUntil *time.Time `json:"muted_until,omitempty"`
}

type InteractionResult struct {
	Status       string
	Notification *Notification
	Settings     *PreferenceState
	Toast        string
}
