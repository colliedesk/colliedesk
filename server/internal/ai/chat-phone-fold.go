package ai

import (
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"

	"golang.org/x/text/unicode/norm"
)

// A phone number can be written with any script's decimal digits and with any Unicode space, dash or
// dot between its groups, or with invisible characters inside it ("0912 345 678" with no-break spaces,
// "０９１２–３４５–６７８", "٠٩١٢٣٤٥٦٧٨"). foldPhones writes all of them the one way chatPhone reads,
// for the answer and for the articles alike, so a number the articles hold in one form is known in any
// other and a made-up number is found however it is written.

// foldPhones returns text with every decimal digit as its ASCII digit, every space as ' ', every dash
// as '-', middle dots as '.', the compatibility forms of + ( ) . as those, and format characters (zero
// width space, word joiner, soft hyphen) dropped. Text of ASCII only is returned as it is.
func foldPhones(text string) string {
	ascii := true
	for i := 0; i < len(text); i++ {
		if text[i] >= utf8.RuneSelf {
			ascii = false
			break
		}
	}
	if ascii {
		return text
	}
	var out strings.Builder
	out.Grow(len(text))
	for _, r := range text {
		if f := foldPhoneRune(r); f >= 0 {
			out.WriteRune(f)
		}
	}
	return out.String()
}

// foldPhoneRune is the rune foldPhones writes for r, or -1 when it drops r.
func foldPhoneRune(r rune) rune {
	switch {
	case r < utf8.RuneSelf || unicode.IsLetter(r):
		// Most text outside ASCII is letters (accents, other alphabets): one lookup keeps them.
		return r
	case unicode.IsDigit(r):
		return digitValue(r)
	case unicode.IsSpace(r):
		return ' '
	case unicode.Is(unicode.Cf, r):
		return -1
	case unicode.Is(unicode.Pd, r) || r == '−':
		return '-'
	case r == '·' || r == '・':
		return '.'
	case unicode.In(r, unicode.Po, unicode.Ps, unicode.Pe, unicode.Sm):
		// Fullwidth and small forms: NFKC makes '＋' '+', '（' '(' and '．' '.'.
		if k := norm.NFKC.String(string(r)); len(k) == 1 && strings.Contains("+().", k) {
			return rune(k[0])
		}
	}
	return r
}

// digitValue is the ASCII digit of a decimal digit (Unicode Nd). Unicode encodes each script's digits
// as a run of ten from zero, and the runs of unicode.Nd start at a zero, so a digit's value is its
// distance from the start of its run, modulo ten (the mathematical digits are five runs in one).
func digitValue(r rune) rune {
	at := sort.Search(len(nd32), func(i int) bool { return nd32[i].Hi >= uint32(r) })
	if at < len(nd32) && nd32[at].Lo <= uint32(r) {
		return '0' + (r-rune(nd32[at].Lo))%10
	}
	return r
}

// nd32 is unicode.Nd as one sorted list of 32-bit ranges.
var nd32 = func() []unicode.Range32 {
	var ranges []unicode.Range32
	for _, r := range unicode.Nd.R16 {
		ranges = append(ranges, unicode.Range32{Lo: uint32(r.Lo), Hi: uint32(r.Hi), Stride: uint32(r.Stride)})
	}
	return append(ranges, unicode.Nd.R32...)
}()

// phoneWordRune is a rune of a word that may be one group of a phone number.
func phoneWordRune(r rune) bool {
	f := foldPhoneRune(r)
	return f < 0 || f >= '0' && f <= '9' || strings.ContainsRune("+().-", f)
}
