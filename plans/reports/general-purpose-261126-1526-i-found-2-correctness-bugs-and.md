I found 2 correctness bugs and 1 edge case. Nothing was edited. I traced the logic by reading the diff and did not run the tests.

1. **The Solo fit line names the Freshdesk team**, at `web/src/features/settings/plan-fit-line.ts:20`.
   - **Scenario:** the visitor comes with a Team 25 intent, Freshdesk has 3 agents and the workspace has 1 seat.
   - `fitsTeam('solo')` passes because it checks seats (1). The `pick === 'intent'` branch then prints `team`, which is built from `size` (Freshdesk).
   - The Solo card reads "Your Freshdesk has 3 agents. Solo also fits." This is the exact case you asked about. For Solo, the line should be built from `fitSeats(recommendation.seats)`.

2. **The recommendation ranks Solo by the Freshdesk count, not seats**, at `web/src/features/settings/plan-recommendation.ts:42`.
   - The `bySize` search uses `fitsPlan(candidate, size.count)`.
   - **Scenario:** Freshdesk has 1 agent, but the workspace has 2 seats (1 agent and 1 pending invite). Solo is filtered out of the offer because seats > 1, so that case is safe.
   - In the full ladder after Founding closes, Solo shows for any seat count. With `freshdeskAgents=1` and `seats=3`, Solo leads with "fits" text. But `isGreyed` greys it (seats 3 > 1), so the lead card is greyed. The check should use `fitsTeam` with seats for Solo.

3. **The fallback ladder can count as the Founding offer**, at `web/src/features/settings/plan-options.ts:55` and `:62`.
   - **Scenario:** the server sells only Founding and Solo, Founding is refused, and the workspace has 3 seats. Then `offer=[founding]` is not buyable, so `shown = cards = [solo, founding]`.
   - `isFoundingOffer` still returns true because `foundingOfferIds` now contains `solo`. The picker then orders and labels these cards as the Founding offer, not the ladder, and shows Solo to a 3-seat team.

`plan-price-line.ts:19` calls `planCardsFor([plan])` without `teamSize`. That is harmless: with one plan sellable, it gets the single card.

**Status:** DONE_WITH_CONCERNS
**Summary:** Two real bugs: the Solo fit line uses Freshdesk wording, and the ladder recommendation ranks Solo by the Freshdesk count. One edge case where `isFoundingOffer` matches the fallback ladder.
**Concerns/Blockers:** None. Static review only; no tests were run.