package helpscout

import "time"

// The structs below follow the public Help Scout Inbox API 2.0 documentation
// (developer.helpscout.com/mailbox-api). Only the fields the importer uses are decoded.

type hsPage struct {
	Number        int `json:"number"`
	Size          int `json:"size"`
	TotalElements int `json:"totalElements"`
	TotalPages    int `json:"totalPages"`
}

type hsMailbox struct {
	ID    int64  `json:"id"`
	Name  string `json:"name"`
	Email string `json:"email"`
}

type hsUser struct {
	ID        int64  `json:"id"`
	FirstName string `json:"firstName"`
	LastName  string `json:"lastName"`
	Email     string `json:"email"`
}

// hsPerson is the shape of primaryCustomer, assignee, createdBy and customer: type is "user",
// "customer" or "team".
type hsPerson struct {
	ID    int64  `json:"id"`
	Type  string `json:"type"`
	First string `json:"first"`
	Last  string `json:"last"`
	Email string `json:"email"`
}

type hsTag struct {
	ID  int64  `json:"id"`
	Tag string `json:"tag"`
}

type hsSource struct {
	Type string `json:"type"`
	Via  string `json:"via"`
}

type hsConversation struct {
	ID              int64      `json:"id"`
	Number          int64      `json:"number"`
	Threads         int        `json:"threads"`
	Type            string     `json:"type"`
	MailboxID       int64      `json:"mailboxId"`
	Status          string     `json:"status"`
	State           string     `json:"state"`
	Subject         string     `json:"subject"`
	Assignee        *hsPerson  `json:"assignee"`
	PrimaryCustomer hsPerson   `json:"primaryCustomer"`
	CreatedAt       time.Time  `json:"createdAt"`
	ClosedAt        *time.Time `json:"closedAt"`
	UserUpdatedAt   *time.Time `json:"userUpdatedAt"`
	Source          hsSource   `json:"source"`
	Tags            []hsTag    `json:"tags"`
	CC              []string   `json:"cc"`
}

type hsAttachment struct {
	ID       int64  `json:"id"`
	Filename string `json:"filename"`
	MimeType string `json:"mimeType"`
	Size     int64  `json:"size"`
	State    string `json:"state"`
}

type hsThread struct {
	ID        int64     `json:"id"`
	Type      string    `json:"type"`
	State     string    `json:"state"`
	Body      string    `json:"body"`
	Source    hsSource  `json:"source"`
	CreatedBy hsPerson  `json:"createdBy"`
	To        []string  `json:"to"`
	CC        []string  `json:"cc"`
	BCC       []string  `json:"bcc"`
	CreatedAt time.Time `json:"createdAt"`
	Embedded  struct {
		Attachments []hsAttachment `json:"attachments"`
	} `json:"_embedded"`
}

type hsCustomer struct {
	ID        int64     `json:"id"`
	FirstName string    `json:"firstName"`
	LastName  string    `json:"lastName"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
	Embedded  struct {
		Emails []struct {
			Value string `json:"value"`
		} `json:"emails"`
	} `json:"_embedded"`
}

// updatedAt is the conversation's last change Help Scout reports.
func (c hsConversation) updatedAt() time.Time {
	latest := c.CreatedAt
	for _, t := range []*time.Time{c.UserUpdatedAt, c.ClosedAt} {
		if t != nil && t.After(latest) {
			latest = *t
		}
	}
	return latest.UTC()
}

func fullName(first, last string) string {
	return trimSpace(first + " " + last)
}
