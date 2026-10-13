package mail

// Body is a transient readable message. It is never part of the event or outbox.
type Body struct {
	Text            string `json:"text"`
	HTML            string `json:"html,omitempty"`
	HasRemoteImages bool   `json:"has_remote_images"`
	Truncated       bool   `json:"truncated"`
}
