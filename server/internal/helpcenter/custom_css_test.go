package helpcenter

import (
	"errors"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// Hostile custom CSS, written by hand, with where and why it must be refused. Each string is an attack
// a stylesheet could carry: loading from elsewhere, running script in old browsers, or leaving the
// <style> element the preview puts it in.
var refusedCustomCSS = []struct {
	name, css string
	line, col int
	reason    string
}{
	{"import", `@import "x";`, 1, 1, "@import is not allowed"},
	{"import in capitals", `@IMPORT "x";`, 1, 1, "@import is not allowed"},
	{"import inside media", `@media screen{@import "x";}`, 1, 15, "@import is not allowed"},
	{"font face", `@font-face{font-family:x}`, 1, 1, "@font-face is not allowed"},
	{"namespace", `@namespace svg "http://www.w3.org/2000/svg";`, 1, 1, "@namespace is not allowed"},
	{"charset", `@charset "utf-8";`, 1, 1, "@charset is not allowed"},
	{"property", `@property --x{syntax:"*"}`, 1, 1, "@property is not allowed"},
	{"moz document", `@-moz-document url-prefix(){a{color:red}}`, 1, 1, "@-moz-document is not allowed"},
	{"url", `a{background:url(x)}`, 1, 14, "url() is not allowed"},
	{"url quoted with spaces", `a{background:url( "x" )}`, 1, 14, "url() is not allowed"},
	{"url in capitals", `a{background:URL(x)}`, 1, 14, "url() is not allowed"},
	{"url in a custom property", `a{--bg:url(x)}`, 1, 8, "url() is not allowed"},
	{"url in supports", `@supports (background:url(x)){a{color:red}}`, 1, 23, "url() is not allowed"},
	{"javascript url", `a{background:url(javascript:alert(1))}`, 1, 14, "url() is not allowed"},
	{"url on line three", "a{\n  color:red;\n  background:url(x)\n}", 3, 14, "url() is not allowed"},
	{"hex escape in url", `a{background:u\72l(x)}`, 1, 15, "a backslash is allowed only inside quotes"},
	{"hex escape with space", `a{background:\75 rl(x)}`, 1, 14, "a backslash is allowed only inside quotes"},
	{"letter escape", `a{background:u\rl(x)}`, 1, 15, "a backslash is allowed only inside quotes"},
	{"escaped at-rule", `@\69mport "x";`, 1, 2, "a backslash is allowed only inside quotes"},
	{"image-set", `a{background:image-set("x" 1x)}`, 1, 14, "image-set() is not allowed"},
	{"webkit image-set", `a{background:-webkit-image-set("x" 1x)}`, 1, 14, "-webkit-image-set() is not allowed"},
	{"image", `a{background:image("x")}`, 1, 14, "image() is not allowed"},
	{"cross-fade", `a{background:cross-fade(red,blue)}`, 1, 14, "cross-fade() is not allowed"},
	{"element", `a{background:element(#x)}`, 1, 14, "element() is not allowed"},
	{"moz element", `a{background:-moz-element(#x)}`, 1, 14, "-moz-element() is not allowed"},
	{"src", `a{background:src("x")}`, 1, 14, "src() is not allowed"},
	{"paint", `a{background:paint(x)}`, 1, 14, "paint() is not allowed"},
	{"expression", `a{x:expression(1)}`, 1, 5, "expression() is not allowed"},
	{"expression in capitals", `a{x:EXPRESSION(alert(1))}`, 1, 5, "expression() is not allowed"},
	{"behavior", `a{behavior:url(x.htc)}`, 1, 3, "behavior is not allowed"},
	{"moz binding", `a{-moz-binding:x}`, 1, 3, "-moz-binding is not allowed"},
	{"closing brace alone", `}`, 1, 1, "} closes nothing open here"},
	{"block never closed", `a{color:red`, 1, 2, "{ is never closed"},
	{"parenthesis closed by a brace", `a{width:calc(1px}`, 1, 17, "} closes nothing open here"},
	{"style end in a string", `a::after{content:"</style><script>alert(1)</script>"}`, 1, 19, "</ is not allowed"},
	{"style end in capitals", `a{}</STYLE><img src=x>`, 1, 4, "</ is not allowed"},
	{"style end in a comment", `/* </style> */`, 1, 4, "</ is not allowed"},
	{"html comment open", `<!-- a{color:red}`, 1, 1, "<!-- and --> are not allowed"},
	{"url hidden in a string by escapes", `a::after{content:"\75 rl(x)"}`, 1, 19, "url( is not allowed"},
	{"import hidden in a string", `a::after{content:"\40 import"}`, 1, 19, "@import is not allowed"},
	{"javascript in a string", `a::after{content:"javascript:alert(1)"}`, 1, 19, "javascript: is not allowed"},
	{"url in a comment", `/* url(x) */ a{color:red}`, 1, 4, "url( is not allowed"},
	{"unclosed string", `a::after{content:"x}`, 1, 18, "the quotation mark is never closed"},
	{"unclosed comment", `a{color:red} /* the rest`, 1, 14, "the comment is never closed"},
	{"nul", "a{color:red}\x00", 1, 13, "a NUL character is not allowed"},
	{"invalid utf-8", "a{color:red}\n\xff", 2, 1, "custom CSS must be UTF-8 text"},
	{"byte order mark", "\ufeffa{color:red}", 1, 1, "a byte order mark is not allowed"},
	{"control character in a string", "a::after{content:\"a\x01b\"}", 1, 20, "a control character (U+0001) is not allowed"},
	{"delete character", "a{}\x7f", 1, 4, "a control character (U+007F) is not allowed"},
	{"behavior after white space", `a{behavior :x}`, 1, 3, "behavior is not allowed"},
	{"behavior before a comment", `a{ /* c */ behavior/**/:x}`, 1, 12, "behavior is not allowed"},
	{"moz binding after a declaration", `a{color:red;-moz-binding:none}`, 1, 13, "-moz-binding is not allowed"},
	// The tokenizer reads U+0 as a unicode range and the rest as a function with a dash before its
	// name, which a vendor prefix would not explain; the decoded word list refuses each anyway.
	{"image after a unicode range", `a{x:U+0-image("x")}`, 1, 9, "image( is not allowed"},
	{"image-set after a unicode range", `a{x:U+0-image-set("x" 1x)}`, 1, 9, "image-set( is not allowed"},
	{"cross-fade after a unicode range", `a{x:U+0-cross-fade(red,blue)}`, 1, 9, "cross-fade( is not allowed"},
	{"src after a unicode range", `a{x:U+0-src("x")}`, 1, 9, "src( is not allowed"},
	{"element after a unicode range", `a{x:U+0-element(#x)}`, 1, 9, "element( is not allowed"},
	{"paint after a unicode range", `a{x:U+0-paint(x)}`, 1, 9, "paint( is not allowed"},
	{"webkit image in a string", `a{--x:"-webkit-image(x)"}`, 1, 16, "image( is not allowed"},
	{"moz element in a comment", `/* -moz-element(#x) */`, 1, 9, "element( is not allowed"},
	{"cross-fade spelled with an escape in a string", `a{--x:"cross\2d fade("}`, 1, 8, "cross-fade( is not allowed"},
	{"paint in capitals in a string", `a{--x:"PAINT("}`, 1, 8, "paint( is not allowed"},
	{"over 32 KiB", "/*" + strings.Repeat("x", 32*1024) + "*/", 1, 32769, "custom CSS holds at most 32,768 bytes"},
}

// Custom CSS an admin would write for details the theme form does not cover.
var acceptedCustomCSS = []string{
	``,
	`.masthead{border-bottom:2px solid #0b5cad}`,
	`@media (max-width:600px){.brand{font-size:1rem}}`,
	`@media (width < 600px){.brand{font-size:1rem}}`,
	`@supports (display:grid){.list{display:grid;gap:1rem}}`,
	`@keyframes fade{from{opacity:0}to{opacity:1}}`,
	`@-webkit-keyframes fade{from{opacity:0}to{opacity:1}}`,
	`@layer brand{a{color:#0b5cad}}`,
	`@container (min-width:400px){.card{padding:2rem}}`,
	`a::before{content:"→ "}`,
	`blockquote::before{content:"\201C"}`,
	`body{font-family:"Iowan Old Style", Georgia, serif}`,
	`.hero{width:calc(100% - var(--gap));color:rgb(0 0 0 / 50%)}`,
	"/* the footer */\n.footer a[href^=\"https\"]:hover{text-decoration:underline}",
	`.behavior-note{color:#333}`,
	`.behavior{color:#333}`,
	`.behavior:hover{color:#333}`,
	`[data-behavior="x"]{color:#333}`,
	"a::after{content:\"a\tb\"}",
	"a{\tcolor:red}",
	`h1 > a + b ~ c{margin:0 !important}`,
}

func TestCheckCustomCSSRefusesWhatCouldLoadOrEscape(t *testing.T) {
	for _, tc := range refusedCustomCSS {
		t.Run(tc.name, func(t *testing.T) {
			err := CheckCustomCSS(tc.css)
			var refused *CustomCSSError
			if !errors.As(err, &refused) {
				t.Fatalf("CheckCustomCSS(%q) = %v, want a refusal", tc.css, err)
			}
			want := "line " + strconv.Itoa(tc.line) + ", column " + strconv.Itoa(tc.col) + ": " + tc.reason
			if refused.Line != tc.line || refused.Column != tc.col || !strings.HasPrefix(err.Error(), want) {
				t.Fatalf("CheckCustomCSS(%q) = %q (line %d, column %d), want %q", tc.css, err, refused.Line, refused.Column, want)
			}
		})
	}
}

func TestCheckCustomCSSAcceptsPlainStyling(t *testing.T) {
	for _, css := range acceptedCustomCSS {
		if err := CheckCustomCSS(css); err != nil {
			t.Errorf("CheckCustomCSS(%q) = %v, want nil", css, err)
		}
	}
	if err := CheckCustomCSS(strings.Repeat("a{}", 32*1024/3) + "ab"); err != nil {
		t.Errorf("32,768 bytes refused: %v", err)
	}
}

// decodeEscapes reads CSS escapes the way a browser does (CSS Syntax 3, "consume an escaped code
// point"), on its own, so the fuzz property does not trust the checker's reading of them.
var cssEscape = regexp.MustCompile(`\\(?:([0-9a-fA-F]{1,6})[ \t\n\r\f]?|(\r\n|[\n\r\f])|(.))`)

func decodeEscapes(text string) string {
	return cssEscape.ReplaceAllStringFunc(text, func(match string) string {
		parts := cssEscape.FindStringSubmatch(match)
		switch {
		case parts[1] != "":
			code, _ := strconv.ParseUint(parts[1], 16, 32)
			if code == 0 || code > 0x10ffff || code >= 0xd800 && code <= 0xdfff {
				return "�"
			}
			return string(rune(code))
		case parts[2] != "":
			return ""
		}
		return parts[3]
	})
}

func FuzzCheckCustomCSS(f *testing.F) {
	for _, tc := range refusedCustomCSS {
		f.Add(tc.css)
	}
	for _, css := range acceptedCustomCSS {
		f.Add(css)
	}
	f.Fuzz(func(t *testing.T, css string) {
		if CheckCustomCSS(css) != nil {
			return
		}
		read := strings.ToLower(decodeEscapes(css))
		for _, banned := range []string{"url(", "@import", "image-set(", "image(", "cross-fade(", "src(", "element(", "paint(", "</", "expression("} {
			if strings.Contains(read, banned) {
				t.Fatalf("accepted %q, which reads as %q and holds %q", css, read, banned)
			}
		}
		if len(css) > 32*1024 {
			t.Fatalf("accepted %d bytes", len(css))
		}
	})
}

// An escape names a code point of up to six hex digits; anything beyond Unicode, a surrogate or zero
// reads as U+FFFD, as CSS Syntax 3 says.
func TestReadCSSReadsAnEscapeOutsideUnicodeAsTheReplacementCharacter(t *testing.T) {
	for text, want := range map[string]string{
		`\110000`: "\ufffd", `\FFFFFF`: "\ufffd", `\0`: "\ufffd", `\d800`: "\ufffd", `\DFFF`: "\ufffd",
		`\10FFFF`: "\U0010ffff", `\41 b`: "ab", `\2d`: "-",
	} {
		var got strings.Builder
		for _, r := range readCSS(text) {
			got.WriteRune(r.r)
		}
		if got.String() != want {
			t.Errorf("readCSS(%q) = %q, want %q", text, got.String(), want)
		}
	}
}
