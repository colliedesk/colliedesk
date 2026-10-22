package domain

import (
	"errors"
	"time"
)

// The states of a help center's custom domain: none set, set and waiting for a DNS check to prove the
// workspace owns it, and serving the help center.
const (
	HelpCenterDomainNone    = "none"
	HelpCenterDomainPending = "pending"
	HelpCenterDomainActive  = "active"
)

// MaxHelpCenterDomainCheckError bounds the stored text of a failed DNS check.
const MaxHelpCenterDomainCheckError = 500

// HelpCenterDomain is a workspace's custom help center host name and its last DNS check. Token is the
// value of its TXT record; it is shown to the workspace's own people and never exported or logged.
type HelpCenterDomain struct {
	Hostname               string
	Token                  string
	Status                 string
	CreatedAt              time.Time
	VerifiedAt             *time.Time
	CheckedAt              *time.Time
	CheckError             *string
	CertificateRequestedAt *time.Time
	// WorkspaceSlug is the workspace's slug, whose <slug>.<HELP_CENTER_DOMAIN> a CNAME may point at.
	WorkspaceSlug string
}

var (
	// ErrHelpCenterDomainTaken: another workspace's help center already serves on the host name.
	ErrHelpCenterDomainTaken = errors.New("another workspace's help center already serves on this host name")
	// ErrHelpCenterDomainMissing: the workspace has no custom domain, or it changed during a check.
	ErrHelpCenterDomainMissing = errors.New("no custom domain is set")
)
