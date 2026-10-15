package httpapi

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
)

func TestStoreErrorTellsClientsToRetryAfterADeadlockOrSerializationFailure(t *testing.T) {
	for name, code := range map[string]string{"deadlock": "40P01", "serialization failure": "40001"} {
		t.Run(name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			writeStoreError(recorder, fmt.Errorf("bulk update conversations: %w", &pgconn.PgError{Code: code}))
			if recorder.Code != http.StatusConflict || !strings.Contains(recorder.Body.String(), "try again") {
				t.Fatalf("status=%d body=%s, want 409 asking to try again", recorder.Code, recorder.Body.String())
			}
		})
	}
}
