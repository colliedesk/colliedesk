package prompt

import (
	"slices"
	"strings"
	"testing"
)

// viKeywords are the words a Vietnamese-speaking channel would list in options.redact_keywords. The frame
// holds none of them; they reach it as data.
var viKeywords = []string{"mật khẩu", "mat khau", "mk", "mã pin", "ma pin", "mã otp", "ma otp", "mã xác thực", "mã xác nhận",
	"ma xac thuc", "ma xac nhan", "mã đăng nhập", "đăng nhập", "dang nhap"}

func redactOne(keywords []string, text string) (string, bool) {
	out, changed := newRedactor(keywords).sequence([]string{text})
	return out[0], changed[0]
}

func TestRedactHidesCredentialsAndLeavesOrdinaryText(t *testing.T) {
	tests := []struct {
		name, in, want string
		redacted       bool
	}{
		{"a lone one-time code", "123456", hidden, true},
		{"a code on the line under its label", "my OTP\n123456", "my OTP\n" + hidden, true},
		{"a code inside a service message", "Your verification code: 123456. Do not share it.", "Your verification code: " + hidden + ". Do not share it.", true},
		{"an email line followed by a password line", "store@example.com\nShop@2026", hidden + "\n" + hidden, true},
		{"an email and a password on one line", "store@example.com / Shop@2026", hidden, true},
		{"a password after its label", "password: abc12345 thanks", "password: " + hidden + " thanks", true},
		{"a password a few words after its label", "the password is Abc@12345", "the password is " + hidden, true},
		{"an API token", "my key sk-or-v1-9f8a7b6c5d4e3f2a1b0c9d8e7f6a5b4c", "my key " + hidden, true},
		{"a bearer token", "Authorization: Bearer eyJhbGciOiJIUzI1NiJ9.e30.Zm9v", "Authorization: " + hidden, true},
		{"a user and password with a slash", "admin / Abc@12345", hidden, true},
		{"a user line and a password line", "admin\nAbc@12345", hidden + "\n" + hidden, true},
		{"a pin after its label", "PIN: 1234", "PIN: " + hidden, true},
		{"a token in a link query", "open https://x.test/reset?token=abc123XYZ&lang=en please", "open https://x.test/reset?token=" + hidden + "&lang=en please", true},
		{"a code written in groups", "OTP: 12 34 56", "OTP: " + hidden, true},
		{"a user and a password on one line with a space", "tranthib Matkhau@2024", hidden, true},
		{"a ticket reference after a login and password stays", "tranthib\nAbc@12345\nABC-2026-0090", hidden + "\n" + hidden + "\nABC-2026-0090", true},
		{"a link after a login and password stays", "tranthib\nAbc@12345\nhttps://pos.example.test/login", hidden + "\n" + hidden + "\nhttps://pos.example.test/login", true},
		{"a password four plain words after its label", "my password for my account is Hunter2024", "my password for my account is " + hidden, true},
		{"a password a sentence after its label stays a sentence", "the password is wrong. order SO2026001 failed", "the password is wrong. order SO2026001 failed", false},

		{"an amount is not a code", "short 450k yesterday", "short 450k yesterday", false},
		{"an order number in a sentence", "order 123456 failed to print", "order 123456 failed to print", false},
		{"a phone number stays for the contact mask", "call 0912345678 please", "call 0912345678 please", false},
		{"a grouped amount with a currency mark", "total 1.500.000đ", "total 1.500.000đ", false},
		{"talking about a password without giving one", "the password is wrong and I cannot log in", "the password is wrong and I cannot log in", false},
		{"an email address in a sentence", "send the report to store@example.com", "send the report to store@example.com", false},
		{"a link with a long path", "see https://cdn.example.test/files/9f8a7b6c5d4e3f2a1b0c9d8e7f6a5b4c.jpg", "see https://cdn.example.test/files/9f8a7b6c5d4e3f2a1b0c9d8e7f6a5b4c.jpg", false},
		{"a ticket reference", "see ABC-2026-0083", "see ABC-2026-0083", false},
		{"an empty message", "", "", false},
		{"too few digits after a label", "pin 123 ok", "pin 123 ok", false},
		{"an order code after a word", "order SO2026001", "order SO2026001", false},
		{"a file name after a word", "see report.pdf", "see report.pdf", false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, redacted := redactOne(nil, tc.in)
			if got != tc.want || redacted != tc.redacted {
				t.Errorf("redact(%q) = (%q, %v), want (%q, %v)", tc.in, got, redacted, tc.want, tc.redacted)
			}
		})
	}
}

func TestRedactTakesTheChannelsWordsAsData(t *testing.T) {
	tests := []struct {
		name, in, want string
		redacted       bool
	}{
		{"a password after a keyword", "mật khẩu: abc12345 nhé", "mật khẩu: " + hidden + " nhé", true},
		{"a short keyword and no separator", "mk 123456abc", "mk " + hidden, true},
		{"a code after a multi-word keyword", "Mã OTP của em là 482913", "Mã OTP của em là " + hidden, true},
		{"labelled login lines", "Tk: kho@example.com\nMk: Shop@2026", hidden + "\nMk: " + hidden, true},
		{"talk about a password keeps its words", "mật khẩu bị sai không đăng nhập được", "mật khẩu bị sai không đăng nhập được", false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, redacted := redactOne(viKeywords, tc.in)
			if got != tc.want || redacted != tc.redacted {
				t.Errorf("redact(%q) = (%q, %v), want (%q, %v)", tc.in, got, redacted, tc.want, tc.redacted)
			}
		})
	}
	// Without the channel's words the frame does not know them.
	if got, _ := redactOne(nil, "mật khẩu: abc12345 nhé"); got != "mật khẩu: abc12345 nhé" {
		t.Errorf("without keywords = %q, want the text unchanged", got)
	}
	r := newRedactor(viKeywords)
	if !r.hints("không đăng nhập được") || newRedactor(nil).hints("không đăng nhập được") {
		t.Error("a keyword must count as talk about credentials only when the channel lists it")
	}
}

func TestRedactHidesALoginAndPasswordSentAsTwoMessages(t *testing.T) {
	tests := []struct {
		name    string
		in      []string
		want    []string
		changed []bool
	}{
		{"an email, then a bare password, then a sentence",
			[]string{"someone@example.com", "Matkhau@2024", "this is what I use"},
			[]string{hidden, hidden, "this is what I use"}, []bool{true, true, false}},
		{"a user name, then a bare password", []string{"nguyenvana", "Matkhau@2024"}, []string{hidden, hidden}, []bool{true, true}},
		{"empty messages between the two do not break the pair",
			[]string{"someone@example.com", "", "  ", "Matkhau@2024"}, []string{hidden, "", "  ", hidden}, []bool{true, false, false, true}},
		{"a password alone is not hidden", []string{"the till is down", "Matkhau@2024"}, []string{"the till is down", "Matkhau@2024"}, []bool{false, false}},
		{"no messages", []string{}, []string{}, []bool{}},
		{"NUL bytes are dropped", []string{"error \x001\x00 at", "someone@example.com\x00"}, []string{"error 1 at", "someone@example.com"}, []bool{false, false}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, changed := newRedactor(nil).sequence(tc.in)
			if !slices.Equal(got, tc.want) || !slices.Equal(changed, tc.changed) {
				t.Errorf("sequence(%q) = (%q, %v), want (%q, %v)", tc.in, got, changed, tc.want, tc.changed)
			}
		})
	}
}

// A label on its own (a message, or a line, that is nothing but "password:" or "mật khẩu là") opens the
// next message or line of the same sender, and a password written before its label is hidden too.
func TestRedactHidesAPasswordNextToALabelOnItsOwn(t *testing.T) {
	tests := []struct {
		name    string
		in      []string
		want    []string
		changed []bool
	}{
		{"a keyword, then the password", []string{"mật khẩu", "Matkhau123"}, []string{"mật khẩu", hidden}, []bool{false, true}},
		{"a keyword and a colon, then the password", []string{"Mật khẩu:", "Matkhau@123"}, []string{"Mật khẩu:", hidden}, []bool{false, true}},
		{"a keyword with a word after it", []string{"mật khẩu là", "Hunter2024"}, []string{"mật khẩu là", hidden}, []bool{false, true}},
		{"an English label and a colon", []string{"password:", "Hunter2024"}, []string{"password:", hidden}, []bool{false, true}},
		{"a PIN after its label", []string{"PIN:", "4821"}, []string{"PIN:", hidden}, []bool{false, true}},
		{"a label on its own line in one message", []string{"password:\nHunter2024"}, []string{"password:\n" + hidden}, []bool{true}},
		{"talk after a label stays", []string{"password:", "it does not work"}, []string{"password:", "it does not work"}, []bool{false, false}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, changed := newRedactor(viKeywords).sequence(tc.in)
			if !slices.Equal(got, tc.want) || !slices.Equal(changed, tc.changed) {
				t.Errorf("sequence(%q) = (%q, %v), want (%q, %v)", tc.in, got, changed, tc.want, tc.changed)
			}
		})
	}
	for in, want := range map[string]string{
		"Abc@12345 is my password":     hidden + " is my password",
		"Abc@12345 là mật khẩu":        hidden + " là mật khẩu",
		"Hunter2024 is the password.":  hidden + " is the password.",
		"order SO2026001 is late":      "order SO2026001 is late",
		"my password is wrong":         "my password is wrong",
		"call me at 0912345678 please": "call me at 0912345678 please",
	} {
		if got, _ := redactOne(viKeywords, in); got != want {
			t.Errorf("redact(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestRedactNeverLeavesANulByte(t *testing.T) {
	for _, in := range []string{"error \x001\x00 at https://x.test/a", "\x000\x00", "https://x.test/\x00" + "7\x00", "\x00"} {
		if got, _ := redactOne(nil, in); strings.Contains(got, "\x00") {
			t.Errorf("redact(%q) = %q, still holds a NUL byte", in, got)
		}
	}
}

func TestSecretShapesHidesABarePasswordReadAlone(t *testing.T) {
	r := newRedactor(nil)
	for in, want := range map[string]string{
		"Matkhau@2024":               hidden,
		"use Matkhau2024x to log in": "use " + hidden + " to log in",
		"order SO2026001 is late":    "order SO2026001 is late",
		"iPhone15 screen broke":      "iPhone15 screen broke",
	} {
		if got := r.secretShapes(in); got != want {
			t.Errorf("secretShapes(%q) = %q, want %q", in, got, want)
		}
	}
}
