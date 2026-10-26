package prompt

import (
	"regexp"
	"slices"
	"strconv"
	"strings"
	"unicode"
)

// Hiding credentials before anything reaches a model (docs/internals/intake.md "What AI reads"). The
// frame knows the shapes of credentials and the English words around them; the words of a channel's
// language are data (Options.RedactKeywords), so nothing here names a language a channel speaks.

// hidden replaces whatever is hidden from a message.
const hidden = "[hidden]"

// credentialWords name a secret that follows them; the longer spelling comes first so that it wins.
var credentialWords = []string{"verification code", "security code", "one-time code", "password", "passcode", "passwd", "pass", "pwd", "pin", "otp"}

// otpWords say the digits on their line are a one-time code.
var otpWords = []string{"otp", "verification code", "security code", "one-time", "login code", "2fa"}

// hintWords are what people say around credentials: pictures sent with them may show one.
var hintWords = []string{"otp", "password", "passcode", "pin", "login", "log in", "sign in", "2fa"}

var (
	urlPattern   = regexp.MustCompile(`https?://\S+`)
	emailPattern = regexp.MustCompile(`[^\s@/|,;]+@[^\s@/|,;]+\.[^\s@/|,;]+`)
	// Two words around a slash, bar, comma or semicolon: a user name and a password.
	userPasswordPattern = regexp.MustCompile(`^\s*([^\s/|,;]+)\s*[/|,;]\s*(\S+)\s*$`)
	// Query values of a link that carry a secret.
	linkSecretPattern = regexp.MustCompile(`(?i)([?&;](?:token|access_token|api_key|apikey|key|secret|pass|password|code|sig|signature|auth|session|otp)=)([^&#\s]+)`)
	// Credentials with a recognisable shape.
	tokenPatterns = []*regexp.Regexp{
		regexp.MustCompile(`(?i)\bbearer\s+[A-Za-z0-9._~+/=\-]{8,}`),
		regexp.MustCompile(`\beyJ[A-Za-z0-9_\-]{5,}\.[A-Za-z0-9_\-]+\.[A-Za-z0-9_\-]+`),
		regexp.MustCompile(`\b(?:sk|pk|rk)-[A-Za-z0-9_\-]{16,}`),
		regexp.MustCompile(`\bgh[pousr]_[A-Za-z0-9]{20,}`),
		regexp.MustCompile(`\bxox[abprs]-[A-Za-z0-9\-]{10,}`),
		regexp.MustCompile(`\bAKIA[0-9A-Z]{16}\b`),
	}
	linkHolderPattern = regexp.MustCompile("\x00(\\d+)\x00")
	// Digits in groups of up to four, separated by spaces or dashes.
	groupedDigits    = regexp.MustCompile(`^\d{1,4}(?:[ -]\d{1,4})+`)
	longTokenPattern = regexp.MustCompile(`[A-Za-z0-9_\-]{32,}`)
	ticketRefPattern = regexp.MustCompile(`^[A-Z]{2,8}(-[0-9]{1,8}){1,2}$`)
	// An amount: grouped digits with an optional currency mark or code ("1.500.000đ", "315,000 USD"), or
	// plain digits with a currency mark or code. Plain digits and a "k" stay password-shaped: "123456k" is
	// more often a password than an amount.
	moneyPattern    = regexp.MustCompile(`(?i)^(?:\d{1,3}(?:[.,]\d{3})+\s*(?:\p{L}{1,3}|\p{Sc})?|\d+\s*(?:\p{Sc}|đ|usd|eur|gbp|jpy|cny|inr|aud|cad|sgd|thb|idr|php|vnd|vnđ))$`)
	userNamePattern = regexp.MustCompile(`^\p{L}[\p{L}\p{N}._-]{1,39}$`)
	// A line that is only a user name: a word, no spaces.
	userLinePattern   = regexp.MustCompile(`^\p{L}[\p{L}\p{N}._-]{2,29}$`)
	passwordSymbols   = "!@#$%^&*_-+=.?"
	inlineSymbols     = "!@#$%^&*"
	maxPasswordLength = 40
)

// redactor hides credentials with the frame's words and a channel's own.
type redactor struct {
	labelPattern *regexp.Regexp
	labelWord    *regexp.Regexp
	hintPattern  *regexp.Regexp
	otpWords     []string
	// labelOnly is a line that is a credential label and nothing after it ("password:", a label and a
	// few words); beforeLabel is a value a few plain words before a label near the end of the sentence.
	labelOnly   *regexp.Regexp
	beforeLabel *regexp.Regexp
}

// newRedactor builds the patterns from the frame's words and the channel's keywords (words of its
// language that name a password, a PIN or a code, or that people say around them).
func newRedactor(keywords []string) *redactor {
	labels := append(slices.Clone(credentialWords), keywords...)
	hints := append(slices.Clone(hintWords), keywords...)
	otp := append(slices.Clone(otpWords), lowerAll(keywords)...)
	slices.SortStableFunc(labels, func(a, b string) int { return len(b) - len(a) })
	alternatives := quoteAll(labels)
	return &redactor{
		// A credential label, up to three words of description, an optional ':' or '=' and the value.
		labelPattern: regexp.MustCompile(`(?i)(` + alternatives + `)((?:\s+\p{L}+){0,3})([\s\p{Z}]*[:：=\-–—][\s\p{Z}]*|[\s\p{Z}]+)(\S+)`),
		labelWord:    regexp.MustCompile(`(?i)^(?:` + alternatives + `)$`),
		hintPattern:  regexp.MustCompile(`(?i)(?:^|[^\p{L}\p{N}])(?:` + quoteAll(hints) + `)(?:[^\p{L}\p{N}]|$)`),
		otpWords:     otp,
		labelOnly:    regexp.MustCompile(`(?i)^\s*(?:\p{L}+\s+){0,2}(?:` + alternatives + `)(?:\s+\p{L}+){0,3}\s*[:=]?\s*$`),
		beforeLabel:  regexp.MustCompile(`(?i)(^|\s)(\S+)((?:\s+\p{L}+){0,3}\s+(?:` + alternatives + `)(?:\s+\p{L}+){0,3}\s*[.!?]?\s*)$`),
	}
}

func quoteAll(words []string) string {
	quoted := make([]string, 0, len(words))
	for _, word := range words {
		if word = strings.TrimSpace(word); word != "" {
			quoted = append(quoted, regexp.QuoteMeta(word))
		}
	}
	return strings.Join(quoted, "|")
}

func lowerAll(words []string) []string {
	out := make([]string, len(words))
	for i, word := range words {
		out[i] = strings.ToLower(word)
	}
	return out
}

// hints reports whether a text talks about credentials.
func (r *redactor) hints(text string) bool { return r.hintPattern.MatchString(text) }

// sequence hides credentials in consecutive messages of one sender, where a login and its password may
// arrive as separate messages: each message is one or more lines of a single text. It returns one text
// per message and which of them it changed.
func (r *redactor) sequence(texts []string) ([]string, []bool) {
	out := make([]string, len(texts))
	changed := make([]bool, len(texts))
	var lines []string
	var owner []int
	for message, text := range texts {
		text = strings.ReplaceAll(text, "\x00", "")
		out[message] = text
		if strings.TrimSpace(text) == "" {
			continue
		}
		for _, line := range strings.Split(text, "\n") {
			lines = append(lines, line)
			owner = append(owner, message)
		}
	}
	lineChanged := r.lines(lines)
	rebuilt := make([][]string, len(texts))
	for i, line := range lines {
		rebuilt[owner[i]] = append(rebuilt[owner[i]], line)
		changed[owner[i]] = changed[owner[i]] || lineChanged[i]
	}
	for message, parts := range rebuilt {
		if parts != nil {
			out[message] = strings.Join(parts, "\n")
		}
	}
	return out, changed
}

type lineKind int

const (
	plainLine lineKind = iota
	labelledPassword
	wholeLine
	// loginLine is a whole line that held a user name and its password.
	loginLine
)

// lines hides credentials in place and reports which lines it changed: a password after its label, a
// login and password on one line, a password on the line after an email address or a user name, and
// the passwords that follow (an old and a new one).
func (r *redactor) lines(lines []string) []bool {
	label := make([]bool, len(lines))
	whole := make([]bool, len(lines))
	login := make([]bool, len(lines))
	changed := make([]bool, len(lines))
	opened := make([]bool, len(lines))
	for i, line := range lines {
		opened[i] = r.labelOnly.MatchString(line)
		out, kind := r.line(line)
		if out != line {
			changed[i] = true
			lines[i] = out
		}
		whole[i] = kind == wholeLine || kind == loginLine
		label[i] = kind == labelledPassword
		login[i] = kind == loginLine
	}
	for i := 0; i < len(lines); i++ {
		if login[i] {
			hideMorePasswords(lines, whole, changed, i)
			continue
		}
		next := nextContentLine(lines, i)
		if next < 0 {
			break
		}
		// A label on its own opens the next line: "password:", then the password.
		if opened[i] && !whole[next] {
			if rest := strings.TrimSpace(lines[next]); bareSecret(rest) || allDigits(rest) && len(rest) >= 4 && len(rest) <= 12 {
				lines[next], whole[next], changed[next] = hidden, true, true
				hideMorePasswords(lines, whole, changed, next)
				continue
			}
		}
		if whole[i] || whole[next] {
			continue
		}
		user, secret := lines[i], lines[next]
		pairsWithEmail := emailPattern.MatchString(user) && (label[next] || bareSecret(secret))
		pairsWithUser := userLinePattern.MatchString(strings.TrimSpace(user)) && !ticketRefPattern.MatchString(strings.TrimSpace(user)) && bareSecret(secret)
		if !pairsWithEmail && !pairsWithUser {
			continue
		}
		lines[i], changed[i] = hidden, true
		if !label[next] {
			lines[next], whole[next], changed[next] = hidden, true, true
		}
		hideMorePasswords(lines, whole, changed, next)
	}
	return changed
}

// hideMorePasswords hides the lines shaped like a password that follow line i, up to the first line
// that is neither one nor a lone link.
func hideMorePasswords(lines []string, whole, changed []bool, i int) {
	for k := nextContentLine(lines, i); k >= 0 && !whole[k]; k = nextContentLine(lines, k) {
		rest := strings.TrimSpace(lines[k])
		if rest != "" && urlPattern.FindString(rest) == rest {
			continue
		}
		if !bareSecret(rest) {
			return
		}
		lines[k], whole[k], changed[k] = hidden, true, true
	}
}

// nextContentLine is the index of the first line after line i that holds text, or -1.
func nextContentLine(lines []string, i int) int {
	next := slices.IndexFunc(lines[i+1:], func(line string) bool { return strings.TrimSpace(line) != "" })
	if next < 0 {
		return -1
	}
	return next + i + 1
}

func (r *redactor) line(line string) (string, lineKind) {
	masked, links := maskLinks(hideUserinfo(line))
	if m := userPasswordPattern.FindStringSubmatch(masked); m != nil && userName(m[1]) && passwordShaped(m[2]) {
		return hidden, loginLine
	}
	if r.spacedLogin(masked) {
		return hidden, loginLine
	}
	kind := plainLine
	masked = r.hideBeforeLabel(masked, &kind)
	masked = r.hideLabelled(masked, &kind)
	masked = r.hideCodes(masked, &kind)
	for _, pattern := range tokenPatterns {
		masked = pattern.ReplaceAllString(masked, hidden)
	}
	masked = longTokenPattern.ReplaceAllStringFunc(masked, func(token string) string {
		if hasLetter(token) && hasDigit(token) {
			return hidden
		}
		return token
	})
	return restoreLinks(masked, links), kind
}

// spacedLogin is a line of exactly two words, a user name or email and a password, with nothing but a
// space between them. Two ordinary words are far more common, so the second must look like a password.
func (r *redactor) spacedLogin(line string) bool {
	words := strings.Fields(line)
	if len(words) != 2 || r.labelWord.MatchString(words[0]) || !userName(words[0]) {
		return false
	}
	return mixedSecret(words[1], 8)
}

// mixedSecret is a word shaped like a password: letters, a digit and an inline symbol, or at least min
// characters of upper case, lower case and digits only.
func mixedSecret(word string, min int) bool {
	if !passwordShaped(word) || emailPattern.MatchString(word) {
		return false
	}
	core := strings.Trim(word, inlineSymbols)
	if hasLetter(core) && hasDigit(core) && strings.ContainsAny(core, inlineSymbols) {
		return true
	}
	return len(word) >= min && hasDigit(core) && strings.IndexFunc(core, unicode.IsUpper) >= 0 && strings.IndexFunc(core, unicode.IsLower) >= 0 &&
		strings.IndexFunc(core, func(r rune) bool { return !isWordRune(r) }) < 0
}

func userName(token string) bool {
	if emailPattern.MatchString(token) {
		return true
	}
	return userNamePattern.MatchString(token) && !ticketRefPattern.MatchString(token)
}

// hideLabelled hides the value after a credential label. After ':' or '=' any value counts; without
// one the value has to look like a secret, so "password is wrong" stays.
func (r *redactor) hideLabelled(line string, kind *lineKind) string {
	var out strings.Builder
	last := 0
	for _, m := range r.labelPattern.FindAllStringSubmatchIndex(line, -1) {
		if m[0] > 0 && isWordRune(lastRune(line[:m[0]])) {
			continue
		}
		separator := line[m[6]:m[7]]
		start, end := m[8], m[9]
		value := strings.TrimRight(line[start:end], ".,;)!?")
		end = start + len(value)
		explicit := strings.ContainsAny(separator, ":：=")
		// A code or a PIN is often written in groups: "12 34 56", "123-456".
		if run := groupedDigits.FindString(line[start:]); run != "" && digitCount(run) >= 4 && digitCount(run) <= 12 {
			value, end = run, start+len(run)
		}
		switch {
		case value == "":
			continue
		case explicit:
		case allDigits(strings.NewReplacer(" ", "", "-", "").Replace(value)) && digitCount(value) >= 4 && digitCount(value) <= 12:
		case len(value) >= 4 && hasLetter(value) && hasDigitOrSymbol(value):
		default:
			// "my password for my account is Hunter2024": the secret may come a few plain words later.
			var found bool
			if start, end, found = secretAfter(line, start); !found {
				continue
			}
		}
		out.WriteString(line[last:start])
		out.WriteString(hidden)
		last = end
		*kind = labelledPassword
	}
	out.WriteString(line[last:])
	return out.String()
}

// labelReach is how many words after a credential label's first value a secret is still looked for.
const labelReach = 4

// secretAfter finds, from from on, the first word shaped like a secret among the next labelReach words
// of the sentence; plain words may stand before it, anything else ends the search.
func secretAfter(line string, from int) (int, int, bool) {
	for _, span := range wordPattern.FindAllStringIndex(line[from:], labelReach+1) {
		start, end := from+span[0], from+span[1]
		word := strings.Trim(line[start:end], quoteEdges)
		switch {
		case passwordShaped(word) || allDigits(word) && len(word) >= 4 && len(word) <= 12:
			offset := strings.Index(line[start:end], word)
			return start + offset, start + offset + len(word), true
		case strings.IndexFunc(word, func(r rune) bool { return !unicode.IsLetter(r) }) >= 0,
			strings.ContainsAny(line[start:end], ".!?;"):
			return 0, 0, false
		}
	}
	return 0, 0, false
}

// hideBeforeLabel hides a password written before its label: "Abc@12345 is my password (of the shop)".
func (r *redactor) hideBeforeLabel(line string, kind *lineKind) string {
	m := r.beforeLabel.FindStringSubmatchIndex(line)
	if m == nil {
		return line
	}
	value := strings.Trim(line[m[4]:m[5]], quoteEdges)
	if !passwordShaped(value) || emailPattern.MatchString(value) || urlPattern.MatchString(value) {
		return line
	}
	*kind = labelledPassword
	return line[:m[4]] + hidden + line[m[5]:]
}

// hideCodes hides a one-time code: a 4 to 8 digit number on a line that names a code, or a line that
// holds nothing but a 6 digit number.
func (r *redactor) hideCodes(line string, kind *lineKind) string {
	lower := strings.ToLower(line)
	named := false
	for _, word := range r.otpWords {
		named = named || (word != "" && strings.Contains(lower, word))
	}
	trimmed := strings.TrimFunc(line, func(c rune) bool { return !unicode.IsDigit(c) && !unicode.IsLetter(c) })
	if !named && (len(trimmed) != 6 || !allDigits(trimmed)) {
		return line
	}
	var out strings.Builder
	runes := []rune(line)
	for i := 0; i < len(runes); {
		if !unicode.IsDigit(runes[i]) {
			out.WriteRune(runes[i])
			i++
			continue
		}
		j := i
		for j < len(runes) && unicode.IsDigit(runes[j]) {
			j++
		}
		if length := j - i; length >= 4 && length <= 8 && standsAlone(runes, i, j) {
			out.WriteString(hidden)
			if !named {
				*kind = wholeLine
			}
		} else {
			out.WriteString(string(runes[i:j]))
		}
		i = j
	}
	return out.String()
}

// standsAlone is false for digits that belong to a longer figure: a decimal, a date, a range.
func standsAlone(runes []rune, start, end int) bool {
	if start > 0 && strings.ContainsRune(".,/-", runes[start-1]) {
		return false
	}
	if end < len(runes) && strings.ContainsRune("/-", runes[end]) {
		return false
	}
	return end+1 >= len(runes) || !strings.ContainsRune(".,", runes[end]) || !unicode.IsDigit(runes[end+1])
}

// bareSecret is a line that is nothing but something shaped like a password; a link is not one.
func bareSecret(line string) bool {
	line = strings.TrimSpace(line)
	return passwordShaped(line) && !emailPattern.MatchString(line) && !ticketRefPattern.MatchString(line) && !urlPattern.MatchString(line)
}

func passwordShaped(token string) bool {
	return len(token) >= 6 && len(token) <= maxPasswordLength && !strings.ContainsAny(token, " \t") &&
		hasLetter(token) && hasDigitOrSymbol(token) && !strings.Contains(token, "\x00") && !ticketRefPattern.MatchString(token) && !moneyPattern.MatchString(token)
}

// secretShapes hides, in a text read alone (a quoted message), a line that is nothing but a password or
// a login, and a word of a sentence shaped like one. Text hidden already is left as it is.
func (r *redactor) secretShapes(text string) string {
	pieces := strings.Split(text, hidden)
	for p, piece := range pieces {
		lines := strings.Split(piece, "\n")
		for l, line := range lines {
			masked, links := maskLinks(line)
			if bareSecret(masked) || r.spacedLogin(masked) {
				lines[l] = hidden
				continue
			}
			masked = wordPattern.ReplaceAllStringFunc(masked, func(word string) string {
				if mixedSecret(strings.Trim(word, quoteEdges), 9) {
					return hidden
				}
				return word
			})
			lines[l] = restoreLinks(masked, links)
		}
		pieces[p] = strings.Join(lines, "\n")
	}
	return strings.Join(pieces, hidden)
}

var wordPattern = regexp.MustCompile(`\S+`)

// quoteEdges are the marks that may stand around a word without being part of it.
const quoteEdges = ".,;:!?()\"'«»[]"

func hasDigitOrSymbol(s string) bool { return hasDigit(s) || strings.ContainsAny(s, passwordSymbols) }

func digitCount(s string) int {
	n := 0
	for _, r := range s {
		if unicode.IsDigit(r) {
			n++
		}
	}
	return n
}

func hasDigit(s string) bool  { return strings.IndexFunc(s, unicode.IsDigit) >= 0 }
func hasLetter(s string) bool { return strings.IndexFunc(s, unicode.IsLetter) >= 0 }
func allDigits(s string) bool {
	return strings.IndexFunc(s, func(r rune) bool { return !unicode.IsDigit(r) }) < 0
}
func isWordRune(r rune) bool { return unicode.IsLetter(r) || unicode.IsDigit(r) }

func lastRune(s string) rune {
	runes := []rune(s)
	return runes[len(runes)-1]
}

// maskLinks swaps every link for a placeholder after hiding the secrets a link can carry, so what a
// link says is never mistaken for a line of the message.
func maskLinks(line string) (string, []string) {
	var links []string
	masked := urlPattern.ReplaceAllStringFunc(line, func(link string) string {
		link = linkSecretPattern.ReplaceAllString(link, "${1}"+hidden)
		for _, pattern := range tokenPatterns {
			link = pattern.ReplaceAllString(link, hidden)
		}
		links = append(links, link)
		return "\x00" + strconv.Itoa(len(links)-1) + "\x00"
	})
	return masked, links
}

func restoreLinks(line string, links []string) string {
	return linkHolderPattern.ReplaceAllStringFunc(line, func(match string) string {
		index, err := strconv.Atoi(strings.Trim(match, "\x00"))
		if err != nil || index < 0 || index >= len(links) {
			return ""
		}
		return links[index]
	})
}
