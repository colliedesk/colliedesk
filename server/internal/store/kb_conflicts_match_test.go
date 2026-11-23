package store_test

import (
	"math"
	"testing"

	"github.com/colliedesk/colliedesk/server/internal/store"
)

const returnsArticle = "Return policy\n\nYou can return unused items within 30 days of delivery. " +
	"Refunds go to the original payment method within 5 business days."

// The scores below were worked by hand: the length of the reply paragraph's distinct words of 3 or
// more letters found in the article, over the length of all of them, for the best paragraph.
func TestKBConflictScoreSendsOnlyRepliesOnTheArticlesTopic(t *testing.T) {
	cases := []struct {
		name, reply, article string
		want                 float64
		match                bool
	}{
		{
			// you can return unused items within days delivery refund = 47 of 54 (for, full miss).
			name:    "same topic, a different number, with a greeting and sign-off",
			reply:   "Hi Anna,\n\nYou can return unused items within 60 days of delivery for a full refund.\n\nBest,\nSam",
			article: returnsArticle,
			want:    47.0 / 54.0,
			match:   true,
		},
		{
			// you can return unused items within days delivery = 41 of 41.
			name:    "same topic, the same words: the model decides it agrees",
			reply:   "You can return unused items within 30 days of delivery.",
			article: returnsArticle,
			want:    1,
			match:   true,
		},
		{
			// the delivery business days payment = 30 of 99: four long words in common, a small share.
			name: "another topic that shares four long words",
			reply: "Your order shipped today and the tracking link is in your email. Delivery usually " +
				"takes 3 to 5 business days, and the payment was charged on Monday.",
			article: returnsArticle,
			want:    30.0 / 99.0,
			match:   false,
		},
		{
			// you within match, but within is the only long word in common: too few to judge.
			name: "another topic that shares a word or two",
			reply: "Hi Anna, to reset your password open Settings, choose Security and click Reset password. " +
				"You will get an email within a few minutes. Best, Sam",
			article: returnsArticle,
			want:    0,
			match:   false,
		},
		{
			// The second paragraph alone counts: 47 of 54, as in the first case.
			name: "a reply about two things, one of them the article's",
			reply: "To reset your password open Settings and click Reset password.\n\n" +
				"You can return unused items within 60 days of delivery for a full refund.",
			article: returnsArticle,
			want:    47.0 / 54.0,
			match:   true,
		},
		{
			// nhan doi tra san pham chua dung trong vong ngay khi hang = 45 of 55 (chao chi ben miss).
			name:    "Vietnamese with accents, folded",
			reply:   "Chào chị, bên em nhận đổi trả sản phẩm chưa sử dụng trong vòng 60 ngày kể từ khi nhận hàng ạ.",
			article: "Chính sách đổi trả\n\nBạn có thể đổi trả sản phẩm chưa sử dụng trong vòng 30 ngày kể từ khi nhận hàng.",
			want:    45.0 / 55.0,
			match:   true,
		},
		{
			// return days = 10 of 10, but only 2 matched words of 4 or more letters: too few to tell.
			name:    "too few words to judge",
			reply:   "Return days?",
			article: returnsArticle,
			want:    0,
			match:   false,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := store.KBConflictScore(c.reply, c.article)
			if math.Abs(got-c.want) > 0.001 {
				t.Fatalf("score = %.3f, want %.3f", got, c.want)
			}
			if match := got >= store.KBConflictMatchThreshold; match != c.match {
				t.Fatalf("score %.3f against threshold %.2f: match = %v, want %v",
					got, store.KBConflictMatchThreshold, match, c.match)
			}
		})
	}
}
