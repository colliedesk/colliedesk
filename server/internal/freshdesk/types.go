package freshdesk

import "time"

// The structs below follow the public Freshdesk API v2 documentation
// (developers.freshdesk.com/api). Only the fields the importer uses are decoded.

type fdAgent struct {
	ID      int64 `json:"id"`
	Contact struct {
		Name  string `json:"name"`
		Email string `json:"email"`
	} `json:"contact"`
}

type fdCompany struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
}

type fdContact struct {
	ID        int64     `json:"id"`
	Name      string    `json:"name"`
	Email     *string   `json:"email"`
	CompanyID *int64    `json:"company_id"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type fdAttachment struct {
	ID            int64  `json:"id"`
	Name          string `json:"name"`
	ContentType   string `json:"content_type"`
	Size          int64  `json:"size"`
	FileSize      int64  `json:"file_size"`
	AttachmentURL string `json:"attachment_url"`
}

// reportedSize reads "size"; some documented examples call it "file_size".
func (a fdAttachment) reportedSize() int64 {
	if a.Size > 0 {
		return a.Size
	}
	return a.FileSize
}

type fdTicket struct {
	ID              int64          `json:"id"`
	Subject         string         `json:"subject"`
	Description     string         `json:"description"`
	DescriptionText string         `json:"description_text"`
	Status          int            `json:"status"`
	Priority        int            `json:"priority"`
	Source          int            `json:"source"`
	RequesterID     int64          `json:"requester_id"`
	ResponderID     *int64         `json:"responder_id"`
	Tags            []string       `json:"tags"`
	Deleted         bool           `json:"deleted"`
	Spam            bool           `json:"spam"`
	Attachments     []fdAttachment `json:"attachments"`
	Type            *string        `json:"type"`
	DueBy           *time.Time     `json:"due_by"`
	FrDueBy         *time.Time     `json:"fr_due_by"`
	CcEmails        []string       `json:"cc_emails"`
	FwdEmails       []string       `json:"fwd_emails"`
	ToEmails        []string       `json:"to_emails"`
	CreatedAt       time.Time      `json:"created_at"`
	UpdatedAt       time.Time      `json:"updated_at"`
}

type fdConversation struct {
	ID          int64          `json:"id"`
	Body        string         `json:"body"`
	BodyText    string         `json:"body_text"`
	Incoming    bool           `json:"incoming"`
	Private     bool           `json:"private"`
	UserID      int64          `json:"user_id"`
	Source      int            `json:"source"`
	FromEmail   *string        `json:"from_email"`
	ToEmails    []string       `json:"to_emails"`
	CcEmails    []string       `json:"cc_emails"`
	BccEmails   []string       `json:"bcc_emails"`
	Attachments []fdAttachment `json:"attachments"`
	CreatedAt   time.Time      `json:"created_at"`
}

type fdTimeEntry struct {
	ID         int64      `json:"id"`
	AgentID    *int64     `json:"agent_id"`
	Billable   bool       `json:"billable"`
	Note       string     `json:"note"`
	TimeSpent  string     `json:"time_spent"`
	ExecutedAt *time.Time `json:"executed_at"`
	CreatedAt  time.Time  `json:"created_at"`
}

type fdCannedFolder struct {
	ID             int64  `json:"id"`
	Name           string `json:"name"`
	Personal       bool   `json:"personal"`
	ResponsesCount int    `json:"responses_count"`
}

// fdCannedResponse: content is Freshdesk's plain-text version, content_html the HTML one.
type fdCannedResponse struct {
	ID          int64          `json:"id"`
	Title       string         `json:"title"`
	FolderID    int64          `json:"folder_id"`
	Content     string         `json:"content"`
	ContentHTML string         `json:"content_html"`
	Attachments []fdAttachment `json:"attachments"`
}
