package channels

import (
	"sort"
	"strings"
	"unicode"

	"golang.org/x/text/unicode/norm"
)

// fold maps the letters canonical decomposition leaves whole to the letters people type without
// them. It is one table for every language, not a choice per country.
var fold = map[rune]string{
	'đ': "d", 'ð': "d", 'ł': "l", 'ŀ': "l", 'ø': "o", 'æ': "ae", 'œ': "oe", 'ß': "ss", 'þ': "th",
	'ı': "i", 'ħ': "h", 'ŧ': "t", 'ŋ': "n", 'ĸ': "k",
}

// FoldText is text as commands compare it: lower case, without accents or marks, with the letters of
// fold replaced, and with runs of spaces and punctuation as one space.
func FoldText(text string) string {
	var b strings.Builder
	space := true
	for _, r := range norm.NFD.String(strings.ToLower(text)) {
		switch {
		case unicode.Is(unicode.Mn, r):
			continue
		case unicode.IsLetter(r) || unicode.IsDigit(r):
			if replaced, ok := fold[r]; ok {
				b.WriteString(replaced)
			} else {
				b.WriteRune(r)
			}
			space = false
		case !space:
			b.WriteByte(' ')
			space = true
		}
	}
	return strings.TrimSpace(b.String())
}

// MatchCommand finds the command a message starts with, as each command's Match says: a phrase matches
// the first words of the text when folded the same (FoldText, keeping a # or / that starts a word of the
// phrase), or with MatchFuzzy, for a phrase of five letters or more, when one letter is wrong, missing,
// added or two are swapped. With MatchExact a phrase is the whole message. The longest phrase wins. rest
// is the customer's own text after the phrase, without the punctuation that joins them.
func MatchCommand(text string, commands map[string]Command) (name string, command Command, rest string, ok bool) {
	words := commandWords(text)
	whole := norm.NFC.String(strings.TrimSpace(text))
	names := make([]string, 0, len(commands))
	for candidate := range commands {
		names = append(names, candidate)
	}
	sort.Strings(names)
	best := 0
	for _, candidate := range names {
		match := commands[candidate].Match
		for _, phrase := range commands[candidate].Phrases {
			if match == MatchExact {
				phrase = norm.NFC.String(strings.TrimSpace(phrase))
				if phrase != "" && strings.EqualFold(whole, phrase) && len(phrase) > best {
					best, name, command, rest, ok = len(phrase), candidate, commands[candidate], "", true
				}
				continue
			}
			wanted := commandWords(phrase)
			joined := strings.Join(texts(wanted), " ")
			used := phraseWords(texts(words), texts(wanted), match)
			if used == 0 || len(joined) <= best {
				continue
			}
			best, name, command, ok = len(joined), candidate, commands[candidate], true
			rest = strings.TrimLeft(text[words[used-1].end:], restSeparators)
		}
	}
	if ok && command.Action == "" {
		command.Action = ActionOpen
	}
	return name, command, rest, ok
}

// restSeparators are what people type between a command and what they say with it.
const restSeparators = " \t\r\n:,.;-–—"

// phraseWords is how many of the text's first words the phrase takes, 0 when it does not match. A # or
// / before the text's first word counts only when the phrase has it.
func phraseWords(words, wanted []string, match string) int {
	if len(wanted) == 0 || len(words) == 0 {
		return 0
	}
	if !hasSigil(wanted[0]) && hasSigil(words[0]) {
		words = append([]string{words[0][1:]}, words[1:]...)
	}
	if match == MatchWords {
		// The words of the phrase, its spaces aside: "báolỗi" is "báo lỗi", "báo lỗix" is not.
		target := strings.Join(wanted, "")
		got := ""
		for used, word := range words {
			got += word
			switch {
			case got == target:
				return used + 1
			case len(got) >= len(target) || !strings.HasPrefix(target, got):
				return 0
			}
		}
		return 0
	}
	if len(wanted) > len(words) {
		return 0
	}
	got := strings.Join(words[:len(wanted)], " ")
	joined := strings.Join(wanted, " ")
	if got != joined && (len([]rune(joined)) < 5 || damerau(got, joined) > 1) {
		return 0
	}
	return len(wanted)
}

func hasSigil(word string) bool { return strings.HasPrefix(word, "#") || strings.HasPrefix(word, "/") }

// word is a folded word of a text and where it ends in the text (in bytes).
type word struct {
	text string
	end  int
}

func texts(words []word) []string {
	out := make([]string, len(words))
	for i, w := range words {
		out[i] = w.text
	}
	return out
}

// commandWords is text folded (FoldText) into words, each a run of letters and digits; a word keeps the #
// or / it starts with after a space, so a command named "/loi" is that token, not the word it spells.
func commandWords(text string) []word {
	var words []word
	runStart := -1
	sigil := ""
	flush := func(at int) {
		if runStart >= 0 {
			if folded := FoldText(text[runStart:at]); folded != "" {
				words = append(words, word{text: sigil + folded, end: at})
			}
		}
		runStart, sigil = -1, ""
	}
	previous := ' '
	for at, r := range text {
		inWord := unicode.IsLetter(r) || unicode.IsDigit(r) || unicode.Is(unicode.Mn, r)
		switch {
		case inWord && runStart < 0:
			runStart = at
			if previous == '#' || previous == '/' {
				sigil = string(previous)
			}
		case !inWord && runStart >= 0:
			flush(at)
		}
		if !inWord && (r == '#' || r == '/') && (at == 0 || unicode.IsSpace(previous)) {
			previous = r
			continue
		}
		if !inWord {
			previous = ' '
			if !unicode.IsSpace(r) {
				previous = 'x'
			}
		} else {
			previous = r
		}
	}
	flush(len(text))
	return words
}

// damerau is the optimal string alignment distance of two strings, in runes.
func damerau(a, b string) int {
	x, y := []rune(a), []rune(b)
	d := make([][]int, len(x)+1)
	for i := range d {
		d[i] = make([]int, len(y)+1)
		d[i][0] = i
	}
	for j := range d[0] {
		d[0][j] = j
	}
	for i := 1; i <= len(x); i++ {
		for j := 1; j <= len(y); j++ {
			cost := 1
			if x[i-1] == y[j-1] {
				cost = 0
			}
			d[i][j] = min(d[i-1][j]+1, d[i][j-1]+1, d[i-1][j-1]+cost)
			if i > 1 && j > 1 && x[i-1] == y[j-2] && x[i-2] == y[j-1] {
				d[i][j] = min(d[i][j], d[i-2][j-2]+1)
			}
		}
	}
	return d[len(x)][len(y)]
}
