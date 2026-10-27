package github

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// The calls the issue connectors make: as the person (their user token, held in memory by the caller and
// revoked after) to check they may write and to create the issue, its @claude comment or its Copilot
// assignment; and as the installation to close an issue and to find an issue Colliedesk marked.

// CopilotLogin is the account Copilot's coding agent is assigned as.
const CopilotLogin = "copilot-swe-agent[bot]"

// User is a GitHub account.
type User struct {
	ID    int64  `json:"id"`
	Login string `json:"login"`
}

// PersonRepository is a repository as the person sees it, with what they may do on it.
type PersonRepository struct {
	Repository
	Permissions struct {
		Admin    bool `json:"admin"`
		Maintain bool `json:"maintain"`
		Push     bool `json:"push"`
		Triage   bool `json:"triage"`
		Pull     bool `json:"pull"`
	} `json:"permissions"`
}

// RepositoryAsPerson reads a repository by its id as the person: ErrNotFound when they cannot see it.
// The permissions are theirs; the token reaches only what both the App and the person may.
func (a *App) RepositoryAsPerson(ctx context.Context, userToken string, repoID int64) (PersonRepository, error) {
	var repo PersonRepository
	err := a.get(ctx, "/repositories/"+strconv.FormatInt(repoID, 10), "Bearer "+userToken, &repo)
	return repo, err
}

// CreatedIssue is an issue GitHub created.
type CreatedIssue struct {
	Number  int    `json:"number"`
	NodeID  string `json:"node_id"`
	HTMLURL string `json:"html_url"`
	User    User   `json:"user"`
}

// CreateIssue creates an issue as the person.
func (a *App) CreateIssue(ctx context.Context, userToken, fullName, title, body string) (CreatedIssue, error) {
	var issue CreatedIssue
	err := a.call(ctx, http.MethodPost, "/repos/"+repoPath(fullName)+"/issues", "Bearer "+userToken, map[string]any{"title": title, "body": body}, &issue)
	return issue, err
}

// CommentAsUser comments on an issue as the person (the @claude comment that starts the workflow).
func (a *App) CommentAsUser(ctx context.Context, userToken, fullName string, number int, body string) error {
	return a.call(ctx, http.MethodPost, "/repos/"+repoPath(fullName)+"/issues/"+strconv.Itoa(number)+"/comments", "Bearer "+userToken, map[string]any{"body": body}, nil)
}

// AssignCopilot assigns the issue to Copilot's coding agent as the person.
func (a *App) AssignCopilot(ctx context.Context, userToken, fullName string, number int) error {
	var answer struct {
		Assignees []User `json:"assignees"`
	}
	if err := a.call(ctx, http.MethodPost, "/repos/"+repoPath(fullName)+"/issues/"+strconv.Itoa(number)+"/assignees", "Bearer "+userToken,
		map[string]any{"assignees": []string{CopilotLogin}}, &answer); err != nil {
		return err
	}
	// GitHub answers 201 and leaves out an assignee it could not add.
	for _, user := range answer.Assignees {
		if strings.EqualFold(user.Login, CopilotLogin) || strings.EqualFold(user.Login, "Copilot") {
			return nil
		}
	}
	return &APIError{Status: http.StatusUnprocessableEntity, Message: "Copilot was not assigned"}
}

// CopilotAssignable reports whether the person can assign Copilot's coding agent on the repository (it is
// among the repository's suggested actors that can be assigned).
func (a *App) CopilotAssignable(ctx context.Context, userToken, fullName string) (bool, error) {
	owner, name, _ := strings.Cut(fullName, "/")
	var answer struct {
		Repository *struct {
			SuggestedActors struct {
				Nodes []struct {
					Login string `json:"login"`
				} `json:"nodes"`
			} `json:"suggestedActors"`
		} `json:"repository"`
	}
	query := `query($owner:String!,$name:String!){repository(owner:$owner,name:$name){suggestedActors(capabilities:[CAN_BE_ASSIGNED],first:100){nodes{login}}}}`
	if err := a.graphqlAs(ctx, "Bearer "+userToken, query, map[string]any{"owner": owner, "name": name}, &answer); err != nil {
		return false, err
	}
	if answer.Repository == nil {
		return false, ErrNotFound
	}
	for _, node := range answer.Repository.SuggestedActors.Nodes {
		if strings.EqualFold(node.Login, "copilot-swe-agent") || strings.EqualFold(node.Login, CopilotLogin) {
			return true, nil
		}
	}
	return false, nil
}

// CloseIssue comments on an issue and closes it as not planned, as the installation (a person cancelled
// the fix in Colliedesk).
func (a *App) CloseIssue(ctx context.Context, installationID int64, fullName string, number int, comment string) error {
	token, err := a.installationToken(ctx, installationID)
	if err != nil {
		return err
	}
	path := "/repos/" + repoPath(fullName) + "/issues/" + strconv.Itoa(number)
	if comment != "" {
		if err := a.call(ctx, http.MethodPost, path+"/comments", "Bearer "+token, map[string]any{"body": comment}, nil); err != nil {
			return err
		}
	}
	return a.call(ctx, http.MethodPatch, path, "Bearer "+token, map[string]any{"state": "closed", "state_reason": "not_planned"}, nil)
}

// ListedIssue is an issue as a list shows it.
type ListedIssue struct {
	Number      int             `json:"number"`
	NodeID      string          `json:"node_id"`
	HTMLURL     string          `json:"html_url"`
	Body        string          `json:"body"`
	User        User            `json:"user"`
	PullRequest json.RawMessage `json:"pull_request"`
}

// FindIssue looks, as the installation, among the repository's issues created since the time given for
// the one whose body holds marker; found is false when none does.
func (a *App) FindIssue(ctx context.Context, installationID int64, fullName string, since time.Time, marker string) (ListedIssue, bool, error) {
	for page := 1; page <= maxPages; page++ {
		var issues []ListedIssue
		query := url.Values{"state": {"all"}, "since": {since.UTC().Format(time.RFC3339)}, "sort": {"created"}, "direction": {"asc"},
			"per_page": {"100"}, "page": {strconv.Itoa(page)}}
		if err := a.asInstallation(ctx, installationID, "/repos/"+repoPath(fullName)+"/issues?"+query.Encode(), &issues); err != nil {
			return ListedIssue{}, false, err
		}
		for _, issue := range issues {
			if len(issue.PullRequest) == 0 && strings.Contains(issue.Body, marker) {
				return issue, true, nil
			}
		}
		if len(issues) < 100 {
			break
		}
	}
	return ListedIssue{}, false, nil
}

// repoPath is owner/name escaped for a path.
func repoPath(fullName string) string {
	owner, name, _ := strings.Cut(fullName, "/")
	return url.PathEscape(owner) + "/" + url.PathEscape(name)
}

// call makes one REST call with a JSON body.
func (a *App) call(ctx context.Context, method, path, authorization string, body, into any) error {
	encoded, err := json.Marshal(body)
	if err != nil {
		return err
	}
	request, err := http.NewRequestWithContext(ctx, method, a.apiURL(path), bytes.NewReader(encoded))
	if err != nil {
		return err
	}
	request.Header.Set("Authorization", authorization)
	request.Header.Set("Content-Type", "application/json")
	return a.send(request, into)
}

// graphqlAs makes a GraphQL read with the authorization given (a person's token here).
func (a *App) graphqlAs(ctx context.Context, authorization, query string, variables map[string]any, into any) error {
	body, err := json.Marshal(map[string]any{"query": query, "variables": variables})
	if err != nil {
		return err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, a.apiURL("/graphql"), bytes.NewReader(body))
	if err != nil {
		return err
	}
	request.Header.Set("Authorization", authorization)
	request.Header.Set("Content-Type", "application/json")
	var answer struct {
		Data   json.RawMessage `json:"data"`
		Errors []struct {
			Type    string `json:"type"`
			Message string `json:"message"`
		} `json:"errors"`
	}
	if err := a.send(request, &answer); err != nil {
		return err
	}
	if len(answer.Errors) > 0 {
		if answer.Errors[0].Type == "NOT_FOUND" {
			return ErrNotFound
		}
		return &APIError{Status: http.StatusOK, Message: cut(answer.Errors[0].Message)}
	}
	return json.Unmarshal(answer.Data, into)
}
