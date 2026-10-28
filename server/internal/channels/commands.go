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

// MatchCommand finds the command a message starts with: a phrase of a command matches the first
// words of the text when folded the same (FoldText), or, for a phrase of five letters or more, when
// one letter is wrong, missing, added or two are swapped. The longest phrase wins. rest is the text
// after the phrase.
func MatchCommand(text string, commands map[string]Command) (name string, command Command, rest string, ok bool) {
	words := strings.Fields(FoldText(text))
	original := strings.Fields(text)
	names := make([]string, 0, len(commands))
	for candidate := range commands {
		names = append(names, candidate)
	}
	sort.Strings(names)
	best := 0
	for _, candidate := range names {
		for _, phrase := range commands[candidate].Phrases {
			wanted := strings.Fields(FoldText(phrase))
			if len(wanted) == 0 || len(wanted) > len(words) {
				continue
			}
			got := strings.Join(words[:len(wanted)], " ")
			joined := strings.Join(wanted, " ")
			if got != joined && (len([]rune(joined)) < 5 || damerau(got, joined) > 1) {
				continue
			}
			if len(joined) > best {
				best, name, command, ok = len(joined), candidate, commands[candidate], true
				if len(wanted) <= len(original) {
					rest = strings.Join(original[len(wanted):], " ")
				}
			}
		}
	}
	if ok && command.Action == "" {
		command.Action = ActionOpen
	}
	return name, command, rest, ok
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
