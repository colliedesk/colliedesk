// Package aiprovider calls a language model on behalf of a Colliedesk feature (triage suggestions,
// reply drafts, the help center's chat). The interface is at the level a feature needs: one request
// in, one answer out, whole or as it is written (Stream), no sampling knobs and no tool use. Each adapter translates it
// to its provider's wire format. Adapters never retry: one Complete or Check is one HTTP attempt,
// so the caller can meter every attempt (see docs/internals/ai.md).
package aiprovider

import (
	"context"
	"errors"
	"fmt"
	"time"
)

// Kind names an adapter. It is stored on a provider row, so values never change.
type Kind string

const (
	KindAnthropic        Kind = "anthropic"
	KindOpenAICompatible Kind = "openai_compatible"
)

// Effort is how much reasoning the model may spend. Adapters map it to their provider's control
// and always send it, because providers default to different levels.
type Effort string

const (
	EffortLow    Effort = "low"
	EffortMedium Effort = "medium"
	EffortHigh   Effort = "high"
)

// Valid says whether the effort is one of the three levels.
func (e Effort) Valid() bool { return e == EffortLow || e == EffortMedium || e == EffortHigh }

// Role is the author of a message in a request.
type Role string

const (
	RoleUser      Role = "user"
	RoleAssistant Role = "assistant"
)

// Message is one turn of the conversation sent to the model.
type Message struct {
	Role Role
	Text string
}

// Request is everything a feature sends. System holds Colliedesk's own instructions only; text
// written by a customer goes in Messages, wrapped by the feature.
type Request struct {
	Model     string
	Effort    Effort
	System    string
	Messages  []Message
	MaxOutput int
	// Schema, when set, asks for JSON matching this JSON Schema object (structured output).
	Schema map[string]any
	// SystemParts follow System, in order. A part marked Cache ends a prefix the provider may cache
	// (Claude: a cache_control breakpoint, five minutes); providers without a prompt cache join them.
	SystemParts []SystemPart
	// CacheHistory lets the provider cache the messages as well (Claude: automatic caching of the
	// last message), so the next turn of a conversation reads them back.
	CacheHistory bool
	// NoThinking turns the model's thinking off where the model allows it; elsewhere it is left out
	// and Effort alone bounds it.
	NoThinking bool
	// Warm asks the provider only to write the prompt's cache: no output, MaxOutput 0, never streamed.
	// Only a provider with a prompt cache takes it (Caches).
	Warm bool
}

// SystemPart is one block of system text.
type SystemPart struct {
	Text  string
	Cache bool
}

// SystemText is the whole system text: System and every part, as a provider without blocks reads it.
func (r Request) SystemText() string {
	text := r.System
	for _, part := range r.SystemParts {
		if text != "" && part.Text != "" {
			text += "\n\n"
		}
		text += part.Text
	}
	return text
}

// MaxOutputLimit is the largest output a request may ask for. Requests are not streamed, and the
// Claude SDK refuses a non-streaming request that could run past ten minutes (about 21,000 tokens).
const MaxOutputLimit = 16000

// Validate refuses a request no adapter can send. The last message must be the user's: providers
// refuse an assistant prefill.
func (r Request) Validate() error {
	switch {
	case r.Model == "":
		return errors.New("aiprovider: request has no model")
	case !r.Effort.Valid():
		return fmt.Errorf("aiprovider: effort %q is not low, medium or high", r.Effort)
	case len(r.Messages) == 0:
		return errors.New("aiprovider: request has no messages")
	case r.Messages[len(r.Messages)-1].Role != RoleUser:
		return errors.New("aiprovider: the last message must be the user's")
	case r.Warm && r.MaxOutput != 0:
		return errors.New("aiprovider: a request that warms the cache asks for no output")
	case !r.Warm && (r.MaxOutput <= 0 || r.MaxOutput > MaxOutputLimit):
		return fmt.Errorf("aiprovider: the output limit must be 1 to %d tokens", MaxOutputLimit)
	}
	for _, message := range r.Messages {
		if message.Role != RoleUser && message.Role != RoleAssistant {
			return fmt.Errorf("aiprovider: message role %q is not user or assistant", message.Role)
		}
	}
	return nil
}

// StopReason says why the model stopped.
type StopReason string

const (
	StopDone      StopReason = "done"
	StopMaxTokens StopReason = "max_tokens"
	StopRefused   StopReason = "refused"
	StopOther     StopReason = "other"
)

// Usage counts tokens as the provider reported them. Reported is false when the provider sent no
// usage at all (some OpenAI-compatible servers): that is "unknown", never zero.
type Usage struct {
	Reported         bool
	InputTokens      int64
	OutputTokens     int64
	CacheReadTokens  int64
	CacheWriteTokens int64
}

// Add sums two usages. The sum is reported when either part is.
func (u Usage) Add(other Usage) Usage {
	return Usage{
		Reported:         u.Reported || other.Reported,
		InputTokens:      u.InputTokens + other.InputTokens,
		OutputTokens:     u.OutputTokens + other.OutputTokens,
		CacheReadTokens:  u.CacheReadTokens + other.CacheReadTokens,
		CacheWriteTokens: u.CacheWriteTokens + other.CacheWriteTokens,
	}
}

// Hop is the usage of one model inside one attempt. An attempt has more than one hop when the
// provider re-ran a declined request on a fallback model; each hop bills at its own model's price.
type Hop struct {
	Model string
	Usage Usage
}

// Response is the provider's complete answer.
type Response struct {
	// Text is the answer as written. With a Schema it is the raw JSON text.
	Text string
	// JSON is Text when a Schema was asked for and Text parses as JSON; empty otherwise. A caller
	// that gets an empty JSON for a schema request decides whether another (metered) call is worth it.
	JSON string
	Stop StopReason
	// Model is the model that produced the answer, which can differ from the requested one.
	Model string
	// Usage is the total of Hops.
	Usage Usage
	// Hops always has at least one entry for a successful attempt.
	Hops []Hop
	// RequestID is the provider's id for the attempt, kept for reconciliation.
	RequestID string
}

// Provider is one configured provider: a kind, an endpoint and a credential.
type Provider interface {
	// Kind names the adapter.
	Kind() Kind
	// Complete sends one request and waits for the whole answer. The context bounds the call.
	Complete(ctx context.Context, request Request) (Response, error)
	// Check sends the smallest request that proves the endpoint, credential and model work. It is a
	// real, billed call: the caller meters it like any other. A max_tokens stop still passes.
	Check(ctx context.Context, model string) (Response, error)
	// Stream sends one request and hands each piece of the answer's text to onText as it is written,
	// in order, then returns the whole answer as Complete does. When the call fails after the
	// provider counted the prompt, the response holds the usage known so far (Usage.Reported), so a
	// call cut short is still metered.
	Stream(ctx context.Context, request Request, onText func(text string)) (Response, error)
}

// TokenCounter is a provider that counts a request's input tokens with its own tokenizer, free of
// charge (Claude's count_tokens).
type TokenCounter interface {
	CountTokens(ctx context.Context, request Request) (int64, error)
}

// CountTokens counts a request's input tokens: exactly when the provider can (exact), else at one
// token per 3 bytes, which no tokenizer this server meets undercounts by much.
func CountTokens(ctx context.Context, provider Provider, request Request) (tokens int64, exact bool, err error) {
	if counter, ok := provider.(TokenCounter); ok {
		tokens, err := counter.CountTokens(ctx, request)
		return tokens, err == nil, err
	}
	return EstimateTokens(request), false, nil
}

// EstimateTokens is a request's input at one token per 3 bytes. Characters ÷ 4 undercounts Claude's
// tokenizer by about 40% on help articles.
func EstimateTokens(request Request) int64 {
	size := len(request.SystemText())
	for _, message := range request.Messages {
		size += len(message.Text)
	}
	return int64(size/3 + 1)
}

// Caches says whether a provider keeps a prompt cache a request can warm (Request.Warm).
func Caches(provider Provider) bool { return provider.Kind() == KindAnthropic }

// ErrorCode is the provider-independent reason a call failed. Codes are stored and shown, so
// values never change.
type ErrorCode string

const (
	ErrorAuth            ErrorCode = "auth"             // the credential is wrong or revoked
	ErrorPermission      ErrorCode = "permission"       // the credential may not use this model or endpoint
	ErrorModelNotFound   ErrorCode = "model_not_found"  // the provider has no such model
	ErrorBadRequest      ErrorCode = "bad_request"      // the provider refused the request shape
	ErrorTooLarge        ErrorCode = "too_large"        // the request is over the provider's size limit
	ErrorRateLimited     ErrorCode = "rate_limited"     // retry after RetryAfter
	ErrorOverloaded      ErrorCode = "overloaded"       // the provider is busy; retry later
	ErrorQuotaExhausted  ErrorCode = "quota_exhausted"  // the provider account is out of credit
	ErrorTimeout         ErrorCode = "timeout"          // no answer before the deadline
	ErrorUnavailable     ErrorCode = "unavailable"      // a server error or no connection
	ErrorBlockedAddress  ErrorCode = "blocked_address"  // the base URL reaches a private network this server does not call
	ErrorInvalidResponse ErrorCode = "invalid_response" // the answer could not be read
)

// Error is a failed call. It never carries the provider's message, the response body, the prompt
// or the credential: those can echo customer text or secrets into logs.
type Error struct {
	Code       ErrorCode
	Status     int           // HTTP status, 0 when there was no response
	RetryAfter time.Duration // from the provider's retry-after header, when it sent one
	RequestID  string
}

func (e *Error) Error() string {
	if e.Status == 0 {
		return "ai provider call failed: " + string(e.Code)
	}
	return fmt.Sprintf("ai provider call failed: %s (HTTP %d)", e.Code, e.Status)
}

// Retryable says whether the same request may succeed later. Errors about the request or the
// account are not retryable.
func (e *Error) Retryable() bool {
	switch e.Code {
	case ErrorRateLimited, ErrorOverloaded, ErrorUnavailable, ErrorTimeout:
		return true
	}
	return false
}

// CodeOf returns the code of an *Error in err's chain, or ErrorUnavailable for any other error.
func CodeOf(err error) ErrorCode {
	var providerErr *Error
	if errors.As(err, &providerErr) {
		return providerErr.Code
	}
	return ErrorUnavailable
}
