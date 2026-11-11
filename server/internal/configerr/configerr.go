// Package configerr gives the operator console a stable code for each message the server writes about its own
// configuration: a setting a parser refuses, a live test, what a change does or why it cannot be saved. The
// English sentence stays the message every API client, the CLI and an AI agent read; the console translates by
// code (docs/internals/operator-console.md › Message codes). Params never hold a secret's value.
package configerr

import (
	"errors"
	"fmt"
	"regexp"
	"strconv"
)

// Params are the values a message names (a setting, a model, a count), strings or numbers.
type Params map[string]any

// Coded is a message's code and params as the API sends them, beside the English text.
type Coded struct {
	Code   string `json:"code"`
	Params Params `json:"params,omitempty"`
}

// Error is a configuration error with its code. Error() is the English sentence, as before codes.
type Error struct {
	Code   string
	Params Params
	Text   string
}

func (e *Error) Error() string { return e.Text }

// Coded is the error's code and params.
func (e *Error) Coded() Coded { return Coded{Code: e.Code, Params: e.Params} }

// Say is the message of code with params: its English is the code's template (Templates) filled in, so the
// English the API sends and the console's table of codes (web/src/operator/codes.en.json, written from
// Templates by TestTheConsoleTableOfCodesIsTheServersEnglish) never drift apart.
func Say(code string, params Params) *Error {
	return &Error{Code: code, Params: params, Text: Render(Templates[code], params)}
}

// placeholder is {name}, {"name"} (quoted, as %q) or {name?one|many} (the word for the number name).
var placeholder = regexp.MustCompile(`\{("?)([a-z_]+)"?(?:\?([^|}]*)\|([^}]*))?\}`)

// Render fills a template with params. web/src/operator/vi/codes-apply.ts renders the same way.
func Render(template string, params Params) string {
	return placeholder.ReplaceAllStringFunc(template, func(found string) string {
		parts := placeholder.FindStringSubmatch(found)
		value := params[parts[2]]
		if parts[3] != "" || parts[4] != "" {
			if number(value) == 1 {
				return parts[3]
			}
			return parts[4]
		}
		text := Text(value)
		if parts[1] == `"` {
			return strconv.Quote(text)
		}
		return text
	})
}

// Text is a param as the English writes it.
func Text(value any) string {
	switch v := value.(type) {
	case string:
		return v
	case int:
		return strconv.Itoa(v)
	case int64:
		return strconv.FormatInt(v, 10)
	case float64:
		return strconv.FormatFloat(v, 'f', -1, 64)
	case nil:
		return ""
	}
	return fmt.Sprint(value)
}

func number(value any) float64 {
	switch v := value.(type) {
	case int:
		return float64(v)
	case int64:
		return float64(v)
	case float64:
		return v
	}
	return 0
}

// Leaves flattens joined errors (errors.Join) into the errors they join, in order; any other error is a leaf.
func Leaves(err error) []error {
	if err == nil {
		return nil
	}
	if joined, ok := err.(interface{ Unwrap() []error }); ok {
		var out []error
		for _, inner := range joined.Unwrap() {
			out = append(out, Leaves(inner)...)
		}
		return out
	}
	return []error{err}
}

// As is the coded form of err when err says exactly what its code says: an *Error, or one wrapped without
// words of its own. A wrapper that adds words is not coded (its text says more than the code).
func As(err error) (*Error, bool) {
	var coded *Error
	if errors.As(err, &coded) && coded.Error() == err.Error() {
		return coded, true
	}
	return nil, false
}
