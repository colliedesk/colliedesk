// Command ai-eval measures AI drafts on a real knowledge base, through the same path a customer's
// email takes: a workspace in a scratch schema, its articles built into retrieval, one conversation
// per case, the production worker and provider, then a stronger model grading every draft against
// the article that answers it (docs/internals/ai-drafts.md, "Measuring").
//
//	AI_SERVER_API_KEY=... TEST_DATABASE_URL=postgres://... go run ./cmd/ai-eval -dataset dataset.json -out results/
//
// It spends real tokens: about 0.03 USD per case with the default models.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/aidraft"
	"github.com/colliedesk/colliedesk/server/internal/aiprovider"
	"github.com/colliedesk/colliedesk/server/internal/auth"
	"github.com/colliedesk/colliedesk/server/internal/database"
	"github.com/colliedesk/colliedesk/server/internal/domain"
	"github.com/colliedesk/colliedesk/server/internal/store"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type article struct {
	ID    string `json:"id"`
	Title string `json:"title"`
	Body  string `json:"body"`
	Path  string `json:"path"`
}

type evalCase struct {
	ID           string   `json:"id"`
	Subject      string   `json:"subject"`
	Message      string   `json:"message"`
	Kind         string   `json:"kind"` // answerable, held_out, not_answerable
	ArticleIDs   []string `json:"article_ids"`
	KBHasArticle bool     `json:"kb_has_article"`
	Detail       string   `json:"detail"`
}

type dataset struct {
	Workspace string     `json:"workspace"`
	Articles  []article  `json:"articles"`
	HeldOut   []string   `json:"held_out"`
	Cases     []evalCase `json:"cases"`
}

func main() {
	datasetPath := flag.String("dataset", "", "dataset JSON (articles, held_out, cases)")
	out := flag.String("out", "ai-eval-results", "directory for drafts.jsonl, grades.jsonl and report.txt")
	model := flag.String("model", "claude-sonnet-5-5", "drafting model")
	effort := flag.String("effort", "low", "drafting effort")
	judgeModel := flag.String("judge-model", "claude-opus-5-5", "grading model")
	selectModel := flag.String("select-model", "claude-haiku-5-5", "model that chooses articles from their titles first; empty: keyword search only")
	limit := flag.Int("limit", 0, "run only the first N cases (0: all)")
	only := flag.String("only", "", "run only cases whose id starts with this")
	workers := flag.Int("workers", 6, "drafts written at once")
	regrade := flag.Bool("regrade", false, "grade the drafts of an earlier run in -out again, without drafting")
	budget := flag.Duration("budget", 90*time.Second, "start no new draft or grade after this long; run again to go on where it stopped")
	flag.Parse()
	deadline := time.Now().Add(*budget)
	if *datasetPath == "" {
		log.Fatal("ai-eval: -dataset is required")
	}
	var data dataset
	raw, err := os.ReadFile(*datasetPath)
	if err != nil {
		log.Fatal(err)
	}
	if err := json.Unmarshal(raw, &data); err != nil {
		log.Fatal(err)
	}
	cases := data.Cases
	if *only != "" {
		var kept []evalCase
		for _, c := range cases {
			if strings.HasPrefix(c.ID, *only) {
				kept = append(kept, c)
			}
		}
		cases = kept
	}
	if *limit > 0 && *limit < len(cases) {
		cases = cases[:*limit]
	}
	provider, err := aiprovider.NewAnthropic(aiprovider.AnthropicConfig{APIKey: os.Getenv("AI_SERVER_API_KEY"), HTTPClient: aiprovider.NewHTTPClient(aiprovider.HTTPOptions{})})
	if err != nil {
		log.Fatal(err)
	}
	if err := os.MkdirAll(*out, 0o755); err != nil {
		log.Fatal(err)
	}
	ctx := context.Background()
	draftsPath, gradesPath := filepath.Join(*out, "drafts.jsonl"), filepath.Join(*out, "grades.jsonl")
	if *regrade {
		_ = os.Remove(gradesPath)
	}
	// A run goes on where the last one stopped: cases with a written draft are not drafted again.
	drafts, err := readJSONL[draftRow](draftsPath)
	if err != nil {
		log.Fatal(err)
	}
	done := map[string]bool{}
	for _, row := range drafts {
		done[row.Case.ID] = true
	}
	var todo []evalCase
	for _, c := range cases {
		if !done[c.ID] {
			todo = append(todo, c)
		}
	}
	if len(todo) > 0 && !*regrade {
		written, err := runDrafts(ctx, data, todo, provider, aidraft.Settings{Model: *model, Effort: aiprovider.Effort(*effort), SelectModel: *selectModel}, *workers, deadline)
		if err != nil {
			log.Fatal(err)
		}
		drafts = append(drafts, written...)
		if err := writeJSONL(draftsPath, drafts); err != nil {
			log.Fatal(err)
		}
		if len(drafts) < len(cases) {
			log.Printf("drafted %d of %d cases; run again to go on", len(drafts), len(cases))
			return
		}
	}
	grades, err := readJSONL[grade](gradesPath)
	if err != nil {
		log.Fatal(err)
	}
	grades = gradeAll(ctx, provider, *judgeModel, data, drafts, grades, *workers, deadline)
	if err := writeJSONL(gradesPath, grades); err != nil {
		log.Fatal(err)
	}
	if len(grades) < len(drafts) {
		log.Printf("graded %d of %d drafts; run again to go on", len(grades), len(drafts))
		return
	}
	report := summarize(grades, *model+" (articles chosen by "+orNone(*selectModel)+")", *judgeModel)
	fmt.Print(report)
	if err := os.WriteFile(filepath.Join(*out, "report.txt"), []byte(report), 0o644); err != nil {
		log.Fatal(err)
	}
}

// draftRow is one case after the worker wrote its draft.
type draftRow struct {
	Case    evalCase        `json:"case"`
	Draft   *domain.AIDraft `json:"draft"`
	Seconds float64         `json:"seconds"`
	Error   string          `json:"error,omitempty"`
	// Shown is what retrieval gave the model, by title, for the grader and for reading failures.
	Shown []string `json:"shown"`
	Usage struct {
		Input, Output int64
	} `json:"usage"`
}

// runDrafts seeds a scratch workspace with the knowledge base minus the held-out articles, opens one
// conversation per case with the customer's email, and lets the production worker write the drafts.
func runDrafts(ctx context.Context, data dataset, cases []evalCase, provider aiprovider.Provider, settings aidraft.Settings, workers int, deadline time.Time) ([]draftRow, error) {
	pool, cleanup, err := scratchPool(ctx)
	if err != nil {
		return nil, err
	}
	defer cleanup()
	if err := database.Migrate(ctx, pool); err != nil {
		return nil, fmt.Errorf("migrate: %w", err)
	}
	db := database.New(pool)
	data.Workspace = strings.TrimSpace(data.Workspace)
	workspace, _, err := auth.NewWithDB(db, "https://eval.example.com").CreateWorkspace(ctx, data.Workspace+" eval", "admin@eval.example.com")
	if err != nil {
		return nil, fmt.Errorf("create workspace: %w", err)
	}
	var adminID string
	if err := db.InWorkspace(ctx, workspace.ID, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `SELECT id::text FROM agents WHERE workspace_id=$1`, workspace.ID).Scan(&adminID)
	}); err != nil {
		return nil, err
	}
	data.Workspace = workspace.ID
	base := store.NewWithDB(db).WithAI(true)
	if err := seedKB(ctx, base, workspace.ID, adminID, data); err != nil {
		return nil, err
	}
	if err := db.InWorkspace(ctx, workspace.ID, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `INSERT INTO tags(workspace_id,name) SELECT $1, unnest($2::text[])`, workspace.ID, evalTags)
		return err
	}); err != nil {
		return nil, err
	}
	if _, err := base.BuildAIRetrieval(ctx); err != nil {
		return nil, fmt.Errorf("build retrieval: %w", err)
	}
	conversations := map[string]evalCase{}
	for _, c := range cases {
		id, err := openConversation(ctx, db, workspace.ID, c)
		if err != nil {
			return nil, fmt.Errorf("case %s: %w", c.ID, err)
		}
		if _, err := base.RequestAIDraft(ctx, workspace.ID, id, ""); err != nil {
			return nil, fmt.Errorf("queue %s: %w", c.ID, err)
		}
		conversations[id] = c
	}
	recorder := &recordingProvider{Provider: provider, shown: map[string][]string{}}
	// One draft per RunOnce, so a worker checks the deadline between drafts.
	worker := &aidraft.Worker{Store: base, Provider: recorder, Settings: settings, LogError: func(err error) { log.Print(err) }, MaxPerRun: 1}
	started := time.Now()
	var wg sync.WaitGroup
	for range max(1, workers) {
		wg.Add(1)
		go func() {
			defer wg.Done()
			// Each worker claims a different draft (SKIP LOCKED), as several servers would.
			for time.Now().Before(deadline) {
				written, err := worker.RunOnce(ctx)
				if err != nil {
					log.Print(err)
				}
				if written == 0 {
					return
				}
			}
		}()
	}
	wg.Wait()
	log.Printf("drafted %d cases in %s", len(cases), time.Since(started).Round(time.Second))
	var rows []draftRow
	for id, c := range conversations {
		draft, err := base.LatestAIDraft(ctx, workspace.ID, id)
		if err == nil && (draft == nil || draft.Status == "queued") {
			// Not written before the deadline: the next run drafts it.
			continue
		}
		row := draftRow{Case: c, Draft: draft, Shown: recorder.shownFor(c.Message)}
		if err != nil {
			row.Error = err.Error()
		}
		if err := db.InWorkspace(ctx, workspace.ID, func(tx pgx.Tx) error {
			return tx.QueryRow(ctx, `SELECT COALESCE(sum(u.input_tokens),0),COALESCE(sum(u.output_tokens),0) FROM ai_usage u JOIN ai_drafts d
				ON d.workspace_id=u.workspace_id AND d.id=u.ai_draft_id WHERE u.workspace_id=$1 AND d.conversation_id=$2`, workspace.ID, id).Scan(&row.Usage.Input, &row.Usage.Output)
		}); err != nil {
			return nil, err
		}
		if draft != nil && draft.ReadyAt != nil {
			row.Seconds = draft.ReadyAt.Sub(draft.CreatedAt).Seconds()
		}
		rows = append(rows, row)
	}
	return rows, nil
}

// recordingProvider keeps the titles of the sources each prompt showed, keyed by the customer text.
type recordingProvider struct {
	aiprovider.Provider
	mu    sync.Mutex
	shown map[string][]string
}

var sourceTitle = regexp.MustCompile(`<source id="S\d+" kind="(\w+)" title="([^"]*)">`)

func (r *recordingProvider) Complete(ctx context.Context, request aiprovider.Request) (aiprovider.Response, error) {
	text := request.Messages[len(request.Messages)-1].Text
	var titles []string
	for _, match := range sourceTitle.FindAllStringSubmatch(text, -1) {
		titles = append(titles, match[1]+": "+match[2])
	}
	r.mu.Lock()
	r.shown[text] = titles
	r.mu.Unlock()
	return r.Provider.Complete(ctx, request)
}

func (r *recordingProvider) shownFor(message string) []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	escaped := strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", `"`, "&quot;").Replace(strings.TrimSpace(message))
	for prompt, titles := range r.shown {
		if strings.Contains(prompt, escaped) {
			return titles
		}
	}
	return nil
}

func seedKB(ctx context.Context, data *store.Store, workspaceID, adminID string, set dataset) error {
	heldOut := map[string]bool{}
	for _, id := range set.HeldOut {
		heldOut[id] = true
	}
	collections := map[string]string{}
	actor := store.Actor{AgentID: adminID}
	for _, a := range set.Articles {
		if heldOut[a.ID] {
			continue
		}
		folder := a.Path
		if folder == "" {
			folder = "Help"
		}
		if len(folder) > 100 {
			folder = folder[:100]
		}
		if _, ok := collections[folder]; !ok {
			collection, err := data.CreateKBCollection(ctx, workspaceID, actor, store.KBCollectionInput{Name: folder})
			if err != nil {
				return fmt.Errorf("collection %q: %w", folder, err)
			}
			collections[folder] = collection.ID
		}
		if _, err := data.CreateKBArticle(ctx, workspaceID, actor, store.KBArticleInput{CollectionID: collections[folder], Title: a.Title, BodyMD: a.Body, Status: "published"}); err != nil {
			return fmt.Errorf("article %s: %w", a.ID, err)
		}
	}
	return nil
}

// openConversation writes a contact, an open conversation and the customer's email, as inbound mail does.
func openConversation(ctx context.Context, db *database.DB, workspaceID string, c evalCase) (string, error) {
	var conversationID string
	err := db.InWorkspace(ctx, workspaceID, func(tx pgx.Tx) error {
		var contactID string
		if err := tx.QueryRow(ctx, `INSERT INTO contacts(workspace_id,name,email) VALUES($1,$2,$3) RETURNING id::text`,
			workspaceID, "Customer "+c.ID, strings.ToLower(c.ID)+"@customer.example").Scan(&contactID); err != nil {
			return err
		}
		subject := c.Subject
		if subject == "" {
			subject = "Question"
		}
		if err := tx.QueryRow(ctx, `INSERT INTO conversations(workspace_id,contact_id,subject) VALUES($1,$2,$3) RETURNING id::text`,
			workspaceID, contactID, subject).Scan(&conversationID); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `INSERT INTO messages(workspace_id,conversation_id,kind,body) VALUES($1,$2,'incoming',$3)`, workspaceID, conversationID, c.Message)
		return err
	})
	return conversationID, err
}

// scratchPool opens TEST_DATABASE_URL on a schema of its own, dropped when done.
func scratchPool(ctx context.Context) (*pgxpool.Pool, func(), error) {
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		return nil, nil, errors.New("ai-eval: set TEST_DATABASE_URL to a PostgreSQL the tool may create a schema in")
	}
	schema := fmt.Sprintf("ai_eval_%d", time.Now().UnixNano())
	// One plain connection makes and drops the schema; every query of the run goes through database.DB.
	admin, err := pgx.Connect(ctx, url)
	if err != nil {
		return nil, nil, err
	}
	if _, err := admin.Exec(ctx, "CREATE SCHEMA "+schema); err != nil {
		admin.Close(ctx)
		return nil, nil, err
	}
	config, err := pgxpool.ParseConfig(url)
	if err != nil {
		admin.Close(ctx)
		return nil, nil, err
	}
	config.ConnConfig.RuntimeParams["search_path"] = schema
	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		admin.Close(ctx)
		return nil, nil, err
	}
	return pool, func() {
		pool.Close()
		_, _ = admin.Exec(context.Background(), "DROP SCHEMA "+schema+" CASCADE")
		admin.Close(context.Background())
	}, nil
}

func orNone(model string) string {
	if model == "" {
		return "keyword search only"
	}
	return model
}

// evalTags are the tags of the eval workspace, so triage has something to choose from.
var evalTags = []string{"domains", "dns", "email", "transfers", "billing", "account-security", "website-builders", "webmail", "renewals"}

func writeJSONL[T any](path string, rows []T) error {
	var b strings.Builder
	for _, row := range rows {
		line, err := json.Marshal(row)
		if err != nil {
			return err
		}
		b.Write(line)
		b.WriteByte('\n')
	}
	return os.WriteFile(path, []byte(b.String()), 0o644)
}

func readJSONL[T any](path string) ([]T, error) {
	raw, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var rows []T
	for _, line := range strings.Split(strings.TrimSpace(string(raw)), "\n") {
		if line == "" {
			continue
		}
		var row T
		if err := json.Unmarshal([]byte(line), &row); err != nil {
			return nil, err
		}
		rows = append(rows, row)
	}
	return rows, nil
}
