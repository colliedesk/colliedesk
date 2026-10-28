package freshdesk_test

import (
	"context"
	"strings"
	"testing"

	"github.com/colliedesk/colliedesk/server/internal/freshdesk"
)

const (
	portalGoogle = "Ab3dEf6hIj9lMn2pQr5tUv8xYz1-Ab3dEf6hIj9lMn2"
	portalBing   = "0A1B2C3D4E5F60718293A4B5C6D7E8F9"
)

// portalPage is an old portal's home page with the tokens in its head, as a team pastes them into the
// portal's head section.
func portalPage(head string) string {
	return `<!doctype html><html><head><title>Acme support</title>` + head + `</head><body><h1>Help</h1></body></html>`
}

func (e *importEnv) searchConsole(t *testing.T) (google, bing *string, version int64) {
	t.Helper()
	err := e.pool.QueryRow(context.Background(), `SELECT google_site_verification, bing_site_verification, content_version FROM help_centers WHERE workspace_id=$1`,
		e.workspaceID).Scan(&google, &bing, &version)
	if err != nil {
		t.Fatalf("read tokens: %v", err)
	}
	return google, bing, version
}

func TestImportCarriesTheOldPortalsSearchConsoleTokens(t *testing.T) {
	env := newImportEnv(t).withSolutions()
	env.setHelpCenter(t, true)
	env.fake.portalPage = portalPage(`<meta name="google-site-verification" content="` + portalGoogle + `" /><META NAME="msvalidate.01" CONTENT="` + portalBing + `">`)
	report := env.linkRun(t)

	section := report.SearchConsole
	if section == nil || section.Status != freshdesk.StatusOK || section.Google != freshdesk.TokenCarried || section.Bing != freshdesk.TokenCarried {
		t.Fatalf("search console section = %+v", section)
	}
	google, bing, version := env.searchConsole(t)
	if google == nil || *google != portalGoogle || bing == nil || *bing != portalBing {
		t.Fatalf("tokens were not carried over: google set=%t bing set=%t", google != nil, bing != nil)
	}
	if report.Status != freshdesk.StatusOK {
		t.Errorf("the import status = %s", report.Status)
	}
	// The portal is public: the page is read without the API key.
	for _, auth := range env.fake.portalAuth {
		if auth != "" {
			t.Errorf("the API key was sent to the portal page: %q", auth)
		}
	}
	for _, want := range []string{"## Search Console", "Google: carried over", "Bing: carried over"} {
		if !strings.Contains(report.Markdown(), want) {
			t.Errorf("markdown lacks %q:\n%s", want, report.Markdown())
		}
	}
	if strings.Contains(report.Markdown(), portalGoogle) {
		t.Errorf("the report prints the token")
	}

	// A rerun finds both set already and changes nothing.
	again := env.linkRun(t)
	if again.SearchConsole == nil || again.SearchConsole.Google != freshdesk.TokenAlreadySet || again.SearchConsole.Bing != freshdesk.TokenAlreadySet {
		t.Fatalf("rerun section = %+v", again.SearchConsole)
	}
	if _, _, after := env.searchConsole(t); after != version {
		t.Errorf("rerun content_version %d, before %d", after, version)
	}
}

// A token the team already set in Colliedesk is never replaced by the old portal's.
func TestImportKeepsATokenAlreadySet(t *testing.T) {
	env := newImportEnv(t).withSolutions()
	env.setHelpCenter(t, true)
	const mine = "Zz9yXx8wVv7uTt6sRr5qPp4oNn3mLl2k"
	if _, err := env.pool.Exec(context.Background(), `UPDATE help_centers SET google_site_verification=$2 WHERE workspace_id=$1`, env.workspaceID, mine); err != nil {
		t.Fatal(err)
	}
	env.fake.portalPage = portalPage(`<meta name="google-site-verification" content="` + portalGoogle + `"><meta name="msvalidate.01" content="` + portalBing + `">`)
	section := env.linkRun(t).SearchConsole
	if section == nil || section.Google != freshdesk.TokenAlreadySet || section.Bing != freshdesk.TokenCarried {
		t.Fatalf("section = %+v", section)
	}
	google, bing, _ := env.searchConsole(t)
	if google == nil || *google != mine || bing == nil || *bing != portalBing {
		t.Fatalf("tokens: google=%v bing=%v, want the one already set and the old portal's Bing", google, bing)
	}
}

// Only a meta tag in the head counts, and only a token the help center could serve.
func TestImportIgnoresTokensOutsideTheHeadAndRefusesAMalformedOne(t *testing.T) {
	env := newImportEnv(t).withSolutions()
	env.setHelpCenter(t, true)
	env.fake.portalPage = `<!doctype html><html><head><title>x</title>` +
		`<meta name="google-site-verification" content="not a token &quot;&gt;&lt;script&gt;">` +
		`<script>document.write('<meta name="msvalidate.01" content="` + portalBing + `">')</script></head>` +
		`<body><meta name="msvalidate.01" content="` + portalBing + `"></body></html>`
	section := env.linkRun(t).SearchConsole
	if section == nil || section.Google != freshdesk.TokenInvalid || section.Bing != freshdesk.TokenNotFound {
		t.Fatalf("section = %+v", section)
	}
	if google, bing, _ := env.searchConsole(t); google != nil || bing != nil {
		t.Fatalf("tokens set: google=%t bing=%t, want none", google != nil, bing != nil)
	}
}

// An old portal that cannot be read leaves the import OK and the section UNVERIFIED with the reason.
func TestImportSaysWhenTheOldPortalCannotBeRead(t *testing.T) {
	env := newImportEnv(t).withSolutions()
	env.setHelpCenter(t, true)
	report := env.linkRun(t)
	if report.Status != freshdesk.StatusOK {
		t.Fatalf("import status = %s", report.Status)
	}
	section := report.SearchConsole
	if section == nil || section.Status != freshdesk.StatusUnverified || len(section.Detail) == 0 || !strings.Contains(section.Detail[0], "404") {
		t.Fatalf("section = %+v", section)
	}
}

// A dry run writes nothing and never reads the portal.
func TestDryRunDoesNotReadTheOldPortal(t *testing.T) {
	env := newImportEnv(t).withSolutions()
	env.fake.portalPage = portalPage(`<meta name="google-site-verification" content="` + portalGoogle + `">`)
	report, err := env.run(t, context.Background(), func(options *freshdesk.Options) { options.DryRun = true })
	if err != nil {
		t.Fatal(err)
	}
	if report.SearchConsole != nil || env.fake.count("/support/home") != 0 {
		t.Fatalf("dry run read the portal: section=%+v requests=%d", report.SearchConsole, env.fake.count("/support/home"))
	}
}
