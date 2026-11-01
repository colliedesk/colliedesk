package prompt

import "testing"

func TestMaskContactsHidesPhonesEmailsIDsAndLabelledAccounts(t *testing.T) {
	tests := []struct{ name, in, want string }{
		{"national mobile", "call me on 0912345678 please", "call me on [phone] please"},
		{"mobile with spaces", "phone 0912 345 678", "phone [phone]"},
		{"mobile with dots", "0912.345.678 is mine", "[phone] is mine"},
		{"mobile with dashes", "number: 091-234-5678.", "number: [phone]."},
		{"international plus", "+84 912 345 678", "[phone]"},
		{"international plus glued", "+44 7911 123456", "[phone]"},
		{"international double zero", "0049 30 1234 5678", "[phone]"},
		{"parenthesised country code", "(+84) 912 345 678", "[phone]"},
		{"two numbers", "0912345678, 0987654321", "[phone], [phone]"},
		{"email", "send it to someone@example.com thanks", "send it to [email] thanks"},
		{"email with plus and dots", "a.b+c@mail.example.co.uk", "[email]"},
		{"labelled passport", "passport: C1234567", "passport: [id number]"},
		{"labelled national id spaced", "national id no. 123 456 789", "national id no. [id number]"},
		{"labelled account", "Account 000123456", "Account [account number]"},
		{"labelled bank account with filler", "bank account number: 1234 5678 9012", "bank account number: [account number]"},
		{"an IBAN", "IBAN DE89 3704 0044 0532 0130 00", "IBAN [account number]"},

		{"money with dots", "charged 1.500.000 twice", "charged 1.500.000 twice"},
		{"ticket reference", "is ABC-2026-0093 done", "is ABC-2026-0093 done"},
		{"date and time", "on 06/10/2026 at 14:05", "on 06/10/2026 at 14:05"},
		{"iso date", "2026-10-06 14:05", "2026-10-06 14:05"},
		{"a long chat user id", "uid 2435403143937537556 fails", "uid 2435403143937537556 fails"},
		{"digits without a leading zero or plus", "code 1234567890", "code 1234567890"},
		{"a barcode", "barcode 8934567890123", "barcode 8934567890123"},
		{"an account label with too few digits", "account 12345", "account 12345"},
		{"the word account alone", "account is locked", "account is locked"},
		{"a date with a time after dots", "on 08.10.2026 12:30 it failed", "on 08.10.2026 12:30 it failed"},
		{"a list of sizes", "sizes: 08 09 10 11 12", "sizes: 08 09 10 11 12"},
		{"a phone followed by a colon", "0912345678: call back", "0912345678: call back"},
		{"empty", "", ""},
	}
	masker := newContactMasker(ContactLabels{})
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := masker.mask(test.in); got != test.want {
				t.Errorf("mask(%q) = %q, want %q", test.in, got, test.want)
			}
		})
	}
}

func TestMaskContactsTakesTheChannelsLabelsAsData(t *testing.T) {
	labels := ContactLabels{Account: []string{"stk", "số tài khoản", "so tai khoan"}, ID: []string{"cccd", "cmnd", "căn cước công dân"}}
	masker := newContactMasker(labels)
	for in, want := range map[string]string{
		"STK: 1234567890 Vietcombank":    "STK: [account number] Vietcombank",
		"số tài khoản 12345678901234":    "số tài khoản [account number]",
		"cccd 012345678901 của em":       "cccd [id number] của em",
		"căn cước công dân 012345678901": "căn cước công dân [id number]",
		"stk 12345":                      "stk 12345",
		"tổng 1.500.000đ bị trừ hai lần": "tổng 1.500.000đ bị trừ hai lần",
	} {
		if got := masker.mask(in); got != want {
			t.Errorf("mask(%q) = %q, want %q", in, got, want)
		}
	}
	if got := newContactMasker(ContactLabels{}).mask("STK: 1234567890"); got != "STK: 1234567890" {
		t.Errorf("without the channel's labels = %q, want it unchanged", got)
	}
}

func TestMaskContactsTakesTheChannelsPatternsAsData(t *testing.T) {
	// A channel whose ID numbers are 12 digits with no label, and whose customers write their phone
	// number with the country code but no plus.
	patterns := []MaskPattern{{Pattern: `\b\d{12}\b`, Mask: "id_number"}, {Pattern: `\b84\d{9}\b`, Mask: "phone"}, {Pattern: `(`, Mask: "hidden"},
		{Pattern: `x`, Mask: "unknown"}}
	masker := newContactMasker(ContactLabels{Patterns: patterns})
	for in, want := range map[string]string{
		"my id 079300012345 please":          "my id [id number] please",
		"call 84912345678":                   "call [phone]",
		"call +84 912 345 678 or 0912345678": "call [phone] or [phone]",
		"barcode 8934567890123":              "barcode 8934567890123",
		"order x1":                           "order x1",
	} {
		if got := masker.mask(in); got != want {
			t.Errorf("mask(%q) = %q, want %q", in, got, want)
		}
	}
	// Without them the frame still masks what is shaped like a phone number, and leaves the rest.
	if got := newContactMasker(ContactLabels{}).mask("my id 079300012345, call 84912345678"); got != "my id [phone], call 84912345678" {
		t.Errorf("without the channel's patterns = %q", got)
	}
}
