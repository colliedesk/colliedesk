package githubtest

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// What the issue connectors do on GitHub: as a person (their user token) read a repository with their
// permissions, create an issue, comment on it, assign Copilot and ask whether Copilot can be assigned;
// as the installation comment on, close and list issues. Answers follow GitHub's documented shapes.

// IssueComment is a comment on an issue of the stand-in.
type IssueComment struct {
	Body string
	// Author is the person's login, or "app" for the installation.
	Author string
}

// CreatedIssue is an issue created through the stand-in, with what was written on it.
type CreatedIssue struct {
	RepoID    int64
	Number    int
	Title     string
	Body      string
	Author    string
	State     string
	Comments  []IssueComment
	Assignees []string
	CreatedAt time.Time
}

type bridgeState struct {
	created map[int64][]*CreatedIssue
	copilot map[int64]bool
	userIDs map[string]int64
	// fail answers the next calls of "METHOD path" with a status.
	fail map[string]int
}

func (b *bridgeState) init() {
	if b.created == nil {
		b.created, b.copilot, b.userIDs, b.fail = map[int64][]*CreatedIssue{}, map[int64]bool{}, map[string]int64{}, map[string]int{}
	}
}

// SetCopilot says whether Copilot's coding agent can be assigned on a repository.
func (s *Server) SetCopilot(repoID int64, on bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.bridge.init()
	s.bridge.copilot[repoID] = on
}

// SetAccess sets a person's permission on a repository (admin, write, read or none).
func (s *Server) SetAccess(repoID int64, login, access string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if repo := s.repos[repoID]; repo != nil {
		if repo.Access == nil {
			repo.Access = map[string]string{}
		}
		repo.Access[login] = access
	}
}

// Fail makes the next call of "METHOD path" (path without its query) answer status.
func (s *Server) Fail(route string, status int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.bridge.init()
	s.bridge.fail[route] = status
}

// CreatedIssues lists the issues created on a repository, oldest first (copies).
func (s *Server) CreatedIssues(repoID int64) []CreatedIssue {
	s.mu.Lock()
	defer s.mu.Unlock()
	var issues []CreatedIssue
	for _, issue := range s.bridge.created[repoID] {
		copied := *issue
		copied.Comments = append([]IssueComment(nil), issue.Comments...)
		copied.Assignees = append([]string(nil), issue.Assignees...)
		issues = append(issues, copied)
	}
	return issues
}

// AddCreatedIssue adds an issue as if someone created it on GitHub (a send that died before it was
// recorded): it returns its number.
func (s *Server) AddCreatedIssue(repoID int64, author, title, body string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.createIssue(s.repos[repoID], author, title, body).Number
}

func (s *Server) userID(login string) int64 {
	if id, ok := s.bridge.userIDs[login]; ok {
		return id
	}
	id := int64(1000 + len(s.bridge.userIDs))
	s.bridge.userIDs[login] = id
	return id
}

// userAccess is the person's permission on a repository through an installation of the App reaching it
// (admin, write or read), "" when they cannot see it.
func (s *Server) userAccess(login string, repo *Repo) string {
	if repo == nil {
		return ""
	}
	for _, installation := range s.installations {
		if installation.Suspended || !s.reaches(installation, repo) {
			continue
		}
		if access, named := repo.Access[login]; named {
			if access != "none" {
				return access
			}
			continue
		}
		for _, user := range installation.Users {
			if strings.EqualFold(user, login) {
				return "admin"
			}
		}
	}
	return ""
}

func (s *Server) createIssue(repo *Repo, author, title, body string) *CreatedIssue {
	s.bridge.init()
	if s.issues[repo.ID] == nil {
		s.issues[repo.ID] = map[int]*Issue{}
	}
	number := len(s.issues[repo.ID]) + len(s.pulls[repo.ID]) + 1
	s.issues[repo.ID][number] = &Issue{Number: number, State: "open"}
	issue := &CreatedIssue{RepoID: repo.ID, Number: number, Title: title, Body: body, Author: author, State: "open", CreatedAt: time.Now().UTC()}
	s.bridge.created[repo.ID] = append(s.bridge.created[repo.ID], issue)
	return issue
}

func (s *Server) createdIssue(repoID int64, number int) *CreatedIssue {
	for _, issue := range s.bridge.created[repoID] {
		if issue.Number == number {
			return issue
		}
	}
	return nil
}

func (s *Server) issueJSON(repo *Repo, issue *CreatedIssue) map[string]any {
	return map[string]any{"number": issue.Number, "node_id": "I_" + strconv.Itoa(issue.Number), "title": issue.Title, "body": issue.Body,
		"state": issue.State, "html_url": "https://github.com/" + repo.FullName + "/issues/" + strconv.Itoa(issue.Number),
		"user": map[string]any{"id": s.userID(issue.Author), "login": issue.Author}}
}

// serveBridge answers the calls of the issue connectors; false leaves the call to the rest of the stand-in.
func (s *Server) serveBridge(w http.ResponseWriter, r *http.Request) bool {
	path := r.URL.Path
	userCall := strings.HasPrefix(r.Header.Get("Authorization"), "Bearer ghu_")
	switch {
	case r.Method == http.MethodGet && strings.HasPrefix(path, "/repositories/") && userCall:
	case r.Method == http.MethodPost && path == "/graphql" && userCall:
	case strings.HasPrefix(path, "/repos/") && (r.Method == http.MethodPost || r.Method == http.MethodPatch):
	case r.Method == http.MethodGet && strings.HasPrefix(path, "/repos/") && strings.HasSuffix(path, "/issues"):
	default:
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.bridge.init()
	if status, ok := s.bridge.fail[r.Method+" "+path]; ok {
		delete(s.bridge.fail, r.Method+" "+path)
		writeJSON(w, status, map[string]string{"message": "Failed by the test"})
		return true
	}
	login, installation := "", (*Installation)(nil)
	if userCall {
		if login = s.userOf(r); login == "" {
			writeJSON(w, http.StatusUnauthorized, map[string]string{"message": "Bad credentials"})
			return true
		}
	} else if installation = s.installationOf(r); installation == nil {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"message": "Bad credentials"})
		return true
	}
	notFound := func() bool { writeJSON(w, http.StatusNotFound, map[string]string{"message": "Not Found"}); return true }
	if strings.HasPrefix(path, "/repositories/") {
		id, _ := strconv.ParseInt(strings.TrimPrefix(path, "/repositories/"), 10, 64)
		repo := s.repos[id]
		access := s.userAccess(login, repo)
		if access == "" {
			return notFound()
		}
		item := repoJSON(repo)
		item["permissions"] = map[string]bool{"admin": access == "admin", "push": access == "admin" || access == "write", "pull": true}
		writeJSON(w, http.StatusOK, item)
		return true
	}
	if path == "/graphql" {
		var call struct {
			Query     string `json:"query"`
			Variables struct {
				Owner string `json:"owner"`
				Name  string `json:"name"`
			} `json:"variables"`
		}
		body, _ := io.ReadAll(r.Body)
		if json.Unmarshal(body, &call) != nil || !strings.Contains(call.Query, "suggestedActors") {
			writeJSON(w, http.StatusOK, map[string]any{"errors": []map[string]any{{"message": "the stand-in does not know this query"}}})
			return true
		}
		var repo *Repo
		for _, candidate := range s.repos {
			if strings.EqualFold(candidate.FullName, call.Variables.Owner+"/"+call.Variables.Name) {
				repo = candidate
			}
		}
		if s.userAccess(login, repo) == "" {
			writeJSON(w, http.StatusOK, map[string]any{"data": map[string]any{"repository": nil}, "errors": []map[string]any{{"type": "NOT_FOUND", "message": "Could not resolve to a Repository"}}})
			return true
		}
		nodes := []map[string]any{{"login": login}}
		if s.bridge.copilot[repo.ID] {
			nodes = append(nodes, map[string]any{"login": "copilot-swe-agent"})
		}
		writeJSON(w, http.StatusOK, map[string]any{"data": map[string]any{"repository": map[string]any{"suggestedActors": map[string]any{"nodes": nodes}}}})
		return true
	}
	// /repos/{owner}/{name}/issues[/{number}[/comments|/assignees]]
	parts := strings.Split(strings.TrimPrefix(path, "/repos/"), "/")
	if len(parts) < 3 || parts[2] != "issues" {
		return notFound()
	}
	fullName, _ := url.PathUnescape(parts[0] + "/" + parts[1])
	var repo *Repo
	if installation != nil {
		repo = s.repoByName(installation, fullName)
	} else {
		for _, candidate := range s.repos {
			if strings.EqualFold(candidate.FullName, fullName) && s.userAccess(login, candidate) != "" {
				repo = candidate
			}
		}
	}
	if repo == nil {
		return notFound()
	}
	author := login
	if installation != nil {
		author = "app"
	}
	var input struct {
		Title       string   `json:"title"`
		Body        string   `json:"body"`
		State       string   `json:"state"`
		StateReason string   `json:"state_reason"`
		Assignees   []string `json:"assignees"`
	}
	if r.Method != http.MethodGet {
		body, _ := io.ReadAll(r.Body)
		_ = json.NewDecoder(bytes.NewReader(body)).Decode(&input)
	}
	switch {
	case len(parts) == 3 && r.Method == http.MethodGet:
		since, _ := time.Parse(time.RFC3339, r.URL.Query().Get("since"))
		items := []map[string]any{}
		for _, issue := range s.bridge.created[repo.ID] {
			if !issue.CreatedAt.Before(since.Add(-time.Second)) {
				items = append(items, s.issueJSON(repo, issue))
			}
		}
		writeJSON(w, http.StatusOK, items)
	case len(parts) == 3 && r.Method == http.MethodPost:
		if strings.TrimSpace(input.Title) == "" {
			writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"message": "Validation Failed"})
			return true
		}
		writeJSON(w, http.StatusCreated, s.issueJSON(repo, s.createIssue(repo, author, input.Title, input.Body)))
	case len(parts) >= 4:
		number, _ := strconv.Atoi(parts[3])
		issue := s.createdIssue(repo.ID, number)
		if issue == nil {
			return notFound()
		}
		switch {
		case len(parts) == 4 && r.Method == http.MethodPatch:
			if input.State != "" {
				issue.State = input.State
				s.issues[repo.ID][number].State, s.issues[repo.ID][number].StateReason = input.State, input.StateReason
			}
			writeJSON(w, http.StatusOK, s.issueJSON(repo, issue))
		case len(parts) == 5 && parts[4] == "comments" && r.Method == http.MethodPost:
			issue.Comments = append(issue.Comments, IssueComment{Body: input.Body, Author: author})
			writeJSON(w, http.StatusCreated, map[string]any{"id": len(issue.Comments), "body": input.Body})
		case len(parts) == 5 && parts[4] == "assignees" && r.Method == http.MethodPost:
			access := s.userAccess(login, repo)
			for _, assignee := range input.Assignees {
				if assignee == "copilot-swe-agent[bot]" && s.bridge.copilot[repo.ID] && (access == "admin" || access == "write") {
					issue.Assignees = append(issue.Assignees, assignee)
				}
			}
			assignees := []map[string]any{}
			for _, assignee := range issue.Assignees {
				assignees = append(assignees, map[string]any{"login": assignee})
			}
			answer := s.issueJSON(repo, issue)
			answer["assignees"] = assignees
			writeJSON(w, http.StatusCreated, answer)
		default:
			return notFound()
		}
	default:
		return notFound()
	}
	return true
}
