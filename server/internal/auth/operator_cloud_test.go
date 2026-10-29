package auth_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/auth"
	"github.com/colliedesk/colliedesk/server/internal/database"
	"github.com/colliedesk/colliedesk/server/internal/database/databasetest"
	"github.com/colliedesk/colliedesk/server/internal/tokenseal"
)

// Review ADR (PR #316) 1: someone who held the operator's mailbox for a while kept an enroll ticket; once the
// operator set up their own authenticator, that ticket must not replace it and sign in. A new ticket ends the
// ones not used yet, and setting up an authenticator ends every other.
func TestAStaleEnrollTicketNeverReplacesTheAuthenticator(t *testing.T) {
	keys, err := tokenseal.ParseKeys("k1:b3BlcmF0b3ItdGVzdC1rZXktMzItYnl0ZXMhISEhISE=")
	if err != nil {
		t.Fatal(err)
	}
	service := auth.NewOperatorService(database.New(databasetest.Pool(t)), keys, "https://desk.example.com/operator", func() bool { return true })
	ctx := context.Background()
	if _, err := service.Add(ctx, "ops@example.com", "cli", "tests"); err != nil {
		t.Fatal(err)
	}
	stale, err := service.CloudSignIn(ctx, "ops@example.com")
	if err != nil || stale.Step != "enroll" {
		t.Fatalf("first ticket = %+v, %v", stale, err)
	}
	fresh, err := service.CloudSignIn(ctx, "ops@example.com")
	if err != nil || fresh.Step != "enroll" {
		t.Fatalf("second ticket = %+v, %v", fresh, err)
	}
	// The newer ticket ended the older one.
	if _, _, err := service.ConfirmTOTP(ctx, stale.Ticket, auth.TOTPCode(stale.Secret, time.Now())); !errors.Is(err, auth.ErrOperatorLinkUsed) {
		t.Fatalf("an older ticket after a newer one: %v, want ErrOperatorLinkUsed", err)
	}
	// Two tickets open at once (a link and a cloud sign-in): setting up with one ends the other.
	link, err := service.Pair(ctx, "ops@example.com", false, "tests")
	if err != nil {
		t.Fatal(err)
	}
	other, err := service.Redeem(ctx, link[len("https://desk.example.com/operator/sign-in?token="):])
	if err != nil || other.Step != "enroll" {
		t.Fatalf("link = %+v, %v", other, err)
	}
	mine, err := service.CloudSignIn(ctx, "ops@example.com")
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := service.ConfirmTOTP(ctx, mine.Ticket, auth.TOTPCode(mine.Secret, time.Now())); err != nil {
		t.Fatalf("the operator's own setup: %v", err)
	}
	step, _, err := service.ConfirmTOTP(ctx, other.Ticket, auth.TOTPCode(other.Secret, time.Now()))
	if err == nil || step.Session != "" {
		t.Fatalf("a ticket left open after the authenticator was set up signed in: %+v", step)
	}
	// The operator's authenticator is still theirs.
	again, err := service.CloudSignIn(ctx, "ops@example.com")
	if err != nil || again.Step != "code" {
		t.Fatalf("next sign-in = %+v, %v", again, err)
	}
	if _, _, err := service.ConfirmTOTP(ctx, again.Ticket, auth.TOTPCode(mine.Secret, time.Now().Add(30*time.Second))); err != nil {
		t.Fatalf("the operator's code after the stale tickets: %v", err)
	}
}
