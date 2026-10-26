// Package prompt lays out what the AI decider reads about one intake card, and reads its answer. It
// reads no clock and no database: the caller hands it a Snapshot, so a replay for evaluation builds the
// same text. People are labels ("Customer A", "Team 1"), credentials are hidden, contact identifiers are
// masked, and nothing it writes names a channel type or the language a channel speaks: those words are
// data (Snapshot.RedactKeywords, Snapshot.Instructions).
package prompt

import (
	"fmt"
	"regexp"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/colliedesk/colliedesk/server/internal/aiprovider"
)

// Version names the frame's instructions and layout; every decision records it.
const Version = "intake-2026-10-26.1"

// The sources the frame builds (docs/internals/intake.md "What AI reads").
const (
	SourceIncoming          = "incoming"
	SourceThreadWindow      = "thread_window"
	SourceOpenConversations = "open_conversations"
	SourceContact           = "contact"
	SourceImages            = "images"
)

// Person is someone who wrote in the place: a customer, or the team (a person or the channel's own bot).
type Person struct {
	// Key is who they are on the channel; people with the same key get the same label.
	Key  string
	Name string
	Team bool
}

// Item is one message in the place.
type Item struct {
	// ID tells a quoted message that is one of the items shown.
	ID   string
	At   time.Time
	From Person
	Text string
	// Media are the kinds of files that came with it: "image", "video", "file".
	Media []string
	Quote *Quote
	// ConversationID is the conversation the message is in, when it is one.
	ConversationID string
}

// Quote is the message an item replies to.
type Quote struct {
	ID             string
	At             time.Time
	From           Person
	Text           string
	Media          []string
	ConversationID string
}

// Candidate is a conversation the card may be attached to.
type Candidate struct {
	ConversationID string
	Status         string
	UpdatedAt      time.Time
	Subject        string
	LastMessage    string
	Reporter       Person
}

// Contact is what the workspace knows of the customer the card is from.
type Contact struct {
	Person            Person
	Company           string
	OpenConversations int
}

// Command is one of the channel's commands that opens a request.
type Command struct {
	Key, Description string
}

// Budget bounds what the model reads, in characters (runes) and items.
type Budget struct {
	Total       int // every source together
	Incoming    int // the card's own messages are never cut below this
	ItemRunes   int // one message before the card
	WindowItems int // messages before the card
	QuoteRunes  int // the quoted message of a reply
	Candidates  int // conversations offered
}

// DefaultBudget is the frame's budget (design §2a).
var DefaultBudget = Budget{Total: 24000, Incoming: 4000, ItemRunes: 1500, WindowItems: 15, QuoteRunes: 300, Candidates: 30}

// Place is where the card's messages were written.
type Place struct {
	// OneToOne is a private chat between one customer and the team; otherwise a group.
	OneToOne bool
	Name     string
}

// Snapshot is everything the builder reads, gathered by the caller at one moment.
type Snapshot struct {
	Location *time.Location // times are shown in it; nil is UTC
	Locale   string         // the language subjects and reasons are written in
	Place    Place
	// Before are the messages of the place before the card, oldest first. More than WindowItems may be
	// given: the ones not shown are still read, so a login there hides the password that follows it.
	Before     []Item
	Incoming   []Item
	Candidates []Candidate
	Contact    *Contact
	Commands   []Command
	// Images are the card's pictures the model may see: the caller leaves them out when the model takes
	// no images or the workspace has not agreed to send them, and says so in Skipped.
	Images []aiprovider.Image
	// Sources turns a source off (false) or on; one not named keeps its default (on).
	Sources map[string]bool
	// Skipped are sources the caller left out, with why ("consent", "model_text_only").
	Skipped        map[string]string
	RedactKeywords []string
	ContactLabels  ContactLabels
	Instructions   string
	Budget         Budget
}

// SourceManifest says what one source put in the prompt, without its content.
type SourceManifest struct {
	Source  string `json:"source"`
	Runes   int    `json:"runes"`
	Items   int    `json:"items"`
	Cut     bool   `json:"cut,omitempty"`
	Hidden  int    `json:"hidden,omitempty"`
	Skipped string `json:"skipped,omitempty"`
}

// Context is one prompt, with what is needed to read its answer.
type Context struct {
	System, User string
	Images       []aiprovider.Image
	Manifest     []SourceManifest
	// Credential is set when the card's messages, or the messages they reply to, held credentials: the
	// card is never applied by AI on its own.
	Credential bool
	// ImagesWithheld counts the pictures not sent because the card talks about credentials.
	ImagesWithheld int
	// Commands are the command keys the answer may name.
	Commands []string
	// candidates maps the labels C1..Cn to conversation ids; names maps people's labels to their names.
	candidates map[string]string
	names      map[string]string
}

// CandidateCount is how many conversations the model was offered.
func (c Context) CandidateCount() int { return len(c.candidates) }

// conversationRef is the placeholder for the label of the conversation a message is in.
var conversationRef = regexp.MustCompile("\x01C\\d+\x01  ")

// mediaMarkers stand for the files that came with a message.
var mediaMarkers = map[string]string{"image": "[image]", "video": "[video]", "file": "[file]"}

const timeLayout = "01-02 15:04"

// Build lays out the card for the model.
func Build(s Snapshot) Context {
	budget := s.Budget
	if budget == (Budget{}) {
		budget = DefaultBudget
	}
	location := s.Location
	if location == nil {
		location = time.UTC
	}
	on := func(source string) bool { enabled, set := s.Sources[source]; return !set || enabled }
	redact := newRedactor(s.RedactKeywords)
	people := newPeople()

	// A login and its password may arrive as two messages, so credentials are hidden across the
	// consecutive messages of one sender, including the ones before the window.
	conversation := slices.Concat(s.Before, s.Incoming)
	firstShown := 0
	if on(SourceThreadWindow) {
		firstShown = max(len(s.Before)-budget.WindowItems, 0)
	} else {
		firstShown = len(s.Before)
	}
	// Labels go to the card's sender first, then to the people as they appear, then to the reporters.
	sender := "the customer"
	if len(s.Incoming) > 0 {
		sender = people.label(s.Incoming[0].From.Key, s.Incoming[0].From.Name, s.Incoming[0].From.Team)
	}
	for _, item := range conversation[firstShown:] {
		people.label(item.From.Key, item.From.Name, item.From.Team)
		if item.Quote != nil {
			people.label(item.Quote.From.Key, item.Quote.From.Name, item.Quote.From.Team)
		}
	}
	candidates := s.Candidates
	if !on(SourceOpenConversations) {
		candidates = nil
	}
	if len(candidates) > budget.Candidates {
		candidates = candidates[:budget.Candidates]
	}
	for _, candidate := range candidates {
		people.label(candidate.Reporter.Key, candidate.Reporter.Name, candidate.Reporter.Team)
	}
	contact := s.Contact
	if !on(SourceContact) {
		contact = nil
	}
	if contact != nil {
		people.label(contact.Person.Key, contact.Person.Name, contact.Person.Team)
	}
	anonymise := people.anonymiser()
	masker := newContactMasker(s.ContactLabels)
	scrub := func(text string) string { return masker.mask(oneLine(anonymise(text))) }

	out := Context{candidates: map[string]string{}, names: people.names}
	labelOf := map[string]string{}
	for i, candidate := range candidates {
		label := fmt.Sprintf("C%d", i+1)
		out.candidates[label] = candidate.ConversationID
		labelOf[candidate.ConversationID] = label
	}
	// A message in an offered conversation says which one, once the budget has settled which are offered:
	// until then it holds a placeholder as long as " in Cn", so the sizes count it right.
	in := func(conversationID string) string {
		if labelOf[conversationID] != "" {
			return "\x01" + labelOf[conversationID] + "\x01" + "  "
		}
		return ""
	}

	bodies, hid := redactBySender(redact, conversation, scrub)
	shown := map[string]int{}
	for i, item := range conversation {
		if item.ID != "" {
			shown[item.ID] = i
		}
	}
	line := func(i int, limit int) (string, bool) {
		item := conversation[i]
		var parts []string
		quoteHid := false
		if q := item.Quote; q != nil {
			var body string
			if j, ok := shown[q.ID]; ok && q.ID != "" {
				body, quoteHid = bodies[j], hid[j]
			} else {
				texts, changed := redact.sequence([]string{q.Text})
				body, quoteHid = scrub(redact.secretShapes(texts[0])), changed[0]
			}
			parts = append(parts, quoteMarker(q, people.label(q.From.Key, q.From.Name, q.From.Team), body, in(q.ConversationID), location, budget.QuoteRunes))
		}
		parts = append(parts, markers(item.Media)...)
		if bodies[i] != "" {
			parts = append(parts, clipTo(bodies[i], limit))
		}
		who := people.label(item.From.Key, item.From.Name, item.From.Team)
		return fmt.Sprintf("%s %s%s: %s", item.At.In(location).Format(timeLayout), who, in(item.ConversationID), strings.Join(parts, " ")), hid[i] || quoteHid
	}

	var window, incoming []string
	windowHidden, incomingHidden := 0, 0
	for i := firstShown; i < len(s.Before); i++ {
		text, h := line(i, budget.ItemRunes)
		window = append(window, "   "+aiprovider.EscapeData(text))
		windowHidden += hiddenCount(text, h)
	}
	for i := len(s.Before); i < len(conversation); i++ {
		// The card's own messages are cut only to fit the whole budget, below.
		text, h := line(i, budget.Total)
		incoming = append(incoming, ">> "+aiprovider.EscapeData(text))
		incomingHidden += hiddenCount(text, h)
		out.Credential = out.Credential || h
	}

	var offered []string
	offeredHidden := 0
	for i, candidate := range candidates {
		texts, _ := redact.sequence([]string{candidate.Subject, candidate.LastMessage})
		subject := orDefault(clipTo(scrub(redact.secretShapes(texts[0])), 200), "(no subject)")
		text := fmt.Sprintf("C%d · reported by %s · %s · updated %s · subject «%s»", i+1,
			people.label(candidate.Reporter.Key, candidate.Reporter.Name, candidate.Reporter.Team), candidate.Status,
			candidate.UpdatedAt.In(location).Format(timeLayout), subject)
		if last := clipTo(scrub(redact.secretShapes(texts[1])), budget.QuoteRunes); last != "" {
			text += " · last message «" + last + "»"
		}
		offered = append(offered, text)
		offeredHidden += hiddenCount(text, false)
	}

	var contactLine string
	if contact != nil {
		contactLine = people.label(contact.Person.Key, contact.Person.Name, contact.Person.Team)
		if company := scrub(contact.Company); company != "" {
			contactLine += " · company " + clipTo(company, 200)
		}
		contactLine += fmt.Sprintf(" · open conversations: %d", contact.OpenConversations)
	}

	// Over budget, sources go in this order: the contact, then the oldest messages before the card, then
	// the conversations offered last (the caller lists the likeliest first), and the card's own messages last of
	// all, never below their floor.
	manifest := map[string]*SourceManifest{
		SourceIncoming:          {Source: SourceIncoming, Items: len(incoming), Hidden: incomingHidden},
		SourceThreadWindow:      {Source: SourceThreadWindow, Items: len(window), Hidden: windowHidden, Cut: firstShown > 0 && on(SourceThreadWindow)},
		SourceOpenConversations: {Source: SourceOpenConversations, Items: len(offered), Hidden: offeredHidden, Cut: len(candidates) < len(s.Candidates) && on(SourceOpenConversations)},
		SourceContact:           {Source: SourceContact},
	}
	if contact != nil {
		manifest[SourceContact].Items = 1
	}
	size := func() int {
		return runes(incoming) + runes(window) + runes(offered) + utf8.RuneCountInString(contactLine)
	}
	if size() > budget.Total && contactLine != "" {
		contactLine, manifest[SourceContact].Cut = "", true
	}
	for size() > budget.Total && len(window) > 0 {
		window, manifest[SourceThreadWindow].Cut = window[1:], true
	}
	for size() > budget.Total && len(offered) > 0 {
		offered, manifest[SourceOpenConversations].Cut = offered[:len(offered)-1], true
		label := fmt.Sprintf("C%d", len(offered)+1)
		delete(out.candidates, label)
	}
	settle := func(lines []string) {
		for i, text := range lines {
			lines[i] = conversationRef.ReplaceAllStringFunc(text, func(found string) string {
				if label := strings.Trim(strings.TrimRight(found, " "), "\x01"); out.candidates[label] != "" {
					return " in " + label
				}
				return ""
			})
		}
	}
	settle(window)
	settle(incoming)
	if over := size() - budget.Total; over > 0 {
		incoming = clipLines(incoming, max(runes(incoming)-over, budget.Incoming))
		manifest[SourceIncoming].Cut = true
	}

	var user strings.Builder
	if s.Place.OneToOne {
		user.WriteString("Place: a one-to-one chat between a customer and the team.\n")
	} else {
		user.WriteString("Place: a group chat «" + aiprovider.EscapeData(scrub(s.Place.Name)) + "».\n")
	}
	user.WriteString("<commands>\n")
	for _, command := range s.Commands {
		out.Commands = append(out.Commands, command.Key)
		user.WriteString(aiprovider.EscapeData(command.Key))
		if command.Description != "" {
			user.WriteString(" — " + aiprovider.EscapeData(oneLine(command.Description)))
		}
		user.WriteString("\n")
	}
	user.WriteString("</commands>\n<open_conversations>\n")
	if len(offered) == 0 {
		user.WriteString("(none)\n")
	}
	for _, text := range offered {
		user.WriteString(aiprovider.EscapeData(text) + "\n")
	}
	user.WriteString("</open_conversations>\n")
	if contactLine != "" {
		user.WriteString("<contact>\n" + aiprovider.EscapeData(contactLine) + "\n</contact>\n")
	}
	user.WriteString("<conversation>\n")
	for _, text := range slices.Concat(window, incoming) {
		user.WriteString(text + "\n")
	}
	user.WriteString("</conversation>\n")
	fmt.Fprintf(&user, "Decide what to do with the messages marked >>, from %s.", sender)
	out.User = user.String()

	manifest[SourceIncoming].Runes = runes(incoming)
	manifest[SourceThreadWindow].Runes, manifest[SourceThreadWindow].Items = runes(window), len(window)
	manifest[SourceOpenConversations].Runes, manifest[SourceOpenConversations].Items = runes(offered), len(offered)
	manifest[SourceContact].Runes = utf8.RuneCountInString(contactLine)
	if contactLine == "" {
		manifest[SourceContact].Items = 0
	}
	for _, source := range []string{SourceIncoming, SourceThreadWindow, SourceOpenConversations, SourceContact} {
		entry := *manifest[source]
		if source != SourceIncoming && !on(source) {
			entry = SourceManifest{Source: source, Skipped: "off"}
		} else if why := s.Skipped[source]; why != "" && entry.Items == 0 {
			entry.Skipped = why
		}
		out.Manifest = append(out.Manifest, entry)
	}

	// Pictures that go with a text about credentials may show one, which no filter can read; they are
	// not sent. A credential that had to be hidden counts as talk about credentials.
	hinted := slices.ContainsFunc(s.Incoming, func(item Item) bool {
		return redact.hints(item.Text) || item.Quote != nil && redact.hints(item.Quote.Text)
	})
	images := SourceManifest{Source: SourceImages, Skipped: s.Skipped[SourceImages]}
	if !on(SourceImages) {
		images.Skipped = "off"
	} else if out.Credential || hinted {
		out.ImagesWithheld = len(s.Images)
		if out.ImagesWithheld > 0 {
			images.Skipped = "credential"
		}
	} else {
		for _, image := range s.Images {
			if len(out.Images) < aiprovider.MaxImages && image.Sendable() {
				out.Images = append(out.Images, image)
			}
		}
	}
	images.Items = len(out.Images)
	out.Manifest = append(out.Manifest, images)
	out.System = system(s.Locale, s.Instructions)
	return out
}

// redactBySender hides credentials across each run of consecutive messages of one sender, then puts
// labels in place of names and masks contact identifiers. hid says which messages had one hidden.
func redactBySender(r *redactor, items []Item, scrub func(string) string) (bodies []string, hid []bool) {
	bodies, hid = make([]string, len(items)), make([]bool, len(items))
	for start := 0; start < len(items); {
		end := start + 1
		for end < len(items) && items[end].From.Key == items[start].From.Key && items[end].From.Team == items[start].From.Team {
			end++
		}
		texts := make([]string, 0, end-start)
		for _, item := range items[start:end] {
			texts = append(texts, item.Text)
		}
		redacted, changed := r.sequence(texts)
		for i, item := range items[start:end] {
			// A reply may hold the password to the login it quotes.
			if item.Quote == nil {
				continue
			}
			if merged := withQuotePairing(r, item.Quote.Text, texts[i], redacted[i]); merged != redacted[i] {
				redacted[i], changed[i] = merged, true
			}
		}
		for i, body := range redacted {
			bodies[start+i] = scrub(body)
		}
		copy(hid[start:end], changed)
		start = end
	}
	return bodies, hid
}

// withQuotePairing adds to redacted, the reply as hidden among its sender's other messages, what is
// hidden when the reply is read right after the message it quotes. Both are made from the original
// text line by line, so no line is hidden twice.
func withQuotePairing(r *redactor, quoted, original, redacted string) string {
	paired, _ := r.sequence([]string{quoted, original})
	original = strings.ReplaceAll(original, "\x00", "")
	lines, pairedLines, originalLines := strings.Split(redacted, "\n"), strings.Split(paired[1], "\n"), strings.Split(original, "\n")
	if len(lines) != len(pairedLines) || len(lines) != len(originalLines) {
		return redacted
	}
	for i := range lines {
		if lines[i] == originalLines[i] {
			lines[i] = pairedLines[i]
		}
	}
	return strings.Join(lines, "\n")
}

// quoteMarker renders the message a reply answers around its cleaned body.
func quoteMarker(q *Quote, label, body, in string, location *time.Location, limit int) string {
	parts := markers(q.Media)
	if body != "" {
		parts = append(parts, body)
	}
	marker := fmt.Sprintf("[reply to %s's message of %s%s", label, q.At.In(location).Format(timeLayout), in)
	if len(parts) > 0 {
		marker += ": «" + clipTo(strings.Join(parts, " "), limit) + "»"
	}
	return marker + "]"
}

func markers(media []string) []string {
	var out []string
	for _, kind := range media {
		if marker, ok := mediaMarkers[kind]; ok {
			out = append(out, marker)
		}
	}
	return out
}

// placeholders are what stands for hidden or masked text.
var placeholders = []string{hidden, phonePlaceholder, emailPlaceholder, idPlaceholder, accountPlaceholder}

// hiddenCount counts what was hidden or masked in a line; a credential hidden counts once at least.
func hiddenCount(text string, credential bool) int {
	n := 0
	for _, placeholder := range placeholders {
		n += strings.Count(text, placeholder)
	}
	if credential && n == 0 {
		n = 1
	}
	return n
}

func runes(lines []string) int {
	n := 0
	for _, line := range lines {
		n += utf8.RuneCountInString(line) + 1
	}
	return n
}

// clipLines shortens the last lines first until they fit limit runes in all, each kept line at least
// its time and author.
func clipLines(lines []string, limit int) []string {
	out := slices.Clone(lines)
	for i := len(out) - 1; i >= 0 && runes(out) > limit; i-- {
		over := runes(out) - limit
		keep := max(utf8.RuneCountInString(out[i])-over-1, min(40, utf8.RuneCountInString(out[i])))
		out[i] = clipTo(out[i], keep)
	}
	return out
}

func oneLine(text string) string {
	return strings.Join(strings.Fields(strings.ReplaceAll(text, "\n", " ")), " ")
}

// clipTo cuts text to limit characters and marks the cut, never in the middle of a placeholder.
func clipTo(text string, limit int) string {
	runes := []rune(text)
	if len(runes) <= limit {
		return text
	}
	kept := string(runes[:limit])
	if open := strings.LastIndex(kept, "["); open > strings.LastIndex(kept, "]") && strings.Contains(string(runes[limit:]), "]") {
		kept = strings.TrimRight(kept[:open], " ")
	}
	return kept + "…"
}

func orDefault(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}
