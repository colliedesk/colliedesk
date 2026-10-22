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

// Why an active custom domain went back to pending: its DNS did not prove the claim for
// HelpCenterDomainRelease, or another workspace proved the host name while this claim's records did not.
const (
	HelpCenterDomainProofLost = "proof_lost"
	HelpCenterDomainTakenOver = "taken_over"
)

// HelpCenterDomainRelease is how long an active custom domain keeps serving while every DNS check of it
// fails; the first check that fails after that puts it back to pending.
const HelpCenterDomainRelease = 7 * 24 * time.Hour

// MaxHelpCenterDomainCheckError bounds the stored text of a failed DNS check.
const MaxHelpCenterDomainCheckError = 500

// HelpCenterDomain is a workspace's custom help center host name (a claim) and its last DNS check.
// Token is the value of its TXT record; it is shown to the workspace's own people and never exported
// or logged. Claim names this claim: its CNAME target is hc-<claim>.<HELP_CENTER_DOMAIN>, made with the
// claim and never changed while it exists.
type HelpCenterDomain struct {
	Hostname               string
	Claim                  string
	Token                  string
	Status                 string
	CreatedAt              time.Time
	VerifiedAt             *time.Time
	CheckedAt              *time.Time
	CheckError             *string
	CertificateRequestedAt *time.Time
	// FailingSince is the first failed check of an active domain since the last one that passed.
	FailingSince *time.Time
	// PendingReason says why an active domain went back to pending (HelpCenterDomainProofLost,
	// HelpCenterDomainTakenOver); nil otherwise.
	PendingReason *string
}

// HelpCenterDomainProof is what DNS showed for a host name in one check: the claim its CNAME chain
// ends at ("" when it ends anywhere else) and the values of its TXT record. A claim is proven by
// either; the store compares it with claims it does not hand out (another workspace's).
type HelpCenterDomainProof struct {
	Claim  string
	Tokens []string
}

var (
	// ErrHelpCenterDomainTaken: another workspace's help center already serves on the host name.
	ErrHelpCenterDomainTaken = errors.New("another workspace's help center already serves on this host name")
	// ErrHelpCenterDomainMissing: the workspace has no custom domain, or it changed during a check.
	ErrHelpCenterDomainMissing = errors.New("no custom domain is set")
)
