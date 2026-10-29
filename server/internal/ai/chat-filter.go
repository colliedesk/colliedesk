package ai

import (
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

// An answer reaches the end user sentence by sentence, each checked first: an email address, a phone
// number or a link that the articles the AI read do not hold is never shown (the AI made it up, or
// was talked into it). docs/internals/ai-chat.md, "Streaming".

// ChatHidden replaces a sentence that held contact details the articles do not.
const ChatHidden = "[contact details hidden — not in the articles]"

const (
	// chatFlushRunes and chatFlushAfter send what is buffered without waiting for a sentence's end.
	chatFlushRunes = 200
	chatFlushAfter = 1500 * time.Millisecond
)

// chatSentences cuts a stream of text into sentences: at a sentence's end, or early when the buffer is
// long or old, at the last space that does not split a run of digits (a phone number written in groups).
type chatSentences struct {
	buffer  string
	started time.Time
}

func sentenceEnd(r rune) bool {
	return r == '.' || r == '?' || r == '!' || r == '。' || r == '！' || r == '？' || r == '\n'
}

// add takes the next piece of text and returns the sentences now complete.
func (c *chatSentences) add(text string, now time.Time) []string {
	if c.buffer == "" {
		c.started = now
	}
	c.buffer += text
	var out []string
	for {
		cut := -1
		runes := []rune(c.buffer)
		for i, r := range runes {
			// A sentence ends at its mark followed by a space; "10.000" and "a.b.example" go on.
			if sentenceEnd(r) && i+1 < len(runes) && (unicode.IsSpace(runes[i+1]) || r == '\n') {
				cut = i + 1
				break
			}
		}
		if cut < 0 {
			break
		}
		out = append(out, string(runes[:cut]))
		c.buffer = string(runes[cut:])
		c.started = now
	}
	if c.buffer != "" && (utf8.RuneCountInString(c.buffer) >= chatFlushRunes || now.Sub(c.started) >= chatFlushAfter) {
		if cut := earlyCut(c.buffer); cut > 0 {
			out = append(out, c.buffer[:cut])
			c.buffer = c.buffer[cut:]
			c.started = now
		}
	}
	return out
}

// rest is what is left when the stream ends.
func (c *chatSentences) rest() []string {
	if strings.TrimSpace(c.buffer) == "" {
		return nil
	}
	out := []string{c.buffer}
	c.buffer = ""
	return out
}

// earlyCut is where a buffer may be sent before its sentence ends: after the last space, keeping back
// the last word and any run of number groups before it ("+84 912 345"), which may be half a phone number.
func earlyCut(buffer string) int {
	cut := strings.LastIndexFunc(buffer, unicode.IsSpace)
	for cut > 0 {
		before := strings.TrimRightFunc(buffer[:cut], unicode.IsSpace)
		start := strings.LastIndexFunc(before, unicode.IsSpace) + 1
		word := before[start:]
		if word == "" || strings.IndexFunc(word, func(r rune) bool { return !strings.ContainsRune("0123456789+().-", r) }) >= 0 {
			break
		}
		cut = start
	}
	return max(cut, 0)
}

var (
	chatEmail = regexp.MustCompile(`[A-Za-z0-9._%+-]+@[A-Za-z0-9-]+(?:\.[A-Za-z0-9-]+)*\.[A-Za-z]{2,}`)
	chatPhone = regexp.MustCompile(`\+?\(?\d[\d\s().-]{4,}\d`)
	// chatLink: a scheme, www., a domain with a path, or a domain under a common top-level domain.
	chatLink     = regexp.MustCompile(`(?i)\b(?:https?://\S+|www\.[a-z0-9.-]+\S*|(?:[a-z0-9-]+\.)+[a-z]{2,}/\S*|(?:[a-z0-9-]+\.)+(?:com|net|org|io|co|app|dev|info|biz|me|us|uk|vn|de|fr|au|ca|ai|shop|store|example)\b)`)
	chatCitation = regexp.MustCompile(`\[#(\d{1,18})\]`)
)

// chatFilter checks sentences against the text of the articles the AI read.
type chatFilter struct {
	text   string
	digits []string
}

func newChatFilter(articles string) *chatFilter {
	f := &chatFilter{text: normalizeLinks(strings.ToLower(articles))}
	for _, match := range chatPhone.FindAllString(articles, -1) {
		f.digits = append(f.digits, onlyDigits(match))
	}
	return f
}

func onlyDigits(text string) string {
	return strings.Map(func(r rune) rune {
		if r >= '0' && r <= '9' {
			return r
		}
		return -1
	}, text)
}

// normalizeLinks drops schemes and www. so a link matches the article's however it is written.
func normalizeLinks(text string) string {
	return strings.NewReplacer("https://", "", "http://", "", "www.", "").Replace(text)
}

func cleanLink(link string) string {
	return strings.TrimRight(normalizeLinks(strings.ToLower(link)), ".,;:!?)]}>\"'")
}

// check returns the sentence as the end user may see it, whether it was hidden, and the links it may
// show as links.
func (f *chatFilter) check(sentence string) (string, bool, []string) {
	for _, email := range chatEmail.FindAllString(sentence, -1) {
		if !strings.Contains(f.text, strings.ToLower(email)) {
			return hiddenSentence(sentence), true, nil
		}
	}
	for _, phone := range chatPhone.FindAllString(sentence, -1) {
		digits := onlyDigits(phone)
		if len(digits) < 6 {
			continue
		}
		if !slices.ContainsFunc(f.digits, func(known string) bool { return strings.Contains(known, digits) }) {
			return hiddenSentence(sentence), true, nil
		}
	}
	var links []string
	for _, link := range chatLink.FindAllString(sentence, -1) {
		if chatEmail.MatchString(link) {
			continue
		}
		clean := cleanLink(link)
		// A placeholder the AI translated ("/<order number>" written in Vietnamese) keeps the part before it.
		if at := strings.IndexByte(clean, '<'); at > 0 && !strings.Contains(f.text, clean) {
			clean = clean[:at]
		}
		if !strings.Contains(f.text, clean) {
			return hiddenSentence(sentence), true, nil
		}
		links = append(links, strings.TrimRight(link, ".,;:!?)]}>\"'"))
	}
	return sentence, false, links
}

// hiddenSentence keeps the sentence's layout (a list item stays one) without its words.
func hiddenSentence(sentence string) string {
	lead := sentence[:len(sentence)-len(strings.TrimLeftFunc(sentence, unicode.IsSpace))]
	trail := sentence[len(strings.TrimRightFunc(sentence, unicode.IsSpace)):]
	return lead + ChatHidden + trail
}

// chatCited is the numbers an answer cites, in the order it first cites them.
func chatCited(answer string) []int64 {
	var numbers []int64
	for _, match := range chatCitation.FindAllStringSubmatch(answer, -1) {
		n, err := strconv.ParseInt(match[1], 10, 64)
		if err == nil && !slices.Contains(numbers, n) {
			numbers = append(numbers, n)
		}
	}
	return numbers
}
