package ai

import (
	"slices"
	"strings"
	"testing"
	"time"
	"unicode"
)

// unicodeArticles hold a number in ASCII, one in fullwidth digits with en dashes, one in
// Arabic-Indic digits, and an order number written in groups.
func unicodeArticles(t *testing.T) string {
	return evalArticles(t) + "Call us on +84 (912) 345-678. Hotline: ０２８–３８２２–９９９９. " +
		"Fax ٠٢٨ ٣٨٢٢ ١٢٣٤. Sample order number: 4401 2210 88."
}

// A made-up number is hidden however its digits and separators are written: Unicode spaces, dashes and
// dots, fullwidth forms, any script's decimal digits, invisible format characters, or a mix of them.
func TestMadeUpPhoneNumbersInUnicodeFormsAreHidden(t *testing.T) {
	f := newChatFilter(unicodeArticles(t))
	for _, number := range []string{
		"0987\u00a0654\u00a0321",      // no-break space
		"0987\u202f654\u202f321",      // narrow no-break space
		"0987\u2009654\u2009321",      // thin space
		"0987\u200a654\u200a321",      // hair space
		"0987\u2007654\u2007321",      // figure space
		"0987\u3000654\u3000321",      // ideographic space
		"0987–654–321",                // en dash
		"0987—654—321",                // em dash
		"0987\u2011654\u2011321",      // non-breaking hyphen
		"0987\u2212654\u2212321",      // minus sign
		"0987－654－321",                // fullwidth hyphen-minus
		"0987·654·321",                // middle dot
		"0987．654．321",                // fullwidth full stop
		"0987\u200b654\u200b321",      // zero-width space
		"0987\u2060654\u2060321",      // word joiner
		"0987\u00ad654\u00ad321",      // soft hyphen
		"０９８７６５４３２１",                  // fullwidth digits
		"（０９８７）６５４ ３２１",               // fullwidth brackets
		"＋８４ ９８７ ６５４ ３２１",             // fullwidth plus
		"٠٩٨٧٦٥٤٣٢١",                  // Arabic-Indic digits
		"۰۹۸۷ ۶۵۴ ۳۲۱",                // extended Arabic-Indic digits
		"०९८७-६५४-३२१",                // Devanagari digits
		"০৯৮৭ ৬৫৪ ৩২১",                // Bengali digits
		"๐๙๘๗๖๕๔๓๒๑",                  // Thai digits
		"𝟎𝟗𝟖𝟕 𝟔𝟓𝟒 𝟑𝟐𝟏",                // mathematical bold digits
		"09٨٧\u00a0６５４–3２1",           // mixed scripts and separators
		"+84\u2009(98)\u2009765–4321", // thin spaces, brackets and a dash
		"⁰⁹⁸⁷ ⁶⁵⁴ ³²¹",                // superscript digits
		"₀₉₈₇ ₆₅₄ ₃₂₁",                // subscript digits
		"⓪⑨⑧⑦ ⑥⑤④ ③②①",                // circled digits
		"⓿❾❽❼ ❻❺❹ ❸❷❶",                // negative circled digits
		"⑴⑼⑻⑺ ⑹⑸⑷ ⑶⑵⑴",                // parenthesized digits
		"0\u03329\u03328\u03327\u0332 6\u03325\u03324\u0332 3\u03322\u03321\u0332",                                 // combining low line after each digit
		"0\u20e39\u20e38\u20e37\u20e36\u20e35\u20e34\u20e33\u20e32\u20e31\u20e3",                                   // enclosing keycap without VS16
		"0\ufe0f\u20e39\ufe0f\u20e38\ufe0f\u20e37\ufe0f\u20e36\ufe0f\u20e35\ufe0f\u20e34\ufe0f\u20e33\ufe0f\u20e3", // keycap emoji
		"٠٩٨٧\u066c٦٥٤\u066c٣٢١",                                                                                   // Arabic thousands separator
		"٠٩٨٧\u066b٦٥٤\u066b٣٢١",                                                                                   // Arabic decimal separator
	} {
		answer := "Call " + number + " for a refund. Thanks!"
		for _, piece := range []int{2, 5, 1000} {
			out, hidden := stream(f, answer, piece)
			if !hidden {
				t.Errorf("piece %d %q was shown: %q", piece, number, out)
			}
			if strings.IndexFunc(out, unicode.IsDigit) >= 0 {
				t.Errorf("piece %d %q leaked digits: %q", piece, number, out)
			}
		}
	}
}

// A number the articles hold is kept when the answer writes it with other digits or separators, and
// text that only looks numeric (prices, dates, versions, short counts) is kept in any script.
func TestKnownNumbersAndOrdinaryFiguresInUnicodeFormsAreShown(t *testing.T) {
	f := newChatFilter(unicodeArticles(t))
	for _, answer := range []string{
		"You can call ＋８４ ９１２ ３４５ ６７８ any day [#104].",
		"You can call +84\u00a0912\u00a0345\u00a0678 any day.",
		"You can call +84–912–345–678 any day.",
		"You can call +٨٤ ٩١٢ ٣٤٥ ٦٧٨ any day.",
		"The hotline is 028 3822 9999.",
		"The hotline is ๐๒๘ ๓๘๒๒ ๙๙๙๙.",
		"The fax is 028.3822.1234.",
		"Your sample order is ４４０１２２１０８８.",
		"Growth costs ４９ US dollars for up to 2,000 orders [#108].",
		"The limit is １０．０００ orders a month.",
		"It costs 1\u202f299,00 € a year.",
		"Version 2.14.3 fixed it, and v١.٢٤.١٠ too.",
		"Order #4821 shipped on 15/11/2026.",
		"It shipped on ２０２６/１１/１５, in 3–5 days.",
		"Gói Growth giá 49 đô la cho 2.000 đơn hàng.",
		"The hotline is ⁰²⁸ ³⁸²² ⁹⁹⁹⁹.",
		"The hotline is 0️⃣2️⃣8️⃣ 3️⃣8️⃣2️⃣2️⃣ 9️⃣9️⃣9️⃣9️⃣.",
		"The fax is ٠٢٨٬٣٨٢٢٬١٢٣٤.",
		"A 100 m² room holds 2³ desks, see note ①.",
		"Cà phê ngon, giá 45.000₫ một ly.",
	} {
		for _, piece := range []int{2, 5, 1000} {
			if out, hidden := stream(f, answer, piece); hidden || out != answer {
				t.Errorf("piece %d hid part of %q: %q", piece, answer, out)
			}
		}
	}
}

// The articles' numbers are found in any script and with any separators, as their ASCII digits.
func TestPhoneNumbersInUnicodeFormsAreFoundInTheArticles(t *testing.T) {
	got := phoneDigits("Hotline：０２８ ３８２２ ９９９９，café ٠٩١٢\u00a0٣٤٥\u00a0٦٧٨é and ١٢–٣٤ or Fax\u2009028–3822–1234.")
	want := []string{"02838229999", "0912345678", "02838221234"}
	if !slices.Equal(got, want) {
		t.Fatalf("phone digits = %q, want %q", got, want)
	}
}

// A long sentence is never sent early in the middle of a number written in groups of other digits.
func TestALongSentenceIsNotCutInsideAUnicodeNumber(t *testing.T) {
	var c chatSentences
	at := time.Unix(0, 0)
	words := strings.Repeat("word ", 45) + "＋８４ ９１２ ٣٤٥"
	if out := c.add(words, at); len(out) != 1 || strings.ContainsRune(out[0], '９') || !strings.HasSuffix(strings.TrimSpace(out[0]), "word") {
		t.Fatalf("early cut = %q", out)
	}
}

// A range or a date written with a dash is not a phone number: two groups of up to three digits
// ("100-500", "9–5") or a year, month and day ("2026–11–15"). A number of phone length written with
// dashes is still hidden, and so is a dashed run that is no such range or date. A range of years
// ("2024–2026") is hidden too: a made-up hotline ("1900-2000") reads the same.
func TestNumberRangesAndDatesWithDashesAreShown(t *testing.T) {
	f := newChatFilter(unicodeArticles(t))
	for _, c := range []struct {
		answer string
		hidden bool
	}{
		{"Call our hotline 1900-2000 for a refund.", true},
		{"Call our hotline 1900–2000 for a refund.", true},
		{"Call our hotline 1900–2050 for a refund.", true},
		{"Call our hotline 1900-2050 for a refund.", true},
		{"Call (1900-2000) for a refund.", true},
		{"Call (1900–2000) for a refund.", true},
		{"Call 2012-2045 for a refund.", true},
		{"Call 2012–2045 for a refund.", true},
		{"We supported 2024–2026 releases.", true},
		{"We supported 2024-2026 releases.", true},
		{"We supported 2024—2026 releases.", true},
		{"We supported releases (2024–2026).", true},
		{"Teams with 100–500 tickets a month pick Growth.", false},
		{"Teams with 100-500 tickets a month pick Growth.", false},
		{"Teams with １００–５００ tickets a month pick Growth.", false},
		{"Released on 2026–11–15.", false},
		{"Released on 2026-11-15.", false},
		{"Support is open 9–5 on weekdays.", false},
		{"Support is open 9-5 on weekdays.", false},
		{"Released on 1999–12–31.", false},
		{"Call 2026–02–30 for a refund.", true},
		{"Call 2026–2024 for a refund.", true},
		{"Call 1899–1950 for a refund.", true},
		{"Call 321–7654 for a refund.", true},
		{"Call our hotline 1900–1234 for a refund.", true},
		{"Call our hotline 1900-1234 for a refund.", true},
		{"Call (1900-1234) for a refund.", true},
		{"Call 1800–6789 for free.", true},
		{"Call 1800-6789 for free.", true},
		{"In Hong Kong call 2123–4567.", true},
		{"In Hong Kong call 2123-4567.", true},
		{"Call 555–1234 for a refund.", true},
		{"Call 555-1234 for a refund.", true},
		{"Call 0987–654–321 for a refund.", true},
		{"Call 0987-654-321 for a refund.", true},
		{"Call 098–7654–321 for a refund.", true},
		{"Call 12345–6789 for a refund.", true},
		{"Call 2026–13–45 for a refund.", true},
		{"Call 2024 – 2026 for a refund.", true},
		{"Call +2024–2026 for a refund.", true},
	} {
		for _, piece := range []int{2, 5, 1000} {
			out, hidden := stream(f, c.answer, piece)
			if hidden != c.hidden {
				t.Errorf("piece %d %q: hidden=%v out=%q", piece, c.answer, hidden, out)
			}
			if !hidden && out != c.answer {
				t.Errorf("piece %d changed %q: %q", piece, c.answer, out)
			}
		}
	}
}

// The articles' numbers are found when their digits are superscript, circled or keycaps, or grouped
// with Arabic separators.
func TestPhoneNumbersInCompatibilityFormsAreFoundInTheArticles(t *testing.T) {
	got := phoneDigits("Hotline ⁰²⁸ ³⁸²² ⁹⁹⁹⁹, fax ⓪②⑧-③⑧②②-①②③④, mobile ٠٩١٢٬٣٤٥٬٦٧٨, " +
		"desk 0️⃣9️⃣8️⃣7️⃣ 654 321.")
	want := []string{"02838229999", "02838221234", "0912345678", "0987654321"}
	if !slices.Equal(got, want) {
		t.Fatalf("phone digits = %q, want %q", got, want)
	}
}
