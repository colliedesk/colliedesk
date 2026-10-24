package aiprovider

import (
	"context"
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
}

// FakeReply is one scripted answer: a response or an error.
type FakeReply struct {
	Response Response
	Err      error
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

func (f *Fake) Kind() Kind { return KindOpenAICompatible }

func (f *Fake) Check(ctx context.Context, model string) (Response, error) {
	return f.Complete(ctx, checkRequest(model))
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
	text := "fake answer"
	if request.Schema != nil {
		text = `{"ok":true}`
	}
	usage := Usage{Reported: true, InputTokens: 10, OutputTokens: 5}
	return Response{
		Text: text, JSON: schemaJSON(request, text), Stop: StopDone, Model: request.Model,
		Usage: usage, Hops: []Hop{{Model: request.Model, Usage: usage}}, RequestID: "fake",
	}, nil
}
