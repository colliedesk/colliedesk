package importjobs

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/database"
	"github.com/colliedesk/colliedesk/server/internal/domain"
	"github.com/colliedesk/colliedesk/server/internal/freshdesk"
	"github.com/jackc/pgx/v5"
)

// Person is an agent of the imported Freshdesk account and the Colliedesk agent it is linked to: that
// agent becomes the author of their imported replies and the assignee of the imported tickets they
// held. Pending is true until the worker has applied a new or removed link.
type Person struct {
	ExternalID    string    `json:"external_id"`
	Name          string    `json:"name"`
	Email         string    `json:"email"`
	Active        bool      `json:"active"`
	LinkedAgentID *string   `json:"linked_agent_id"`
	LinkedByEmail bool      `json:"linked_by_email"`
	Pending       bool      `json:"pending"`
	UpdatedAt     time.Time `json:"updated_at"`
}

var (
	// ErrNoPeople means the job has no Freshdesk account yet (it was not previewed).
	ErrNoPeople = errors.New("this import has not read its Freshdesk account yet")
	// ErrAgent means the Colliedesk agent is not one of the workspace's active agents.
	ErrAgent = errors.New("link to an active agent of this workspace")
)

// savePeople records the agents a preview read, and links each to the workspace agent with the same
// email. A link is applied again to what an earlier import of the account brought in.
func savePeople(ctx context.Context, tx pgx.Tx, workspaceID string, preview *freshdesk.PreviewResult) error {
	for _, agent := range preview.Agents {
		_, err := tx.Exec(ctx, `INSERT INTO import_people(workspace_id,source,external_id,name,email,active,linked_agent_id,linked_by_email,relink_pending)
			SELECT $1,$2,$3,$4,$5,$6,a.id,a.id IS NOT NULL,a.id IS NOT NULL
			FROM (SELECT 1) one LEFT JOIN agents a ON a.workspace_id=$1 AND lower(a.email)=lower($5) AND a.deactivated_at IS NULL
			ON CONFLICT (workspace_id,source,external_id) DO UPDATE SET name=EXCLUDED.name, email=EXCLUDED.email, active=EXCLUDED.active,
				linked_agent_id=CASE WHEN import_people.chosen_by_person THEN import_people.linked_agent_id ELSE COALESCE(import_people.linked_agent_id, EXCLUDED.linked_agent_id) END,
				linked_by_email=CASE WHEN import_people.linked_agent_id IS NULL AND NOT import_people.chosen_by_person THEN EXCLUDED.linked_by_email ELSE import_people.linked_by_email END,
				relink_pending=import_people.relink_pending OR (import_people.linked_agent_id IS NULL AND NOT import_people.chosen_by_person AND EXCLUDED.linked_agent_id IS NOT NULL),
				updated_at=now()`,
			workspaceID, preview.Source, strconv.FormatInt(agent.ID, 10), agent.Name, agent.Email, agent.Active)
		if err != nil {
			return fmt.Errorf("save Freshdesk agents: %w", err)
		}
	}
	return nil
}

// relinkAll asks the worker to apply every link of the account again, after an import wrote replies
// and tickets that point at people no Colliedesk agent matched at the time.
func relinkAll(ctx context.Context, tx pgx.Tx, workspaceID, source string) error {
	_, err := tx.Exec(ctx, `UPDATE import_people SET relink_pending=true, updated_at=now()
		WHERE workspace_id=$1 AND source=$2 AND linked_agent_id IS NOT NULL`, workspaceID, source)
	return err
}

// People lists the Freshdesk agents of a job's account.
func (s *Service) People(ctx context.Context, workspaceID, jobID string) ([]Person, error) {
	if err := known(jobID); err != nil {
		return nil, err
	}
	people := []Person{}
	err := s.inWorkspace(ctx, workspaceID, func(tx pgx.Tx) error {
		source, err := jobSource(ctx, tx, workspaceID, jobID)
		if err != nil {
			return err
		}
		rows, err := tx.Query(ctx, `SELECT external_id,name,email,active,linked_agent_id::text,linked_by_email,relink_pending,updated_at
			FROM import_people WHERE workspace_id=$1 AND source=$2 ORDER BY active DESC, lower(name), external_id`, workspaceID, source)
		if err != nil {
			return err
		}
		people, err = pgx.CollectRows(rows, func(row pgx.CollectableRow) (Person, error) {
			var p Person
			err := row.Scan(&p.ExternalID, &p.Name, &p.Email, &p.Active, &p.LinkedAgentID, &p.LinkedByEmail, &p.Pending, &p.UpdatedAt)
			p.UpdatedAt = domain.UTC(p.UpdatedAt)
			return p, err
		})
		return err
	})
	return people, err
}

func jobSource(ctx context.Context, tx pgx.Tx, workspaceID, jobID string) (string, error) {
	var source *string
	err := tx.QueryRow(ctx, `SELECT account_source FROM import_jobs WHERE workspace_id=$1 AND id=$2`, workspaceID, jobID).Scan(&source)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrNotFound
	}
	if err != nil {
		return "", err
	}
	if source == nil {
		return "", ErrNoPeople
	}
	return *source, nil
}

// Link links a Freshdesk agent to a Colliedesk agent, replacing an earlier link; the worker moves
// their replies and tickets within seconds.
func (s *Service) Link(ctx context.Context, workspaceID, jobID, externalID, agentID string) (Person, error) {
	if err := known(agentID); err != nil {
		return Person{}, ErrAgent
	}
	return s.changeLink(ctx, workspaceID, jobID, externalID, &agentID)
}

// Unlink removes the link: the replies show the Freshdesk name again and the tickets it assigned lose
// their assignee, unless someone reassigned them since.
func (s *Service) Unlink(ctx context.Context, workspaceID, jobID, externalID string) (Person, error) {
	return s.changeLink(ctx, workspaceID, jobID, externalID, nil)
}

func (s *Service) changeLink(ctx context.Context, workspaceID, jobID, externalID string, agentID *string) (Person, error) {
	if err := known(jobID); err != nil {
		return Person{}, err
	}
	var person Person
	err := s.inWorkspace(ctx, workspaceID, func(tx pgx.Tx) error {
		source, err := jobSource(ctx, tx, workspaceID, jobID)
		if err != nil {
			return err
		}
		if agentID != nil {
			var active bool
			if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM agents WHERE workspace_id=$1 AND id=$2 AND deactivated_at IS NULL)`, workspaceID, *agentID).Scan(&active); err != nil {
				return err
			}
			if !active {
				return ErrAgent
			}
		}
		// relink_from keeps the agent the replies are taken away from until the worker has done so; a
		// second change before then keeps the first one.
		err = tx.QueryRow(ctx, `UPDATE import_people SET
				relink_from=CASE WHEN relink_pending AND relink_from IS NOT NULL THEN relink_from
					WHEN linked_agent_id IS DISTINCT FROM $4::uuid THEN linked_agent_id ELSE relink_from END,
				linked_agent_id=$4, linked_by_email=false, chosen_by_person=true, relink_pending=true, updated_at=$5
			WHERE workspace_id=$1 AND source=$2 AND external_id=$3
			RETURNING external_id,name,email,active,linked_agent_id::text,linked_by_email,relink_pending,updated_at`,
			workspaceID, source, externalID, agentID, s.now().UTC()).
			Scan(&person.ExternalID, &person.Name, &person.Email, &person.Active, &person.LinkedAgentID, &person.LinkedByEmail, &person.Pending, &person.UpdatedAt)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		person.UpdatedAt = domain.UTC(person.UpdatedAt)
		return err
	})
	return person, err
}

// LinkByEmail links the Freshdesk agents with this email to an agent who just joined the workspace,
// when nobody linked them yet and no person chose otherwise (an unlink stays). It returns how many it linked.
func (s *Service) LinkByEmail(ctx context.Context, workspaceID, agentID, email string) (int, error) {
	var linked int
	err := s.inWorkspace(ctx, workspaceID, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, `UPDATE import_people SET linked_agent_id=$2, linked_by_email=true, relink_pending=true, updated_at=$4
			WHERE workspace_id=$1 AND linked_agent_id IS NULL AND NOT chosen_by_person AND lower(email)=lower($3)`, workspaceID, agentID, email, s.now().UTC())
		linked = int(tag.RowsAffected())
		return err
	})
	return linked, err
}

type pendingLink struct{ workspaceID, source, externalID string }

// claimRelinks returns people whose link changed and is not applied yet, of every workspace.
func claimRelinks(ctx context.Context, db *database.DB, limit int) ([]pendingLink, error) {
	var out []pendingLink
	err := db.Privileged(ctx, database.PrivilegeImportJobClaim, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `SELECT workspace_id::text,source,external_id FROM import_people WHERE relink_pending ORDER BY updated_at LIMIT $1`, limit)
		if err != nil {
			return fmt.Errorf("list pending import links: %w", err)
		}
		out, err = pgx.CollectRows(rows, func(row pgx.CollectableRow) (pendingLink, error) {
			var p pendingLink
			return p, row.Scan(&p.workspaceID, &p.source, &p.externalID)
		})
		return err
	})
	return out, err
}

// applyLink moves one person's imported replies and tickets in their workspace, in one transaction:
// away from relink_from, then to the linked agent where they have no author or assignee. Only rows the
// import of this account wrote (its ledger) change; timestamps stay as Freshdesk had them.
func applyLink(ctx context.Context, db *database.DB, p pendingLink) error {
	return db.InWorkspace(ctx, p.workspaceID, func(tx pgx.Tx) error {
		var linked, from *string
		err := tx.QueryRow(ctx, `SELECT linked_agent_id::text, relink_from::text FROM import_people
			WHERE workspace_id=$1 AND source=$2 AND external_id=$3 AND relink_pending FOR UPDATE SKIP LOCKED`, p.workspaceID, p.source, p.externalID).Scan(&linked, &from)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil // applied by another worker meanwhile, or locked by it now
		}
		if err != nil {
			return err
		}
		messages := `id IN (SELECT target_id FROM import_items WHERE workspace_id=$1 AND source=$2 AND entity IN ('description','conversation') AND target_id IS NOT NULL)
			AND origin->>'system'='freshdesk' AND origin->>'author_external_id'=$3`
		conversations := `id IN (SELECT target_id FROM import_items WHERE workspace_id=$1 AND source=$2 AND entity='ticket' AND target_id IS NOT NULL)
			AND origin->>'system'='freshdesk' AND origin->>'responder_external_id'=$3`
		args := []any{p.workspaceID, p.source, p.externalID}
		if from != nil {
			if _, err := tx.Exec(ctx, `UPDATE messages SET author_agent_id=NULL WHERE workspace_id=$1 AND author_agent_id=$4 AND `+messages, append(args, *from)...); err != nil {
				return fmt.Errorf("unlink imported replies: %w", err)
			}
			if _, err := tx.Exec(ctx, `UPDATE conversations SET assignee_id=NULL WHERE workspace_id=$1 AND assignee_id=$4 AND `+conversations, append(args, *from)...); err != nil {
				return fmt.Errorf("unlink imported tickets: %w", err)
			}
		}
		if linked != nil {
			if _, err := tx.Exec(ctx, `UPDATE messages SET author_agent_id=$4 WHERE workspace_id=$1 AND author_agent_id IS NULL AND `+messages, append(args, *linked)...); err != nil {
				return fmt.Errorf("link imported replies: %w", err)
			}
			if _, err := tx.Exec(ctx, `UPDATE conversations SET assignee_id=$4 WHERE workspace_id=$1 AND assignee_id IS NULL AND `+conversations, append(args, *linked)...); err != nil {
				return fmt.Errorf("link imported tickets: %w", err)
			}
		}
		_, err = tx.Exec(ctx, `UPDATE import_people SET relink_pending=false, relink_from=NULL WHERE workspace_id=$1 AND source=$2 AND external_id=$3`, args...)
		return err
	})
}

// keptPeople is when the agents of an account stay: an import of it is being previewed, or was started
// and the account was not undone since (an undo removes everything the account brought in). $1 is the workspace, $2 the ledger source.
const keptPeople = `EXISTS (SELECT 1 FROM import_jobs j WHERE j.workspace_id=$1 AND j.account_source=$2 AND j.kind='import'
	AND (j.state IN ('previewing','previewed') OR (j.started_at IS NOT NULL AND NOT EXISTS (
		SELECT 1 FROM import_jobs u WHERE u.workspace_id=j.workspace_id AND u.kind='undo' AND u.account_source=j.account_source AND u.state='finished' AND u.finished_at>=j.started_at))))`

// forgetPeople removes the agents of an account nothing imports or previews any more: after a preview
// nobody started, or after the undo of its import. A preview reads them; it writes nothing for long.
func forgetPeople(ctx context.Context, tx pgx.Tx, workspaceID, source string) error {
	_, err := tx.Exec(ctx, `DELETE FROM import_people WHERE workspace_id=$1 AND source=$2 AND NOT `+keptPeople, workspaceID, source)
	return err
}
