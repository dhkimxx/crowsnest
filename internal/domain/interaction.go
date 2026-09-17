package domain

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

type InteractionResult struct {
	Status       string
	Notification *Notification
	Toast        string
}
