package store

import (
	"strconv"
	"testing"

	"github.com/colliedesk/colliedesk/server/internal/domain"
)

// The topic record query writes the similarity bar as SQL text; it must be the one the domain uses.
func TestTheTopicRecordUsesTheDomainSimilarityBar(t *testing.T) {
	sql, err := strconv.ParseFloat(aiAcceptedSimilaritySQL, 64)
	if err != nil || sql != domain.AIAcceptedSimilarity {
		t.Fatalf("aiAcceptedSimilaritySQL = %q, domain.AIAcceptedSimilarity = %v", aiAcceptedSimilaritySQL, domain.AIAcceptedSimilarity)
	}
}
