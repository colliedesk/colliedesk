package auth_test

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

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

func onceOperator(t *testing.T) (*auth.OperatorService, string) {
	t.Helper()
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
	return service, operators[0].ID
}

// Requests that arrive together with one Idempotency-Key make the change once: the others are told it is
// still running, and a request with the same key but another body is refused.
func TestOnceRequestsArrivingTogetherMakeTheChangeOnce(t *testing.T) {
	service, operator := onceOperator(t)
	const requests = 8
	var made atomic.Int32
	var others sync.WaitGroup
	others.Add(requests - 1)
	errs := make([]error, requests)
	var start, done sync.WaitGroup
	start.Add(1)
	for i := range requests {
		done.Add(1)
		go func() {
			defer done.Done()
			start.Wait()
			_, _, errs[i] = service.Once(context.Background(), operator, "token-create", "key-1", []byte("fp"), func(context.Context) (any, error) {
				made.Add(1)
				finished := make(chan struct{})
				go func() { others.Wait(); close(finished) }()
				select {
				case <-finished:
				case <-time.After(20 * time.Second):
				}
				return map[string]string{"token": "cdop_made"}, nil
			})
			if errs[i] != nil {
				others.Done()
			}
		}()
	}
	start.Done()
	done.Wait()
	if made.Load() != 1 {
		t.Fatalf("the change was made %d times, want once", made.Load())
	}
	inFlight := 0
	for _, err := range errs {
		if errors.Is(err, auth.ErrOperatorKeyInFlight) {
			inFlight++
		} else if err != nil {
			t.Fatalf("request: %v", err)
		}
	}
	if inFlight != requests-1 {
		t.Fatalf("%d requests were told the change is running, want %d", inFlight, requests-1)
	}
	_, _, err := service.Once(context.Background(), operator, "token-create", "key-1", []byte("another body"), func(context.Context) (any, error) {
		t.Fatal("a reused key made the change")
		return nil, nil
	})
	if !errors.Is(err, auth.ErrOperatorKeyReused) {
		t.Fatalf("same key, another body = %v, want %v", err, auth.ErrOperatorKeyReused)
	}
}

// A change that fails frees its Idempotency-Key, so the retry makes it.
func TestOnceAFailedChangeLetsTheRetryMakeIt(t *testing.T) {
	service, operator := onceOperator(t)
	failure := errors.New("provider down")
	_, _, err := service.Once(context.Background(), operator, "token-create", "key-1", []byte("fp"), func(context.Context) (any, error) {
		return nil, failure
	})
	if !errors.Is(err, failure) {
		t.Fatalf("failed change = %v, want %v", err, failure)
	}
	retried := false
	stored, replayed, err := service.Once(context.Background(), operator, "token-create", "key-1", []byte("fp"), func(context.Context) (any, error) {
		retried = true
		return map[string]string{"token": "cdop_made"}, nil
	})
	if err != nil || !retried || replayed || stored != nil {
		t.Fatalf("retry = %s %v %v (made %v), want the change made now", stored, replayed, err, retried)
	}
}
