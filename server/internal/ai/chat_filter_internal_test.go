package ai

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"
)

func evalArticles(t *testing.T) string {
	t.Helper()
	data, err := os.ReadFile("testdata/chat-eval.json")
	if err != nil {
		t.Fatal(err)
	}
	var eval struct {
		Articles []struct{ Title, Body string } `json:"articles"`
	}
	if err := json.Unmarshal(data, &eval); err != nil {
		t.Fatal(err)
	}
	var text strings.Builder
	for _, a := range eval.Articles {
		text.WriteString(a.Title + "\n" + a.Body + "\n")
	}
	return text.String()
}

// stream sends an answer through the sentence cutter and the filter in pieces of the given size.
func stream(f *chatFilter, answer string, piece int) (string, bool) {
	var c chatSentences
	var out strings.Builder
	hidden := false
	at := time.Unix(0, 0)
	send := func(sentences []string) {
		for _, s := range sentences {
			text, h, _ := f.check(s)
			hidden = hidden || h
			out.WriteString(text)
		}
	}
	runes := []rune(answer)
	for i := 0; i < len(runes); i += piece {
		end := min(i+piece, len(runes))
		at = at.Add(400 * time.Millisecond)
		send(c.add(string(runes[i:end]), at))
	}
	send(c.rest())
	return out.String(), hidden
}

// Answers Claude gave from the eval's articles keep every word: prices, "10.000", placeholders of
// links it translated, settings paths and article numbers are the articles' own.
func TestRealAnswersFromTheArticlesAreNeverHidden(t *testing.T) {
	data, err := os.ReadFile("testdata/chat-eval-answers.json")
	if err != nil {
		t.Fatal(err)
	}
	var answers []string
	if err := json.Unmarshal(data, &answers); err != nil {
		t.Fatal(err)
	}
	f := newChatFilter(evalArticles(t))
	for _, answer := range answers {
		for _, piece := range []int{3, 17, 1000} {
			if out, hidden := stream(f, answer, piece); hidden || out != answer {
				t.Errorf("piece %d hid part of %q: %q", piece, answer, out)
			}
		}
	}
}

// Contact details the articles do not hold are hidden with their sentence, however the stream splits
// them; the same details written another way in the articles are kept.
func TestMadeUpContactDetailsAreHidden(t *testing.T) {
	f := newChatFilter(evalArticles(t) + "Call us on +84 (912) 345-678 or write to help@parcelpilot.example. See https://parcelpilot.example/status.")
	for _, c := range []struct {
		answer string
		hidden bool
	}{
		{"You can call +84 912 345 678 any day [#104]. Thanks!", false},
		{"Call +84 999 888 777 for a refund. The rest stays.", true},
		{"Email billing@other.example and ask. Next.", true},
		{"Write to help@parcelpilot.example [#110].", false},
		{"Pay at https://evil.example/pay now. Done.", true},
		{"Open www.parcelpilot.example/status for news.", false},
		{"Call 0912.345.678 now. Or not.", true},
		{"Growth costs 49 US dollars for up to 2,000 orders [#108].", false},
		{"Track it at track.parcelpilot.example/<mã đơn hàng> [#103].", false},
	} {
		for _, piece := range []int{2, 5, 1000} {
			out, hidden := stream(f, c.answer, piece)
			if hidden != c.hidden {
				t.Errorf("piece %d %q: hidden=%v out=%q", piece, c.answer, hidden, out)
			}
			if hidden && (strings.Contains(out, "999") || strings.Contains(out, "billing@") || strings.Contains(out, "evil")) {
				t.Errorf("hidden details leaked: %q", out)
			}
		}
	}
}

// A long sentence is sent before its end, never in the middle of a number written in groups.
func TestALongSentenceIsSentEarlyWithoutSplittingANumber(t *testing.T) {
	var c chatSentences
	at := time.Unix(0, 0)
	words := strings.Repeat("word ", 45) + "+84 912 345"
	if out := c.add(words, at); len(out) != 1 || strings.Contains(out[0], "912") || !strings.HasSuffix(strings.TrimSpace(out[0]), "word") {
		t.Fatalf("early cut = %q", out)
	}
	// The number and its last word wait together; the end of the stream sends them whole.
	if out := c.add(" 678 now", at.Add(2*time.Second)); len(out) != 0 {
		t.Fatalf("second = %q", out)
	}
	if rest := c.rest(); len(rest) != 1 || !strings.Contains(rest[0], "+84 912 345 678") {
		t.Fatalf("rest = %q", rest)
	}
	if got := chatCited("See [#12], then [#3] and [#12] [#x]."); len(got) != 2 || got[0] != 12 || got[1] != 3 {
		t.Fatalf("cited = %v", got)
	}
}
