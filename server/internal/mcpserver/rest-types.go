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
	// TimezoneOverride is set only in the answer of /auth/me.
	TimezoneOverride *string `json:"timezone_override"`
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
	Channel    string    `json:"channel"`
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
}

type restDelivery struct {
	Status string `json:"status"`
}

type restMessage struct {
	ID            string    `json:"id"`
	Kind          string    `json:"kind"`
	Body          string    `json:"body"`
	AuthorAgentID *string   `json:"author_agent_id"`
	AuthorName    *string   `json:"author_name"`
	CreatedAt     time.Time `json:"created_at"`
	AgentToken    *struct {
		Label string `json:"label"`
		Scope string `json:"scope"`
	} `json:"agent_token"`
	// Origin is set on a message imported from another helpdesk; Kind is how that system classified it.
	Origin *struct {
		Kind string `json:"kind"`
	} `json:"origin"`
	Delivery *restDelivery `json:"delivery"`
	CC       []string      `json:"cc"`
	Email    *struct {
		From string `json:"from"`
	} `json:"email"`
	Attachments []struct {
		ID       string `json:"id"`
		Filename string `json:"filename"`
		Size     int64  `json:"size"`
	} `json:"attachments"`
}

type restContactDetail struct {
	restContact
	Conversations []restConversation `json:"conversations"`
}

type restTimeEntry struct {
	ID         string    `json:"id"`
	AgentID    *string   `json:"agent_id"`
	AgentName  string    `json:"agent_name"`
	Minutes    int       `json:"minutes"`
	Note       string    `json:"note"`
	Billable   bool      `json:"billable"`
	ExecutedAt time.Time `json:"executed_at"`
}

type restActivityItem struct {
	ID          string     `json:"id"`
	Subject     string     `json:"subject"`
	ContactName string     `json:"contact_name"`
	AssigneeID  *string    `json:"assignee_id"`
	CreatedAt   time.Time  `json:"created_at"`
	AssignedAt  time.Time  `json:"assigned_at"`
	UpdatedAt   time.Time  `json:"updated_at"`
	IncomingAt  *time.Time `json:"incoming_at"`
}

type restActivity struct {
	Now              time.Time          `json:"now"`
	NewConversations []restActivityItem `json:"new_conversations"`
	AssignedToMe     []restActivityItem `json:"assigned_to_me"`
	Updated          []restActivityItem `json:"updated"`
}
