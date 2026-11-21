package store_test

import (
	"bytes"
	"context"
	"testing"

	"github.com/colliedesk/colliedesk/server/internal/database/databasetest"
	"github.com/colliedesk/colliedesk/server/internal/store"
)

// Server processes that start together and each ask for a secret no one made yet all get the same
// one, and later asks keep getting it.
func TestServerProcessesStartingTogetherShareOneSecret(t *testing.T) {
	pool := databasetest.Pool(t)
	ctx := context.Background()
	const processes = 8
	secrets := make([][]byte, processes)
	errs := make([]error, processes)
	atOnce(processes, func(i int) {
		secrets[i], errs[i] = store.New(pool).ServerSecret(ctx, "test-signing-key")
	})
	for i := range processes {
		if errs[i] != nil {
			t.Fatalf("process %d: %v", i, errs[i])
		}
		if len(secrets[i]) != 32 || !bytes.Equal(secrets[i], secrets[0]) {
			t.Fatalf("process %d got %x, process 0 got %x: want one 32-byte secret", i, secrets[i], secrets[0])
		}
	}
	later, err := store.New(pool).ServerSecret(ctx, "test-signing-key")
	if err != nil || !bytes.Equal(later, secrets[0]) {
		t.Fatalf("later ask = %x %v, want %x", later, err, secrets[0])
	}
	other, err := store.New(pool).ServerSecret(ctx, "test-other-key")
	if err != nil || len(other) != 32 || bytes.Equal(other, secrets[0]) {
		t.Fatalf("another name = %x %v, want a secret of its own", other, err)
	}
}
