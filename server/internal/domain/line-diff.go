package domain

import "strings"

// DiffLine is one line of a line diff: kept (same), removed from the text before, or added in the
// text after.
type DiffLine struct {
	Change string `json:"change"`
	Text   string `json:"text"`
}

// maxDiffCells bounds the table LineDiff builds: past it, the changed part shows as removed then
// added instead of matched line by line, as two long, very different texts would take too much memory.
const maxDiffCells = 4_000_000

func diffLinesOf(text string) []string {
	if text == "" {
		return nil
	}
	return strings.Split(strings.ReplaceAll(strings.ReplaceAll(text, "\r\n", "\n"), "\r", "\n"), "\n")
}

// LineDiff lists the lines of both texts in order, as the web app's Compare with published shows
// them: in a changed run the removed lines come first, then the added ones.
func LineDiff(before, after string) []DiffLine {
	a, b := diffLinesOf(before), diffLinesOf(after)
	start := 0
	for start < len(a) && start < len(b) && a[start] == b[start] {
		start++
	}
	endA, endB := len(a), len(b)
	for endA > start && endB > start && a[endA-1] == b[endB-1] {
		endA, endB = endA-1, endB-1
	}
	out := make([]DiffLine, 0, len(a)+len(b))
	push := func(change string, lines []string) {
		for _, text := range lines {
			out = append(out, DiffLine{Change: change, Text: text})
		}
	}
	push("same", a[:start])
	n, m := endA-start, endB-start
	if n*m > maxDiffCells {
		push("removed", a[start:endA])
		push("added", b[start:endB])
	} else {
		// longest[i*width+j] is the longest run of lines, in order, that a[start+i:endA] and
		// b[start+j:endB] share.
		width := m + 1
		longest := make([]uint32, (n+1)*width)
		for i := n - 1; i >= 0; i-- {
			for j := m - 1; j >= 0; j-- {
				if a[start+i] == b[start+j] {
					longest[i*width+j] = longest[(i+1)*width+j+1] + 1
				} else {
					longest[i*width+j] = max(longest[(i+1)*width+j], longest[i*width+j+1])
				}
			}
		}
		for i, j := 0, 0; i < n || j < m; {
			switch {
			case i < n && j < m && a[start+i] == b[start+j]:
				out = append(out, DiffLine{"same", a[start+i]})
				i, j = i+1, j+1
			case j >= m || (i < n && longest[(i+1)*width+j] >= longest[i*width+j+1]):
				out = append(out, DiffLine{"removed", a[start+i]})
				i++
			default:
				out = append(out, DiffLine{"added", b[start+j]})
				j++
			}
		}
	}
	push("same", a[endA:])
	return out
}
