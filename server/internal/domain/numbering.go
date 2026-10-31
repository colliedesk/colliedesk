package domain

import (
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"unicode"
)

// A number format says how a conversation's number is shown and typed back: literal text with {n} (the
// number), {n:0K} (padded to K digits, 2 to 8) and {yyyy} (the year it was opened, in the workspace's
// timezone). "#{n}" is the default series'.
type NumberFormat struct {
	parts []formatPart
}

type formatPart struct {
	literal string
	number  bool
	pad     int
	year    bool
}

// DefaultNumberFormat is how the default series shows its numbers.
const DefaultNumberFormat = "#{n}"

var formatToken = regexp.MustCompile(`\{(n|n:0[2-8]|yyyy)\}`)

// ErrNumberFormat refuses a format: say what it may hold.
var ErrNumberFormat = errors.New("use letters, digits, # - _ / . and {n} once, with {n:04} for padding (2 to 8) and {yyyy} for the year")

// ParseNumberFormat reads a format; it holds {n} (or {n:0K}) exactly once, at most one {yyyy}, and
// literal letters, digits and # - _ / . between, 40 characters at most.
func ParseNumberFormat(format string) (NumberFormat, error) {
	if format == "" || len(format) > 40 {
		return NumberFormat{}, ErrNumberFormat
	}
	var parsed NumberFormat
	numbers, years, at := 0, 0, 0
	for _, match := range formatToken.FindAllStringSubmatchIndex(format, -1) {
		if err := parsed.literal(format[at:match[0]]); err != nil {
			return NumberFormat{}, err
		}
		switch token := format[match[2]:match[3]]; {
		case token == "yyyy":
			years++
			parsed.parts = append(parsed.parts, formatPart{year: true})
		default:
			numbers++
			pad := 0
			if strings.HasPrefix(token, "n:0") {
				pad, _ = strconv.Atoi(token[3:])
			}
			parsed.parts = append(parsed.parts, formatPart{number: true, pad: pad})
		}
		at = match[1]
	}
	if err := parsed.literal(format[at:]); err != nil {
		return NumberFormat{}, err
	}
	if numbers != 1 || years > 1 {
		return NumberFormat{}, ErrNumberFormat
	}
	return parsed, nil
}

func (f *NumberFormat) literal(text string) error {
	for _, r := range text {
		if !(r < unicode.MaxASCII && (unicode.IsLetter(r) || unicode.IsDigit(r) || strings.ContainsRune("#-_/.", r))) {
			return ErrNumberFormat
		}
	}
	if text != "" {
		f.parts = append(f.parts, formatPart{literal: text})
	}
	return nil
}

// HasYear says whether the code shows the year a conversation was opened.
func (f NumberFormat) HasYear() bool {
	for _, part := range f.parts {
		if part.year {
			return true
		}
	}
	return false
}

// Render is the code of number n opened in year.
func (f NumberFormat) Render(n int64, year int) string {
	var code strings.Builder
	for _, part := range f.parts {
		switch {
		case part.number:
			fmt.Fprintf(&code, "%0*d", part.pad, n)
		case part.year:
			fmt.Fprintf(&code, "%04d", year)
		default:
			code.WriteString(part.literal)
		}
	}
	return code.String()
}
