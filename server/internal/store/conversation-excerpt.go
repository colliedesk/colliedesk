package store

// excerptRunes is how much of a customer's message an inbox row shows: one line of the list column.
const excerptRunes = 140

// excerptReadRunes is how much the list query reads, one more than is shown, so a longer message is
// known to go on and gets its ellipsis without reading the whole body.
const excerptReadRunes = excerptRunes + 1

// excerpt cuts a message already on one line (spaces folded by the query) to excerptRunes, ending a cut
// one with "…" at the last word boundary that keeps most of it.
func excerpt(text string) string {
	runes := []rune(text)
	if len(runes) <= excerptRunes {
		return text
	}
	cut := runes[:excerptRunes]
	for i := len(cut) - 1; i > excerptRunes*3/4; i-- {
		if cut[i] == ' ' {
			cut = cut[:i]
			break
		}
	}
	return string(cut) + "…"
}
