package helpcenter

import "sync"

const (
	// streamsPerClient and streamsPerServer bound the videos a help center sends at once: a player
	// reads one or two ranges at a time, and a video is up to 200 MiB, so the page limiter (which
	// counts requests, not bytes) cannot keep one address from holding the server's bandwidth.
	streamsPerClient = 4
	streamsPerServer = 64
)

// streams counts the video responses being sent, per client address and on the server.
type streams struct {
	mu       sync.Mutex
	total    int
	byClient map[string]int
}

func newStreams() *streams { return &streams{byClient: map[string]int{}} }

// enter takes a slot for client, or reports false when the client or the server has none free.
func (s *streams) enter(client string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.total >= streamsPerServer || s.byClient[client] >= streamsPerClient {
		return false
	}
	s.total++
	s.byClient[client]++
	return true
}

func (s *streams) leave(client string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.total--
	if s.byClient[client]--; s.byClient[client] <= 0 {
		delete(s.byClient, client)
	}
}
