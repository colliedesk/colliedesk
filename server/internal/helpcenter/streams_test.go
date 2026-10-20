package helpcenter

import (
	"fmt"
	"testing"
)

// One address plays at most streamsPerClient videos at once, the server streamsPerServer; a finished
// response frees its slot.
func TestVideoStreamsAreBoundedPerAddressAndServer(t *testing.T) {
	s := newStreams()
	for range streamsPerClient {
		if !s.enter("192.0.2.1") {
			t.Fatal("a slot below the bound was refused")
		}
	}
	if s.enter("192.0.2.1") {
		t.Fatal("one address got more than its bound")
	}
	s.leave("192.0.2.1")
	if !s.enter("192.0.2.1") {
		t.Fatal("a freed slot stayed taken")
	}
	for i := 0; s.total < streamsPerServer; i++ {
		if !s.enter(fmt.Sprintf("198.51.100.%d", i/streamsPerClient)) {
			t.Fatalf("refused at %d of %d", s.total, streamsPerServer)
		}
	}
	if s.enter("203.0.113.9") {
		t.Fatal("the server went past its bound")
	}
}
