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
		start := 0
		if space := strings.LastIndexFunc(before, unicode.IsSpace); space >= 0 {
			// A space may be more than one byte (a thin or no-break space).
			_, size := utf8.DecodeRuneInString(before[space:])
			start = space + size
		}
		word := before[start:]
		if word == "" || strings.IndexFunc(word, func(r rune) bool { return !phoneWordRune(r) }) >= 0 {
			break
		}
		cut = start
	}
	return max(cut, 0)
}

var (
	chatEmail = regexp.MustCompile(`[A-Za-z0-9._%+-]+@[A-Za-z0-9-]+(?:\.[A-Za-z0-9-]+)*\.[A-Za-z]{2,}`)
	chatPhone = regexp.MustCompile(`\+?\(?\d[\d\s().-]{4,}\d`)
	// chatDashed is two or three groups of digits joined by dashes (after foldPhones), which
	// isDashedRangeOrDate reads.
	chatDashed = regexp.MustCompile(`^\(?(\d+)-(\d+)(?:-(\d+))?$`)
	// chatLink: a scheme, www., a domain with a path, or a bare lower-case domain under any top-level
	// domain of letters (acme-login.xyz, bit.ly). A capitalised word after a missing space ("Save.Then")
	// is no domain, and a file name (invoice.pdf) is left to chatFileName.
	chatLink     = regexp.MustCompile(`\b(?:(?i:https?://\S+|www\.[a-z0-9.-]+\S*|(?:[a-z0-9-]+\.)+[a-z]{2,}/\S*)|(?:[a-z0-9-]+\.)+[a-z]{2,24}\b)`)
	chatFileName = regexp.MustCompile(`^[^/]*\.(?:pdf|csv|png|jpe?g|gif|svg|webp|json|js|ts|txt|zip|docx?|xlsx?|pptx?|md|html?|css|py|xml|ya?ml|exe|dmg|apk|mp4|mp3)$`)
	chatCitation = regexp.MustCompile(`\[#(\d{1,18})\]`)
)

// isDashedRangeOrDate reports whether a number chatPhone found (after foldPhones) is a range or a date
// written with dashes, not a phone number. Only three forms are: a range of years from 1900 to 2099
// that does not go backwards ("2024–2026"), a range whose sides have three digits or fewer ("100–500"),
// and a real year, month and day ("2026–11–15"). Every other dashed form is read as a phone number,
// since the common short ones are dashed groups of four or three and four digits: "1900-1234",
// "2123-4567", "555-1234".
func isDashedRangeOrDate(phone string) bool {
	groups := chatDashed.FindStringSubmatch(phone)
	if groups == nil {
		return false
	}
	from, to, day := groups[1], groups[2], groups[3]
	if day != "" {
		if len(from) != 4 || len(to) != 2 || len(day) != 2 {
			return false
		}
		_, err := time.Parse(time.DateOnly, from+"-"+to+"-"+day)
		return err == nil
	}
	if len(from) <= 3 && len(to) <= 3 {
		return true
	}
	// Strings of four digits compare as their numbers do.
	return len(from) == 4 && len(to) == 4 && from >= "1900" && to >= from && to <= "2099"
}

// chatFilter checks sentences against the text of the articles the AI read. One stream uses it, one
// sentence after another, never from two goroutines.
type chatFilter struct {
	text string
	// articles are read for their phone numbers (digits) at the first sentence that holds one: most
	// answers hold none, and a chat turn builds its filter before the provider is called.
	articles string
	digits   []string
	scanned  bool
}

func newChatFilter(articles string) *chatFilter {
	return &chatFilter{text: normalizeLinks(strings.ToLower(articles)), articles: articles}
}

// knownDigits are the digits of the articles' phone numbers.
func (f *chatFilter) knownDigits() []string {
	if !f.scanned {
		f.digits, f.articles, f.scanned = phoneDigits(f.articles), "", true
	}
	return f.digits
}

// phoneDigits are the digits of each phone number (chatPhone) in text, read as foldPhones writes it.
// A number is made only of digits, spaces and +().-, and chatPhone has no anchor or word boundary, so
// its matches are the same when it reads only the runs of those characters that hold two digits and six
// characters or more: the pattern over 180 KB of excerpts took 12 ms (docs/internals/performance.md).
// Each run is folded on its own, so the letters between runs are never copied.
func phoneDigits(text string) []string {
	var digits []string
	var run []byte
	count := 0
	flush := func() {
		if count >= 2 && len(run) >= 6 {
			for _, match := range chatPhone.FindAll(run, -1) {
				digits = append(digits, onlyDigits(string(match)))
			}
		}
		run, count = run[:0], 0
	}
	for _, r := range text {
		f := foldPhoneRune(r)
		switch {
		case f < 0: // an invisible character inside a number
		case f >= utf8.RuneSelf || !phoneByte(byte(f)):
			flush()
		default:
			run = append(run, byte(f))
			if f >= '0' && f <= '9' {
				count++
			}
		}
	}
	flush()
	return digits
}

// phoneByte is a folded byte chatPhone can match: a digit, \s (ASCII spaces) or +().-.
func phoneByte(b byte) bool {
	return b >= '0' && b <= '9' || strings.IndexByte(" \t\n\f\r+().-", b) >= 0
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
	for _, phone := range chatPhone.FindAllString(foldPhones(sentence), -1) {
		digits := onlyDigits(phone)
		if len(digits) < 6 || isDashedRangeOrDate(phone) {
			continue
		}
		if !slices.ContainsFunc(f.knownDigits(), func(known string) bool { return strings.Contains(known, digits) }) {
			return hiddenSentence(sentence), true, nil
		}
	}
	var links []string
	for _, link := range chatLink.FindAllString(sentence, -1) {
		if chatEmail.MatchString(link) || chatFileName.MatchString(strings.ToLower(link)) {
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
