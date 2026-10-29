package ai

import (
	"crypto/sha256"
	"encoding/hex"
	"testing"
)

// The rules open every chat's prompt and are cached with the articles: a change of one byte makes every
// workspace's next question a cache miss, so they change only on purpose, here and in
// docs/internals/ai-chat.md together.
func TestTheChatRulesAreFixed(t *testing.T) {
	const want = "2e64373fc13de65cb2d2888f1548839163e857f5705ed264bcd8e9854d0fc9d5"
	sum := sha256.Sum256([]byte(chatRules))
	if got := hex.EncodeToString(sum[:]); got != want {
		t.Fatalf("chatRules hash = %s; if the change is meant, update the hash", got)
	}
}
