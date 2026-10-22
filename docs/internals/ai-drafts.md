# AI drafts, triage and auto-answer

Decisions and traps behind `server/internal/aidraft`, `store/ai-drafts.go` and
`store/ai-draft-worker.go`. Issue #89. Providers: [ai.md](ai.md); what a draft may read:
[ai-retrieval.md](ai-retrieval.md).

## The flow

1. A customer message stored by inbound mail or the web form queues a row in `ai_drafts`
   (`queueAIDraft`, same transaction), on a server with a provider (`Store.WithAI`) in a workspace with
   drafts on. Imports never queue: they write messages through their own path. Open drafts of the
   conversation are superseded; a scheduled answer is cancelled.
2. The worker (`aidraft.Worker`, every 2 s from `serve`) lists the workspaces with work due under
   `PrivilegeAIDraftQueue` (ids only), sends the scheduled answers that are due, then claims queued
   drafts one at a time (`FOR UPDATE SKIP LOCKED` and a lease in `next_attempt_at`, so two servers never
   write the same draft). The claim reads, in the workspace's scope: the 12 newest public messages (never
   a note), the subject, the tags, retrieval's sources and the list of article titles.
3. The chooser (`select.go`, a small model) names up to four articles from the titles, unless the
   knowledge base was sent whole. They go first among at most six articles. It is metered as `select`;
   a failure leaves retrieval's sources.
4. One call writes the draft as JSON (`prompt.go`); `Parse` checks it; `FinishAIDraft` stores it and,
   in the same transaction, schedules auto-answer when the topic has earned it.
5. A reply with `ai_draft_id` records the outcome (`recordAIDraftUse` in `createMessage`): sent unchanged
   or edited, and the similarity. Any reply without it retires the conversation's open drafts.

The screen never waits for a model: drafts are written when the customer writes, and the web app reads
`GET .../ai-draft` (one indexed query) and polls only while a draft is queued.

## Confidence is the server's, not the model's

The model must list every statement of the reply with the id of the source that states it (`S1`...).
`Parse` rates a draft **high** only when the action is an answer, the model said high, every statement
cites a source that was really shown, at least one cited source is a knowledge base article, and the
model flagged none of the risks. A source id that was not shown makes it **low**. The titles, numbers and
ids shown to people come from the retrieved rows, never from the model's text. Triage keeps only tags
that exist (spelled as the workspace spells them) and a known priority.

The prompt's rules each fixed a mistake seen while measuring: a fact for one plan or product applies
only to it (ask when it matters); keep UI labels as the source writes them; never infer what a source
does not say; give every step and prerequisite of a procedure (the commonest "minor edit" before);
never tell a customer who is writing to support to contact support; never copy a commitment from a past
conversation; "normal" priority covers how-to questions (the model put them at "low").

## A second read of every high draft

A draft that `Parse` rates high gets one more call (`review.go`, the drafting model): the reviewer sees
only the customer's newest messages, the full text of the sources the reply cites, and the reply, and
answers whether a support lead would send it exactly as written, listing what is missing or unsupported.
A no, or a review that fails, lowers the draft to medium (`review_flagged`, `review_failed`) and puts
the list in `handoff_reason` for the person who sends it. Measuring showed why: the draft's own call
rated drafts high that left out a condition or caveat of the article (which extensions qualify, a
backup card PayPal needs, a form that expires); a rule in the prompt asking for them made things worse
(85 % from 89 %), a separate reader did not. The review is metered as `review`.

## Auto-answer is earned per article

A draft is sent on its own only when its topic (the first article it cites) has a record that meets the
workspace's bar: `auto_answer_min_reviewed` high-confidence drafts reviewed by a person, at least
`auto_answer_min_accuracy` percent of them sent with similarity >= 0.85 (`domain.AIAcceptedSimilarity`,
also written in `aiTopicRecordQuery`). Answers sent on their own and stopped ones never count, so the
record is people's judgement only. The check runs again when the answer is due: a topic whose record
dropped, or a conversation someone replied to, sends nothing and the draft stays for a person.
Turning drafts or auto-answer off returns every scheduled answer to `ready`. Inbound mail marked
`Auto-Submitted` is dropped before it is stored, and an answer sent on its own carries
`Auto-Submitted: auto-replied`, so auto-answer cannot loop with an autoresponder.

Auto-answer only answers the customer message the draft was written for, on an open conversation,
when that message is email that passed DMARC at the trusted relay (`email_meta.authentication='pass'`,
via the platform or the workspace's own channel). The confidence gate rests partly on what the model
says about itself, so a customer's text could try to talk it up; the address check keeps such a draft
from ever going out on its own to a typed or forged address, and a link or email address in a reply that
no source contains keeps it below high. The worker sends with the API's own store (`Server.AIStore`), so
an answer follows the same outbound policy and plan limits as a person's reply; a refused one returns to
a person with `error_code` `auto_answer_refused` and the others still go. It takes the conversation's
reply-number lock before the draft's row, in the same order as a person's reply, so the two never
deadlock.

A draft that a person passed over for a reply in their own words gets the outcome `ignored`, which
counts against its topic like a discard. A reply an agent token sends from a draft records no outcome:
the record is people's judgement only.

## Failures and metering

One provider call is one `ai_usage` row per model that ran (a fallback bills two), with the error code
when it failed; tokens are NULL when the provider did not report them. A retryable error (rate limit,
overload, timeout, server error), an answer that is not the JSON asked for, or a failure to store the
result is tried again after 30 s times the attempt, three attempts at most: a claim never takes a draft
that used its attempts, and fails it instead. Writes are fenced by the attempt, so a worker whose lease
ran out writes nothing over the next one's result. The lease (4 minutes) is longer than the chooser's
and the draft's timeouts together. Each workspace gets five drafts per pass before the next one's turn,
and a workspace may ask for 60 new drafts an hour by hand or through an agent token. A spending cap per
workspace is not built yet: it waits for the decision on the price of hosted AI usage.

## Measuring

`server/cmd/ai-eval` runs the production path on a scratch schema: a workspace with a real knowledge
base, one conversation per case, the worker and the provider, then a stronger model grades each draft
against the article that answers it: correct (sent as written), minor edit, wrong, handoff right or
handoff missed; and whether it would keep the triage. It reports precision of high-confidence drafts on
tickets the knowledge base answers, with a 95 % Wilson lower bound, and resumes where it stopped.

Results on the Hover help center (102 public articles, 15 held out of the knowledge base), Sonnet 5.5
drafting at low effort with Haiku 5.5 choosing, Opus 5.5 grading:

| Set | Rated high (of answerable) | Correct as written | Correct or minor edit | Wrong | Needs-a-person cases handed over | Triage kept |
|---|---|---|---|---|---|---|
| 138 cases (tuning set) | 49 of 95 | 45 (91.8 %) | 49 (100 %) | 0 | 28 of 28 | 94.2 % |
| 120 new cases written by another model (holdout) | 30 of 90 | 28 (93.3 %) | 30 (100 %) | 0 | 30 of 30 | 93.3 % |

Without the chooser, keyword retrieval missed the answering article for 31 of 95 questions and only 39
were rated high. Limits: the emails are model-written, the grader is a model, one run each.
