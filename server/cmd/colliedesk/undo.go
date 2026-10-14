package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"strings"

	"github.com/colliedesk/colliedesk/server/internal/database"
	"github.com/colliedesk/colliedesk/server/internal/freshdesk"
	"github.com/jackc/pgx/v5/pgxpool"
)

const undoUsage = "usage: colliedesk import undo --workspace <slug> [--source freshdesk:account:<id>] [--dry-run]"

// undoCommand removes what a Freshdesk import created in a workspace.
func undoCommand(ctx context.Context, pool *pgxpool.Pool, args []string, output io.Writer) error {
	flags := flag.NewFlagSet("import undo", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	workspace := flags.String("workspace", "", "workspace slug")
	source := flags.String("source", "", "import to undo when the workspace holds several")
	dryRun := flags.Bool("dry-run", false, "show what would be removed without removing it")
	if err := flags.Parse(args); err != nil || flags.NArg() != 0 || *workspace == "" {
		return errors.New(undoUsage)
	}
	if err := database.Migrate(ctx, pool); err != nil {
		return err
	}
	storage, err := attachmentStorage()
	if err != nil {
		return err
	}
	result, err := freshdesk.Undo(ctx, pool, storage, freshdesk.UndoOptions{WorkspaceSlug: *workspace, Source: *source, DryRun: *dryRun})
	if err != nil {
		return err
	}
	verb := "Removed"
	if *dryRun {
		verb = "Would remove"
	}
	kept := "none"
	if len(result.KeptConversationTickets) > 0 {
		kept = strings.Join(result.KeptConversationTickets, ", ")
	}
	_, err = fmt.Fprintf(output, "%s from %s: %d conversations, %d contacts, %d tags, %d stored files.\nKept because they have messages written after the import (Freshdesk ticket ids): %s\n",
		verb, result.Source, result.Conversations, result.Contacts, result.Tags, result.Files, kept)
	return err
}
