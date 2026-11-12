package httpapi_test

import (
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"testing"
)

// vietnameseLetters is every letter of the Vietnamese alphabet with each of its tone marks, lower and
// upper case, written out by hand. A heading font without one of them draws that letter from another
// font, so a word mixes two typefaces.
const vietnameseLetters = "aàảãáạăằẳẵắặâầẩẫấậbcdđeèẻẽéẹêềểễếệghiìỉĩíịklmnoòỏõóọôồổỗốộơờởỡớợpqrstuùủũúụưừửữứựvxyỳỷỹýỵ" +
	"AÀẢÃÁẠĂẰẲẴẮẶÂẦẨẪẤẬBCDĐEÈẺẼÉẸÊỀỂỄẾỆGHIÌỈĨÍỊKLMNOÒỎÕÓỌÔỒỔỖỐỘƠỜỞỠỚỢPQRSTUÙỦŨÚỤƯỪỬỮỨỰVXYỲỶỸÝỴ"

var (
	fontFaceURL    = regexp.MustCompile(`url\("([^"]+\.woff2)"\)`)
	sameFolderFont = regexp.MustCompile(`url\("[a-z0-9-]+\.woff2"\)`)
)

// TestHelpCenterHeadingFontDrawsEveryVietnameseLetter: the help center serves its serif heading font
// itself, so a heading in Vietnamese is drawn in one typeface on every device, including a Mac whose
// own serif fonts lack some Vietnamese letters.
func TestHelpCenterHeadingFontDrawsEveryVietnameseLetter(t *testing.T) {
	api, service, _, _ := testAPIWithStorage(t)
	site := helpCenterSite(api)
	admin := createSession(t, api, service, "Heading Font", "admin@heading-font.example.com")
	slug := workspaceSlug(t, api, admin)
	enableHelpCenter(t, api, admin, "Trợ giúp", true)
	base := "/help/" + slug

	page := visit(site, base, "192.0.2.70")
	if csp := page.Header().Get("Content-Security-Policy"); !strings.Contains(csp, "font-src 'self';") {
		t.Fatalf("page policy lets no font load: %q", csp)
	}
	sheets := stylesheets(page.Body.String())
	if len(sheets) == 0 {
		t.Fatalf("page links no style sheet")
	}
	sheetURL, _ := url.Parse(sheets[0])
	sheet := visit(site, sheets[0], "192.0.2.70").Body.String()
	if !strings.Contains(sheet, `--serif: "Noto Serif", `) {
		t.Fatalf("the heading stack does not start with the served font:\n%s", sheet)
	}
	fonts := fontFaceURL.FindAllStringSubmatch(sheet, -1)
	if len(fonts) != 2 {
		t.Fatalf("font faces in the sheet = %v, want the regular and the bold Noto Serif", fonts)
	}
	for _, found := range fonts {
		ref, _ := url.Parse(found[1])
		address := sheetURL.ResolveReference(ref).Path
		response := visit(site, address, "192.0.2.70")
		header := response.Header()
		if response.Code != http.StatusOK || header.Get("Content-Type") != "font/woff2" || header.Get("Cache-Control") != "public, max-age=31536000, immutable" {
			t.Fatalf("%s status=%d headers=%v", address, response.Code, header)
		}
		runes, err := woff2Runes(response.Body.Bytes())
		if err != nil {
			t.Fatalf("%s: %v", address, err)
		}
		var missing []string
		for _, letter := range vietnameseLetters {
			if !runes[letter] {
				missing = append(missing, string(letter))
			}
		}
		if len(missing) > 0 {
			t.Errorf("%s draws no %s", address, strings.Join(missing, " "))
		}
		// A name that is not the current font's is the help center's 404, never a font kept for good.
		stale := strings.Replace(address, ".woff2", "0.woff2", 1)
		if response := visit(site, stale, "192.0.2.70"); response.Code != http.StatusNotFound {
			t.Errorf("%s status=%d, want 404", stale, response.Code)
		}
	}

	// A theme that picks a serif heading keeps the served font first.
	kbCall(t, api, http.MethodPut, themePath, map[string]any{"custom_css": "", "heading_font": "transitional-serif"}, admin, http.StatusOK, nil)
	brand := visit(site, brandSheet(t, visit(site, base, "192.0.2.70")), "192.0.2.70").Body.String()
	if !strings.Contains(brand, `--font-head:"Noto Serif", `) {
		t.Fatalf("transitional-serif brand sheet:\n%s", brand)
	}
}
