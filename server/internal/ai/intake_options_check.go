package ai

import "encoding/json"

// CheckIntakeOptions checks intake's options as a client sends them, as saving them would: a field it
// does not know or a value out of bounds is refused with why. An agent request carries only options that
// pass, so a person never approves a call that cannot run.
func CheckIntakeOptions(raw json.RawMessage) error {
	_, err := parseIntakeOptions(raw)
	return err
}
