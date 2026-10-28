package httpapi

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/colliedesk/colliedesk/server/internal/store"
)

func TestAChannelRouteThatIsRateLimitedSaysWhenToTryAgain(t *testing.T) {
	recorder := httptest.NewRecorder()
	writeChannelError(recorder, fmt.Errorf("replay held: %w", store.ErrRateLimited))
	if recorder.Code != http.StatusTooManyRequests || !strings.Contains(recorder.Body.String(), `"code":"rate_limited"`) {
		t.Fatalf("status=%d body=%s, want 429 rate_limited", recorder.Code, recorder.Body.String())
	}
	if got := recorder.Header().Get("Retry-After"); got != strconv.Itoa(int(store.InboundRateWindow.Seconds())) {
		t.Fatalf("Retry-After=%q", got)
	}
}
