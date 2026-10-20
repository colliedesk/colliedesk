package helpcenter

import (
	"strings"
	"testing"
)

func TestArticleMarkdownNeverRendersScriptOrRawHTML(t *testing.T) {
	cases := map[string]string{
		"raw script":            "Hello <script>alert(1)</script>",
		"raw block":             "<div onclick=\"alert(1)\">x</div>",
		"javascript link":       "[click](javascript:alert(1))",
		"mixed case scheme":     "[click](JaVaScRiPt:alert(1))",
		"scheme with tab":       "[click](java\tscript:alert(1))",
		"entity in scheme":      "[click](java&#x09;script:alert(1))",
		"data link":             "[click](data:text/html;base64,PHNjcmlwdD4=)",
		"vbscript autolink":     "<vbscript:msgbox(1)>",
		"unknown autolink":      "<intent:evil>",
		"javascript image":      "![x](javascript:alert(1))",
		"onerror in alt":        "![\" onerror=\"alert(1)](https://example.com/a.png)",
		"style attribute table": "| a |\n|:-:|\n| b |",
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			html, err := renderArticle(body, "/help/acme")
			if err != nil {
				t.Fatal(err)
			}
			lower := strings.ToLower(string(html))
			for _, bad := range []string{"<script", "onclick", "onerror=\"", "style=", "<div"} {
				if strings.Contains(lower, bad) {
					t.Errorf("%q rendered %q, which holds %q", body, html, bad)
				}
			}
			for _, scheme := range []string{"javascript", "vbscript", "data", "intent", "java"} {
				for _, attribute := range []string{"href=\"", "src=\""} {
					if strings.Contains(lower, attribute+scheme) {
						t.Errorf("%q rendered %q, which links %s", body, html, scheme)
					}
				}
			}
		})
	}
}

func TestArticleMarkdownServesKnowledgeBaseImagesFromTheHelpCenter(t *testing.T) {
	id := "0b6f8a8e-3c4d-4e5f-8a9b-0c1d2e3f4a5b"
	html, err := renderArticle("![Settings](/api/v1/kb/images/"+id+")\n\n![Logo](https://cdn.example.com/logo.png)\n\n[download](/api/v1/kb/images/"+id+")", "https://acme.help.example")
	if err != nil {
		t.Fatal(err)
	}
	got := string(html)
	for _, want := range []string{
		`<img src="https://acme.help.example/images/` + id + `" alt="Settings" loading="lazy">`,
		`<a href="https://cdn.example.com/logo.png">Logo</a>`,
		`<a href="https://acme.help.example/images/` + id + `">download</a>`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("rendered %s\nwant it to hold %s", got, want)
		}
	}
	if strings.Contains(got, "/api/v1/") || strings.Contains(got, "<img src=\"https://cdn") {
		t.Errorf("rendered %s: an API path or an outside image is left", got)
	}
}

func TestArticleMarkdownKeepsEverydayFormatting(t *testing.T) {
	html, err := renderArticle("## Reset your password\n\n1. Open **Settings**\n2. Choose ~~old~~ new\n\n- [x] done\n\nVisit https://example.com or [mail us](mailto:help@example.com).\n\n```\ncode <b>\n```", "")
	if err != nil {
		t.Fatal(err)
	}
	got := string(html)
	for _, want := range []string{`<h2 id="reset-your-password">`, "<strong>Settings</strong>", "<del>old</del>", `type="checkbox"`,
		`<a href="https://example.com">https://example.com</a>`, `<a href="mailto:help@example.com">mail us</a>`, "code &lt;b&gt;"} {
		if !strings.Contains(got, want) {
			t.Errorf("rendered %s\nwant it to hold %s", got, want)
		}
	}
}

func TestExcerptIsPlainTextCutAtAWord(t *testing.T) {
	got := excerpt("# Title\n\nOpen **Settings** and [choose](https://x.example) the plan.\n\n```\nsecret code\n```\n\nMore words follow here.", 40)
	if got != "Title Open Settings and choose the plan…" {
		t.Fatalf("excerpt=%q", got)
	}
	if got := excerpt("Sign in as an **admin**.\nThen `open` it.", 80); got != "Sign in as an admin. Then open it." {
		t.Fatalf("inline excerpt=%q", got)
	}
	if got := excerpt("Short text.", 40); got != "Short text." {
		t.Fatalf("short excerpt=%q", got)
	}
}
