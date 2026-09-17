package domain

type AddressKind string

const AddressKindEmail AddressKind = "email"

type RecipientAddress struct {
	Kind        AddressKind `json:"kind"`
	Value       string      `json:"value"`
	DisplayName string      `json:"display_name,omitempty"`
}

type NotificationReason struct {
	Code string `json:"code"`
	Text string `json:"text"`
}

type NotificationLink struct {
	Label string `json:"label"`
	URL   string `json:"url"`
}

const (
	ActionMuteAll   = "mute_all"
	ActionUnmuteAll = "unmute_all"
)

type NotificationAction struct {
	Action string            `json:"action"`
	Label  string            `json:"label"`
	Value  map[string]string `json:"value,omitempty"`
}

type Notification struct {
	EventKey       string               `json:"event_key"`
	Kind           EventKind            `json:"kind"`
	Action         string               `json:"action"`
	Project        ProjectRef           `json:"project"`
	Object         ResourceRef          `json:"object"`
	Recipient      RecipientAddress     `json:"recipient"`
	Reasons        []NotificationReason `json:"reasons"`
	Title          string               `json:"title"`
	Summary        string               `json:"summary"`
	SourceText     string               `json:"source_text,omitempty"`
	URL            string               `json:"url,omitempty"`
	Facts          map[string]string    `json:"facts,omitempty"`
	FailedJobs     []string             `json:"failed_jobs,omitempty"`
	FailedJobLinks []NotificationLink   `json:"failed_job_links,omitempty"`
	RelatedLinks   []NotificationLink   `json:"related_links,omitempty"`
	Actions        []NotificationAction `json:"actions,omitempty"`
}

type Delivery struct {
	Key          string       `json:"key"`
	Notification Notification `json:"notification"`
}

type DeliveryReceipt struct {
	ProviderMessageID string `json:"provider_message_id,omitempty"`
}

type DeliveryFailure struct {
	Class     string `json:"class"`
	Retryable bool   `json:"retryable"`
	Message   string `json:"message"`
}
