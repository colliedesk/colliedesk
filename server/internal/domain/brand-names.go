package domain

import (
	"strings"
	"unicode"

	"golang.org/x/text/unicode/norm"
)

// lookalikes maps the Cyrillic and Greek letters that read as Latin ones to those letters. It is a
// small list of the common ones, not a confusables database.
var lookalikes = map[rune]rune{
	'а': 'a', 'е': 'e', 'о': 'o', 'р': 'p', 'с': 'c', 'х': 'x', 'у': 'y', 'і': 'i', 'ј': 'j', 'ѕ': 's',
	'һ': 'h', 'ԁ': 'd', 'ѵ': 'v', 'к': 'k', 'м': 'm', 'т': 't',
	'α': 'a', 'ο': 'o', 'ρ': 'p', 'ν': 'v', 'ι': 'i', 'κ': 'k', 'τ': 't', 'υ': 'u', 'χ': 'x',
}

// foldForMatch lowercases the name and reduces the spellings that read the same to one: fullwidth
// and other compatibility forms become plain letters (NFKD), accents are dropped, and common
// Cyrillic and Greek look-alikes become Latin letters.
func foldForMatch(name string) string {
	var folded strings.Builder
	for _, r := range norm.NFKD.String(strings.ToLower(name)) {
		switch {
		case unicode.Is(unicode.Mn, r):
			continue
		case lookalikes[r] != 0:
			folded.WriteRune(lookalikes[r])
		default:
			folded.WriteRune(r)
		}
	}
	return folded.String()
}

// deniedBrands are names a workspace on the hosted cloud may not use: the sender-looking name of a
// workspace ends up in a subdomain and in mail headers, so a big brand there is a phishing
// setup. The list is short on purpose; a real customer with such a name asks support, and an operator
// creates the workspace with the CLI. Entries are lowercase letters and digits only.
var deniedBrands = map[string]bool{
	"paypal": true, "apple": true, "google": true, "gmail": true, "microsoft": true, "outlook": true,
	"office365": true, "amazon": true, "facebook": true, "instagram": true, "whatsapp": true, "netflix": true,
	"stripe": true, "coinbase": true, "binance": true, "chase": true, "wellsfargo": true, "bankofamerica": true,
	"citibank": true, "hsbc": true, "barclays": true, "santander": true, "revolut": true, "dhl": true,
	"fedex": true, "usps": true, "royalmail": true, "docusign": true, "dropbox": true, "linkedin": true,
	"colliedesk": true, "freshdesk": true, "zendesk": true, "hclabs": true,
}

// DeniedBrand reports whether name contains a denied brand and returns the brand. The name is
// split into words at every character that is not a letter or digit, lowercased, and a brand matches
// when some run of consecutive words, joined without separators, equals it. So "PayPal Support",
// "pay pal" and "G-O-O-G-L-E" match, while "Pineapple", "Applebee" and "Googleplex" do not: a brand
// must be a whole word, never a substring. A genuine business named like a brand ("Apple Orchard")
// is refused too; that is the accepted false positive. Fullwidth forms, accents and common Cyrillic and
// Greek look-alikes are folded first; digit-for-letter spellings ("paypa1") are not caught.
func DeniedBrand(name string) (string, bool) {
	words := strings.FieldsFunc(foldForMatch(name), func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) })
	for start := range words {
		var joined strings.Builder
		for _, word := range words[start:] {
			joined.WriteString(word)
			if joined.Len() > longestBrand {
				break
			}
			if deniedBrands[joined.String()] {
				return joined.String(), true
			}
		}
	}
	return "", false
}

var longestBrand = func() int {
	longest := 0
	for brand := range deniedBrands {
		longest = max(longest, len(brand))
	}
	return longest
}()
