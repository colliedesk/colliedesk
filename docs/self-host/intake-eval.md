# Score AI intake on your own cases

AI intake on a chat channel acts on its own only for an action its labels show it gets right (see
[What AI does by itself, per channel](../user/ai.md#when-ai-acts-on-its-own)). Before a channel has
labels of its own, an operator can score intake on cases people already decided, and record the result
so it counts towards the channel's readiness.

```sh
docker compose exec server /app/collie intake-eval \
  --workspace <workspace id> --channel <chat channel id> --scope group \
  --dataset /data/cases.jsonl [--model <another model of intake's provider>] \
  [--thresholds 0.85,0.90,0.95] [--record]
```

1. It runs the same prompt and answer reader AI intake uses, with the workspace's intake provider and
   model and the channel scope's options (words to hide, patterns to mask, instructions). Each call is
   metered like any intake call, as asked by a person, and counts in the workspace's AI usage.
2. Before calling the model it prints how many cases and calls it will make. The result is JSON on
   standard output: per action (`create`, `attach`, `ignore`) the labelled cases, how many it got right
   and its severe and light errors; how labels and answers were confused; how many it would have applied
   on its own at the thresholds (the scope's unless `--thresholds` says) and how many of those were wrong;
   the calls' latency.
3. `--record` keeps the counts for the scope, the model and this server's prompt version. A newer run of
   the same dataset replaces the older one, and a run counts for nothing once intake moves to another
   model or the server to another prompt version. The same is
   `POST /api/v1/channels/{id}/ai/intake/evaluations` (an agent token's record waits for a person).

## The dataset

One case a line, already anonymised:

```json
{"place": "group",
 "before": [{"role": "staff", "sender": "Hoa", "text": "Which store?"}],
 "items": [{"role": "customer", "sender": "Lan", "text": "Store 12, the export still fails",
            "quote": {"role": "customer", "sender": "Lan", "text": "the stock export fails"}}],
 "candidates": [{"ref": "170", "reporter": "Lan", "subject": "Stock export fails", "status": "open",
                 "updated_at": "2026-10-20T03:00:00Z", "last_message": "We are looking at it"}],
 "commands": ["open"],
 "label": {"action": "attach", "target_ref": "170"}}
```

`items` are the messages to decide, `before` the ones before them in the place, `candidates` the
conversations they may join (by a reference of your own), and `label` what a person decided: `create`,
`attach` with `target_ref`, or `ignore`. A case whose label names a conversation it does not offer is left
out: AI could never get it right.

Severe errors are the ones that keep a gate closed: an `attach` of a new request (it hides it in an old
conversation) and an `ignore` of a request. Other wrong answers are light errors.
