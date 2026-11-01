package zalo

import (
	"context"
	"html"
	"regexp"
	"strings"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/channels"
)

// VerificationDir is the directory below /api/v1 whose URL prefix a team verifies in its Zalo app
// (Domain verification › URL prefix, HTML file method): it covers the webhook, so a team on a host name
// it cannot change (Colliedesk Cloud) still sets the webhook. Zalo fetches zalo_verifier<code>.html in it.
const VerificationDir = "/channels/zalo"

// MaxSiteVerificationPaste bounds what may be pasted for a code (the whole file fits).
const MaxSiteVerificationPaste = 2000

var (
	siteCode = regexp.MustCompile(`^[A-Za-z0-9_-]{8,100}$`)
	siteFile = regexp.MustCompile(`^zalo_verifier([A-Za-z0-9_-]{8,100})\.html$`)
	// The file's name, with or without .html, alone or at the end of its address.
	pastedFile   = regexp.MustCompile(`zalo_verifier([A-Za-z0-9_-]+)(?:\.html)?$`)
	pastedMeta   = regexp.MustCompile(`(?is)<meta[^>]*zalo-platform-site-verification[^>]*>`)
	metaContent  = regexp.MustCompile(`(?is)\bcontent\s*=\s*["']([^"']*)["']`)
	pastedRecord = regexp.MustCompile(`(?i)^zalo-platform-site-verification\s*=\s*"?([^"\s]*)"?$`)
)

// SiteVerificationCode reads Zalo's site verification code from what a person pasted: the code, the TXT
// value (zalo-platform-site-verification=<code>), the meta tag, the file's name or address, or the whole
// file. ok is false when no code of Zalo's shape (letters, digits, _ and -, 8 to 100) is in it.
func SiteVerificationCode(pasted string) (string, bool) {
	pasted = strings.TrimSpace(pasted)
	if len(pasted) > MaxSiteVerificationPaste {
		return "", false
	}
	code := pasted
	if meta := pastedMeta.FindString(pasted); meta != "" {
		if content := metaContent.FindStringSubmatch(meta); content != nil {
			code = strings.TrimSpace(html.UnescapeString(content[1]))
		}
	} else if file := pastedFile.FindStringSubmatch(pasted); file != nil {
		code = file[1]
	} else if record := pastedRecord.FindStringSubmatch(pasted); record != nil {
		code = record[1]
	}
	if !siteCode.MatchString(code) {
		return "", false
	}
	return code, true
}

// SiteVerificationFileCode is the code of a verification file's name, zalo_verifier<code>.html.
func SiteVerificationFileCode(name string) (string, bool) {
	match := siteFile.FindStringSubmatch(name)
	if match == nil {
		return "", false
	}
	return match[1], true
}

// SiteVerificationFile is the file Zalo's checker expects for a code, byte for byte as Zalo hands it out.
func SiteVerificationFile(code string) []byte {
	return []byte(`<!DOCTYPE html><html lang="en"><head><meta property="zalo-platform-site-verification" content="` + code +
		`" /></head><body>There Is No Limit To What You Can Accomplish Using Zalo!</body></html>`)
}

// verificationFile answers zalo_verifier<code>.html while a live channel of the server holds the code.
func (a *Adapter) verificationFile(kit channels.RouteKit) func(ctx context.Context, name string) (channels.PublicFile, bool, error) {
	return func(ctx context.Context, name string) (channels.PublicFile, bool, error) {
		code, ok := SiteVerificationFileCode(name)
		if !ok {
			return channels.PublicFile{}, false, nil
		}
		held, err := kit.SiteVerificationHeld(ctx, code)
		if err != nil || !held {
			return channels.PublicFile{}, false, err
		}
		// A short cache: a code cleared stops being served within a minute.
		return channels.PublicFile{ContentType: "text/html; charset=utf-8", Body: SiteVerificationFile(code), MaxAge: time.Minute}, true, nil
	}
}
