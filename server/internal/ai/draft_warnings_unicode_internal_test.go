package ai

import (
	"testing"
)

// numberWarnings are the numbers draftWarnings lists for reply.
func numberWarnings(reply string, c draftContext) []string {
	var out []string
	for _, w := range draftWarnings(reply, c) {
		if w.Kind == "number" {
			out = append(out, w.Value)
		}
	}
	return out
}

// A made-up number is listed however its digits and separators are written, as the draft writes it.
func TestADraftWarnsOfAMadeUpNumberInUnicodeForms(t *testing.T) {
	c := draftContext{subject: "Refund", threadText: "Customer: where is my refund?"}
	for _, number := range []string{
		"0987\u00a0654\u00a0321", // no-break space
		"0987\u202f654\u202f321", // narrow no-break space
		"0987\u2009654\u2009321", // thin space
		"0987\u3000654\u3000321", // ideographic space
		"0987\u2013654\u2013321", // en dash
		"0987\u2212654\u2212321", // minus sign
		"0987\u00b7654\u00b7321", // middle dot
		"0987\u200b654\u200b321", // zero-width space
		"0987\u00ad654\u00ad321", // soft hyphen
		"\uff10\uff19\uff18\uff17\uff16\uff15\uff14\uff13\uff12\uff11",                                           // fullwidth digits
		"\uff0b\uff18\uff14 \uff19\uff18\uff17 \uff16\uff15\uff14 \uff13\uff12\uff11",                            // fullwidth plus and digits
		"\u0660\u0669\u0668\u0667\u0666\u0665\u0664\u0663\u0662\u0661",                                           // Arabic-Indic digits
		"\u0966\u096f\u096e\u096d-\u096c\u096b\u096a-\u0969\u0968\u0967",                                         // Devanagari digits
		"\U0001d7ce\U0001d7d7\U0001d7d6\U0001d7d5 \U0001d7d4\U0001d7d3\U0001d7d2 \U0001d7d1\U0001d7d0\U0001d7cf", // mathematical bold digits
		"09\u0668\u0667\u00a0\uff16\uff15\uff14\u20133\uff121",                                                   // mixed scripts and separators
	} {
		got := numberWarnings("Call "+number+" for a refund. Thanks!", c)
		if len(got) != 1 || got[0] != number {
			t.Errorf("%q: number warnings = %q, want [%q]", number, got, number)
		}
	}
}

// A number the conversation holds is not listed when the draft writes it with other digits or
// separators, in either direction.
func TestADraftDoesNotWarnOfAKnownNumberInAnotherForm(t *testing.T) {
	c := draftContext{subject: "Order ４４０１ ２２１０ ８８",
		threadText: "Customer: call me on +84 912 345 678 or at the office on ٠٢٨ ٣٨٢٢ ٩٩٩٩.",
		prompt:     "mention the hotline 028 3822 1234"}
	for _, reply := range []string{
		"We will call you on ＋８４ ９１２ ３４５ ６７８ today.",
		"We will call you on +84 912 345 678 today.",
		"We will call you on +٨٤ ٩١٢ ٣٤٥ ٦٧٨ today.",
		"We will call the office on 028 3822 9999.",
		"We will call the office on 028–3822–9999.",
		"Our hotline is 028 3822 1234.",
		"Our hotline is ０２８．３８２２．１２３４.",
		"Your order 4401221088 ships today.",
		"Your order 4401 2210 88 ships today.",
	} {
		if got := numberWarnings(reply, c); len(got) != 0 {
			t.Errorf("%q: number warnings = %q, want none", reply, got)
		}
	}
}

// Text that only looks numeric (prices, dates, versions, short order numbers) is not listed in any script.
func TestADraftDoesNotWarnOfOrdinaryFiguresInUnicodeForms(t *testing.T) {
	c := draftContext{subject: "Plans", threadText: "Customer: what does Growth cost?"}
	for _, reply := range []string{
		"Growth costs ４９ US dollars for up to 2,000 orders.",
		"The limit is １０．０００ orders a month.",
		"It costs 1 299,00 € a year.",
		"Version 2.14.3 fixed it, and v١.٢٤.١٠ too.",
		"Order #4821 shipped on 15/11/2026.",
		"Order #４８２１ shipped on ２０２６/１１/１５, in 3–5 days.",
		"It shipped on １５.１１, at 10:30.",
		"Gói Growth giá 49 đô la cho 2.000 đơn hàng.",
	} {
		if got := numberWarnings(reply, c); len(got) != 0 {
			t.Errorf("%q: number warnings = %q, want none", reply, got)
		}
	}
}
