package domain

import (
	"strings"
	"unicode"
)

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
// is refused too; that is the accepted false positive. Look-alike spellings ("paypa1") are not caught.
func DeniedBrand(name string) (string, bool) {
	words := strings.FieldsFunc(strings.ToLower(name), func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) })
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
