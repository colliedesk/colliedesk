package mcpserver

import "time"

// The shapes below are the parts of the REST answers (contracts/openapi.yaml) that tools read.

type restMe struct {
	Agent     restAgent `json:"agent"`
	Workspace struct {
		ID       string `json:"id"`
		Slug     string `json:"slug"`
		Name     string `json:"name"`
		Timezone string `json:"timezone"`
	} `json:"workspace"`
	Scope *string `json:"scope"`
	Plan  *struct {
		Locked bool `json:"locked"`
	} `json:"plan"`
}

type restAgent struct {
	ID          string `json:"id"`
	Email       string `json:"email"`
	DisplayName string `json:"display_name"`
	Role        string `json:"role"`
}

type restTag struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type restContact struct {
	ID           string    `json:"id"`
	Name         string    `json:"name"`
	Email        string    `json:"email"`
	Organization *string   `json:"organization"`
	CreatedAt    time.Time `json:"created_at"`
}

type restConversation struct {
	ID         string    `json:"id"`
	ContactID  string    `json:"contact_id"`
	Subject    string    `json:"subject"`
	Status     string    `json:"status"`
	Priority   string    `json:"priority"`
	AssigneeID *string   `json:"assignee_id"`
	Tags       []restTag `json:"tags"`
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
}

type restMessage struct {
	ID         string    `json:"id"`
	Kind       string    `json:"kind"`
	Body       string    `json:"body"`
	AuthorName *string   `json:"author_name"`
	CreatedAt  time.Time `json:"created_at"`
	Delivery   *struct {
		Status string `json:"status"`
	} `json:"delivery"`
	Email *struct {
		From string `json:"from"`
	} `json:"email"`
	Attachments []struct {
		Filename string `json:"filename"`
	} `json:"attachments"`
}
