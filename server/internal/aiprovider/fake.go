package aiprovider

import (
	"context"
	"strings"
	"sync"
)

// Fake is a provider for tests and local development. It records every request it receives, so a
// test can assert what left Colliedesk (for example, that no prompt holds another workspace's
// text), and answers from a script. It never makes a network call. Safe for concurrent use.
type Fake struct {
	mu       sync.Mutex
	requests []Request
	script   []FakeReply
	// Answer, when set, answers every request the script does not cover.
	Answer func(Request) (Response, error)
	// FakeModels is what Models lists; ModelsErr, when set, is what it fails with.
	FakeModels []string
	ModelsErr  error
	// FakeKind is what Kind reports; empty is KindOpenAICompatible.
	FakeKind Kind
}

// FakeReply is one scripted answer: a response or an error.
type FakeReply struct {
	Response Response
	Err      error
	// Deltas are the pieces Stream hands out, in order; Response.Text defaults to their join. A
	// stream with an Err fails after its deltas, with Response as what it had answered.
	Deltas []string
	// Hold, when set, is waited for after the first delta: a test cuts the call there.
	Hold <-chan struct{}
}

// Script queues answers; each call takes the next one.
func (f *Fake) Script(replies ...FakeReply) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.script = append(f.script, replies...)
}

// Requests returns a copy of every request received, in order.
func (f *Fake) Requests() []Request {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]Request(nil), f.requests...)
}

func (f *Fake) Kind() Kind {
	if f.FakeKind != "" {
		return f.FakeKind
	}
	return KindOpenAICompatible
}

func (f *Fake) Check(ctx context.Context, model string) (Response, error) {
	return f.Complete(ctx, checkRequest(model))
}

// Stream records the request like Complete and hands out the scripted reply's deltas; a reply without
// deltas is streamed as one piece. A context canceled before a delta fails the call there, with the
// usage of the scripted response kept, as a provider reports usage once it read the prompt.
func (f *Fake) Stream(ctx context.Context, request Request, onText func(text string)) (Response, error) {
	if err := request.Validate(); err != nil {
		return Response{}, err
	}
	f.mu.Lock()
	f.requests = append(f.requests, request)
	var reply FakeReply
	scripted := len(f.script) > 0
	if scripted {
		reply = f.script[0]
		f.script = f.script[1:]
	}
	answer := f.Answer
	f.mu.Unlock()
	if !scripted {
		var err error
		if answer != nil {
			reply.Response, err = answer(request)
		} else {
			reply.Response, err = f.fixed(request), nil
		}
		reply.Err = err
	}
	deltas := reply.Deltas
	if deltas == nil && reply.Response.Text != "" {
		deltas = []string{reply.Response.Text}
	}
	if reply.Response.Text == "" {
		reply.Response.Text = strings.Join(deltas, "")
	}
	for i, delta := range deltas {
		if i == 1 && reply.Hold != nil {
			select {
			case <-reply.Hold:
			case <-ctx.Done():
			}
		}
		if err := ctx.Err(); err != nil {
			partial := reply.Response
			partial.Text, partial.Stop = strings.Join(deltas[:i], ""), StopOther
			return partial, transportError(ctx, err)
		}
		onText(delta)
	}
	return reply.Response, reply.Err
}

// Complete records the request and answers with the next scripted reply, then Answer, then a
// fixed text. A canceled context fails like a real call would.
func (f *Fake) Complete(ctx context.Context, request Request) (Response, error) {
	if err := request.Validate(); err != nil {
		return Response{}, err
	}
	f.mu.Lock()
	f.requests = append(f.requests, request)
	var reply *FakeReply
	if len(f.script) > 0 {
		reply = &f.script[0]
		f.script = f.script[1:]
	}
	answer := f.Answer
	f.mu.Unlock()

	if err := ctx.Err(); err != nil {
		return Response{}, transportError(ctx, err)
	}
	switch {
	case reply != nil:
		return reply.Response, reply.Err
	case answer != nil:
		return answer(request)
	}
	return f.fixed(request), nil
}

// fixed is the answer of a request nothing scripted.
func (f *Fake) fixed(request Request) Response {
	text := "fake answer"
	if request.Schema != nil {
		text = `{"ok":true}`
	}
	usage := Usage{Reported: true, InputTokens: 10, OutputTokens: 5}
	return Response{
		Text: text, JSON: schemaJSON(request, text), Stop: StopDone, Model: request.Model,
		Usage: usage, Hops: []Hop{{Model: request.Model, Usage: usage}}, RequestID: "fake",
	}
}
