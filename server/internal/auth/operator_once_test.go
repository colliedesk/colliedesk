package auth_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/colliedesk/colliedesk/server/internal/auth"
	"github.com/colliedesk/colliedesk/server/internal/database"
	"github.com/colliedesk/colliedesk/server/internal/database/databasetest"
	"github.com/colliedesk/colliedesk/server/internal/tokenseal"
)

// Review ADR 4: a client that hangs up while its change is made still gets the answer on a retry with the
// same Idempotency-Key: the answer is recorded even when the request's context ended.
func TestOnceRecordsTheAnswerWhenTheClientLeaves(t *testing.T) {
	keys, err := tokenseal.ParseKeys("k1:b3BlcmF0b3ItdGVzdC1rZXktMzItYnl0ZXMhISEhISE=")
	if err != nil {
		t.Fatal(err)
	}
	service := auth.NewOperatorService(database.New(databasetest.Pool(t)), keys, "https://desk.example.com/operator", nil)
	if _, err := service.Pair(context.Background(), "ops@example.com", false, "tests"); err != nil {
		t.Fatal(err)
	}
	operators, err := service.Operators(context.Background())
	if err != nil || len(operators) != 1 {
		t.Fatalf("operators = %v %v", operators, err)
	}
	ctx, hangUp := context.WithCancel(context.Background())
	_, _, err = service.Once(ctx, operators[0].ID, "token-create", "key-1", []byte("fp"), func(context.Context) (any, error) {
		hangUp()
		return map[string]string{"token": "cdop_made"}, nil
	})
	if err != nil {
		t.Fatalf("first call: %v", err)
	}
	stored, replayed, err := service.Once(context.Background(), operators[0].ID, "token-create", "key-1", []byte("fp"), func(context.Context) (any, error) {
		t.Fatal("the retry made the change again")
		return nil, nil
	})
	var answer map[string]string
	_ = json.Unmarshal(stored, &answer)
	if err != nil || !replayed || answer["token"] != "cdop_made" {
		t.Fatalf("retry = %s %v %v, want the recorded answer", stored, replayed, err)
	}
}
