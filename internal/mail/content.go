package mail

// Body is a transient readable message. It is never part of the event or outbox.
type Body struct {
	Text      string `json:"text"`
	Truncated bool   `json:"truncated"`
}
