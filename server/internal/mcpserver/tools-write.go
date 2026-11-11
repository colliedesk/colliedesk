package mcpserver

import (
	"fmt"
	"net/http"
	"net/url"
	"strings"
)

type updateArgs struct {
	ConversationID string   `json:"conversation_id"`
	Status         string   `json:"status"`
	Priority       string   `json:"priority"`
	Assignee       string   `json:"assignee"`
	AddTags        []string `json:"add_tags"`
	RemoveTags     []string `json:"remove_tags"`
	Notify         *bool    `json:"notify"`
}

type updateOut struct {
	Conversation stateOut `json:"conversation"`
	Timezone     string   `json:"timezone"`
}

func conversationUpdate(c *call, arguments map[string]any) (any, error) {
	args, err := decodeArguments[updateArgs](arguments)
	if err != nil {
		return nil, err
	}
	if args.Status == "" && args.Priority == "" && args.Assignee == "" && len(args.AddTags) == 0 && len(args.RemoveTags) == 0 {
		return nil, &toolError{"Nothing to change. Give at least one of status, priority, assignee, add_tags or remove_tags."}
	}
	c.notFound = notFoundConversation(args.ConversationID)
	// Everything is looked up and checked before the first write, so a bad name changes nothing.
	changes := map[string]any{}
	if args.Status != "" {
		changes["status"] = args.Status
		if args.Notify != nil {
			changes["notify"] = *args.Notify
		}
	}
	if args.Priority != "" {
		changes["priority"] = args.Priority
	}
	if args.Assignee != "" {
		assignee, err := c.resolveAssignee(args.Assignee)
		if err != nil {
			return nil, err
		}
		changes["assignee_id"] = assignee
	}
	add, missing, err := c.splitNewTags(args.AddTags)
	if err != nil {
		return nil, err
	}
	remove, err := c.resolveTags(args.RemoveTags)
	if err != nil {
		return nil, err
	}
	for _, tag := range add {
		for _, other := range remove {
			if tag.ID == other.ID {
				return nil, &toolError{fmt.Sprintf("The tag %q is in both add_tags and remove_tags. Choose one.", tag.Name)}
			}
		}
	}
	path := "/api/v1/conversations/" + url.PathEscape(args.ConversationID)
	if len(missing) > 0 {
		// A tag is created only for a conversation that exists, so a wrong id leaves nothing behind.
		if err := c.get(path, nil, nil); err != nil {
			return nil, err
		}
		for _, name := range missing {
			tag, err := c.createTag(name)
			if err != nil {
				return nil, err
			}
			add = append(add, tag)
		}
	}
	var latest restConversation
	if len(changes) > 0 {
		if err := c.send(http.MethodPatch, path, changes, &latest); err != nil {
			return nil, err
		}
	}
	for _, tag := range add {
		if err := c.send(http.MethodPut, path+"/tags/"+tag.ID, nil, &latest); err != nil {
			return nil, err
		}
	}
	for _, tag := range remove {
		if err := c.send(http.MethodDelete, path+"/tags/"+tag.ID, nil, &latest); err != nil {
			return nil, err
		}
	}
	me, err := c.me()
	if err != nil {
		return nil, err
	}
	agents, err := c.agentsByID()
	if err != nil {
		return nil, err
	}
	return updateOut{Conversation: state(latest, agents), Timezone: me.Workspace.Timezone}, nil
}

// resolveAssignee turns what a model wrote into an agent id; nil means unassigned.
func (c *call) resolveAssignee(who string) (*string, error) {
	who = strings.TrimSpace(who)
	if strings.EqualFold(who, "unassigned") {
		return nil, nil
	}
	if strings.EqualFold(who, "me") {
		me, err := c.me()
		if err != nil {
			return nil, err
		}
		return &me.Agent.ID, nil
	}
	agents, err := c.agentList()
	if err != nil {
		return nil, err
	}
	var matches []restAgent
	for _, agent := range agents {
		if strings.EqualFold(agent.ID, who) || strings.EqualFold(agent.Email, who) || strings.EqualFold(strings.TrimSpace(agent.DisplayName), who) {
			matches = append(matches, agent)
		}
	}
	switch len(matches) {
	case 1:
		return &matches[0].ID, nil
	case 0:
		return nil, &toolError{fmt.Sprintf("No team member matches %q. Team members: %s.", who, describeAgents(agents))}
	}
	return nil, &toolError{fmt.Sprintf("More than one team member matches %q. Use an email address: %s.", who, describeAgents(matches))}
}

func describeAgents(agents []restAgent) string {
	parts := make([]string, len(agents))
	for index, agent := range agents {
		parts[index] = agentName(agent) + " <" + agent.Email + ">"
	}
	return strings.Join(parts, ", ")
}

// splitNewTags sorts the names a caller wants on a conversation into tags that exist and names that
// do not, each once and ignoring case. Nothing is created here.
func (c *call) splitNewTags(names []string) (existing []restTag, missing []string, err error) {
	seen := map[string]bool{}
	for _, name := range names {
		name = strings.TrimSpace(name)
		if name == "" {
			return nil, nil, &toolError{"A tag name cannot be empty."}
		}
		tag, found, err := c.findTag(name)
		if err != nil {
			return nil, nil, err
		}
		key := strings.ToLower(name)
		if found {
			key = tag.ID
		}
		if seen[key] {
			continue
		}
		seen[key] = true
		if found {
			existing = append(existing, tag)
		} else {
			missing = append(missing, name)
		}
	}
	return existing, missing, nil
}

// resolveTags finds existing tags by name, each once.
func (c *call) resolveTags(names []string) ([]restTag, error) {
	var tags []restTag
	seen := map[string]bool{}
	for _, name := range names {
		tag, err := c.tagByName(name)
		if err != nil {
			return nil, err
		}
		if !seen[tag.ID] {
			seen[tag.ID] = true
			tags = append(tags, tag)
		}
	}
	return tags, nil
}

type noteArgs struct {
	ConversationID string          `json:"conversation_id"`
	Body           string          `json:"body"`
	Files          []inlineFileArg `json:"files"`
}

type noteOut struct {
	ConversationID string `json:"conversation_id"`
	Message        struct {
		ID                  string             `json:"id"`
		Kind                string             `json:"kind"`
		CreatedAt           string             `json:"created_at"`
		Author              string             `json:"author,omitempty"`
		WrittenByAgentToken *tokenOut          `json:"written_by_agent_token,omitempty"`
		Attachments         []attachmentRowOut `json:"attachments,omitempty"`
	} `json:"message"`
}

func noteAdd(c *call, arguments map[string]any) (any, error) {
	args, err := decodeArguments[noteArgs](arguments)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(args.Body) == "" {
		return nil, &toolError{"The note is empty. Write the note text in body."}
	}
	files, err := decodeFiles(args.Files)
	if err != nil {
		return nil, err
	}
	c.notFound = notFoundConversation(args.ConversationID)
	path := "/api/v1/conversations/" + url.PathEscape(args.ConversationID) + "/messages"
	var created restMessage
	if len(files) > 0 {
		contentType, form := messageForm([][2]string{{"kind", "note"}, {"body", args.Body}}, files)
		err = c.sendBytes(http.MethodPost, path, contentType, nil, form, &created)
	} else {
		err = c.send(http.MethodPost, path, map[string]any{"kind": "note", "body": args.Body}, &created)
	}
	if err != nil {
		return nil, err
	}
	out := noteOut{ConversationID: args.ConversationID}
	out.Message.ID, out.Message.Kind, out.Message.CreatedAt = created.ID, created.Kind, utc(created.CreatedAt)
	out.Message.Attachments = attachmentRows(created)
	if nativeTeamMessage(created) {
		out.Message.WrittenByAgentToken = tokenOf(created)
		// The note is saved already, so a failed team lookup only leaves the author's name out.
		if agents, err := c.agentsByID(); err == nil {
			out.Message.Author = authorOf(created, agents)
		}
	}
	return out, nil
}
