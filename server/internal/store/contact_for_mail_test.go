package store_test

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/colliedesk/colliedesk/server/internal/database"
	"github.com/colliedesk/colliedesk/server/internal/database/databasetest"
	"github.com/colliedesk/colliedesk/server/internal/domain"
	"github.com/colliedesk/colliedesk/server/internal/store"
)

// reportedBy answers the contact of each conversation and how many contacts have email, in workspaceID.
func reportedBy(t *testing.T, pool *pgxpool.Pool, workspaceID, email string, conversations []string) ([]string, int) {
	t.Helper()
	contacts := make([]string, len(conversations))
	var count int
	err := database.New(pool).InWorkspace(context.Background(), workspaceID, func(tx pgx.Tx) error {
		for i, id := range conversations {
			if err := tx.QueryRow(context.Background(), `SELECT contact_id::text FROM conversations WHERE workspace_id=$1 AND id=$2`, workspaceID, id).Scan(&contacts[i]); err != nil {
				return err
			}
		}
		return tx.QueryRow(context.Background(), `SELECT count(*) FROM contacts WHERE workspace_id=$1 AND email=$2`, workspaceID, email).Scan(&count)
	})
	if err != nil {
		t.Fatal(err)
	}
	return contacts, count
}

// Reports that arrive at once from an address no contact has yet make one contact, and every report
// is that contact's.
func TestReportsArrivingTogetherFromANewAddressMakeOneContact(t *testing.T) {
	pool := databasetest.Pool(t)
	ctx := context.Background()
	s := store.New(pool)
	workspace := testWorkspace(t, pool, "reports-new")
	const reports = 8
	conversations := make([]string, reports)
	errs := make([]error, reports)
	atOnce(reports, func(i int) {
		conversations[i], errs[i] = s.CreateProblemReport(ctx, workspace, store.ProblemReport{
			Subject: fmt.Sprintf("Export stuck %d", i), Body: "The export never ends.", ReporterEmail: "ana@example.com", ReporterName: "Ana",
		})
	})
	for i, err := range errs {
		if err != nil {
			t.Fatalf("report %d: %v", i, err)
		}
	}
	contacts, count := reportedBy(t, pool, workspace, "ana@example.com", conversations)
	if count != 1 {
		t.Fatalf("%d contacts have the address, want 1", count)
	}
	for i, contact := range contacts {
		if contact != contacts[0] {
			t.Fatalf("report %d is contact %s's, report 0 is %s's: want one contact", i, contact, contacts[0])
		}
	}
}

// A report from the address of a contact merged into another is the kept contact's.
func TestAReportFromAMergedAddressJoinsTheKeptContact(t *testing.T) {
	pool := databasetest.Pool(t)
	ctx := context.Background()
	s := store.New(pool)
	workspace := testWorkspace(t, pool, "reports-merged")
	kept, err := s.CreateContact(ctx, workspace, "Ana", "ana@example.com")
	if err != nil {
		t.Fatal(err)
	}
	duplicate, err := s.CreateContact(ctx, workspace, "Ana (work)", "ana@work.example.com")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.MergeContacts(ctx, workspace, kept.ID, duplicate.ID, store.Actor{}); err != nil {
		t.Fatal(err)
	}
	conversation, err := s.CreateProblemReport(ctx, workspace, store.ProblemReport{
		Subject: "Export stuck", Body: "The export never ends.", ReporterEmail: "ana@work.example.com", ReporterName: "Ana",
	})
	if err != nil {
		t.Fatal(err)
	}
	contacts, _ := reportedBy(t, pool, workspace, "ana@work.example.com", []string{conversation})
	if contacts[0] != kept.ID {
		t.Fatalf("report is contact %s's, want the kept contact %s", contacts[0], kept.ID)
	}
}

// A report from an address whose contact is being merged waits for the merge and joins the kept
// contact, instead of reading the duplicate as it was before the merge.
func TestAReportArrivingDuringAMergeJoinsTheKeptContact(t *testing.T) {
	pool := databasetest.Pool(t)
	ctx := context.Background()
	s := store.New(pool)
	workspace := testWorkspace(t, pool, "reports-during-merge")
	kept, err := s.CreateContact(ctx, workspace, "Ana", "ana@example.com")
	if err != nil {
		t.Fatal(err)
	}
	duplicate, err := s.CreateContact(ctx, workspace, "Ana (work)", "ana@work.example.com")
	if err != nil {
		t.Fatal(err)
	}
	type report struct {
		conversation string
		err          error
	}
	done := make(chan report, 1)
	// The merge locks the duplicate's row; the report arrives while it holds the lock; once the
	// report waits on that row, the merge points the duplicate at the kept contact and commits.
	err = database.New(pool).InWorkspace(ctx, workspace, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `SELECT 1 FROM contacts WHERE workspace_id=$1 AND id=$2 FOR UPDATE`, workspace, duplicate.ID); err != nil {
			return err
		}
		go func() {
			conversation, err := s.CreateProblemReport(ctx, workspace, store.ProblemReport{
				Subject: "Export stuck", Body: "The export never ends.", ReporterEmail: "ana@work.example.com", ReporterName: "Ana",
			})
			done <- report{conversation, err}
		}()
		for deadline := time.Now().Add(10 * time.Second); ; time.Sleep(10 * time.Millisecond) {
			var waiting int
			// A row lock wait shows as a transactionid lock, which names no database: find it by backend.
			if err := pool.QueryRow(ctx, `SELECT count(*) FROM pg_locks l JOIN pg_stat_activity a ON a.pid=l.pid
				WHERE NOT l.granted AND a.datname=current_database()`).Scan(&waiting); err != nil {
				return err
			}
			if waiting > 0 {
				break
			}
			select {
			case r := <-done:
				return fmt.Errorf("the report finished without waiting on the contact the merge holds: %v", r.err)
			default:
			}
			if time.Now().After(deadline) {
				return fmt.Errorf("the report did not wait on the contact the merge holds")
			}
		}
		_, err := tx.Exec(ctx, `UPDATE contacts SET merged_into_id=$3 WHERE workspace_id=$1 AND id=$2`, workspace, duplicate.ID, kept.ID)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	var r report
	select {
	case r = <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("the report did not finish after the merge committed")
	}
	if r.err != nil {
		t.Fatal(r.err)
	}
	contacts, _ := reportedBy(t, pool, workspace, "ana@work.example.com", []string{r.conversation})
	if contacts[0] != kept.ID {
		t.Fatalf("report is contact %s's (the duplicate is %s), want the kept contact %s", contacts[0], duplicate.ID, kept.ID)
	}
}

// Adding a contact whose address arrives twice at once adds it once; the other add is told which
// contact to use, and an address merged away points to the kept contact.
func TestAddingAContactTwiceAtOnceAddsItOnce(t *testing.T) {
	pool := databasetest.Pool(t)
	ctx := context.Background()
	s := store.New(pool)
	workspace := testWorkspace(t, pool, "contacts-twice")
	const adds = 8
	added := make([]domain.Contact, adds)
	errs := make([]error, adds)
	atOnce(adds, func(i int) {
		added[i], errs[i] = s.CreateContact(ctx, workspace, "Ana", "Ana@Example.com")
	})
	var made string
	for i, err := range errs {
		if err == nil {
			if made != "" {
				t.Fatalf("contact added twice: %s and %s", made, added[i].ID)
			}
			made = added[i].ID
		}
	}
	if made == "" {
		t.Fatalf("no add succeeded: %v", errs)
	}
	for i, err := range errs {
		var exists *domain.ContactExistsError
		if err != nil && (!errors.As(err, &exists) || exists.Use.ID != made) {
			t.Fatalf("add %d = %v, want to be told to use contact %s", i, err, made)
		}
	}
	other, err := s.CreateContact(ctx, workspace, "Ana (work)", "ana@work.example.com")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.MergeContacts(ctx, workspace, made, other.ID, store.Actor{}); err != nil {
		t.Fatal(err)
	}
	_, err = s.CreateContact(ctx, workspace, "Ana", "ana@work.example.com")
	var exists *domain.ContactExistsError
	if !errors.As(err, &exists) || exists.Use.ID != made {
		t.Fatalf("adding a merged address = %v, want to be told to use the kept contact %s", err, made)
	}
}
