// Package textmask takes contact details out of a short text a reader leaves on a public page, before
// it is stored: email addresses, and every run of six digits or more (phone, order, account and card
// numbers), whatever separates the digits. It is stricter than what masks a fix for GitHub
// (internal/autofix): a reader's line has no reason to hold a long number, so none is kept.
package textmask

import (
	"regexp"
	"unicode"
)

// The words a masked part reads as; neither holds a digit or an @, so masking again leaves them.
const (
	emailRemoved  = "[email removed]"
	numberRemoved = "[number removed]"
)

var (
	// An address with any letters, the full-width at sign included.
	emailPattern = regexp.MustCompile(`[\p{L}\p{N}._%+\-]+[@＠][\p{L}\p{N}\-]+(?:\.[\p{L}\p{N}\-]+)+`)
	// Digits (any script's) joined by spaces, dots, dashes, slashes or brackets.
	numberPattern = regexp.MustCompile(`\+?\p{Nd}(?:[ ().\-/]*\p{Nd})+`)
)

// minDigits is the shortest run of digits that is taken out.
const minDigits = 6

// Mask is text without email addresses and runs of six digits or more. Masking twice gives what
// masking once gave.
func Mask(text string) string {
	text = emailPattern.ReplaceAllLiteralString(text, emailRemoved)
	return numberPattern.ReplaceAllStringFunc(text, func(run string) string {
		digits := 0
		for _, r := range run {
			if unicode.IsDigit(r) {
				digits++
			}
		}
		if digits < minDigits {
			return run
		}
		return numberRemoved
	})
}
