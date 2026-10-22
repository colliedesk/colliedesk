package ai

import (
	"fmt"
	"math"
	"strconv"
	"strings"

	"github.com/colliedesk/colliedesk/server/internal/aiprovider"
	"github.com/colliedesk/colliedesk/server/internal/domain"
)

// Price is what one million tokens of a model cost, in US dollars, as the operator declares it
// (AI_SERVER_PRICES). Prices are never in the code: they change, and a refusal fallback can bill a
// model the operator did not pick.
type Price struct {
	Input, Output, CacheRead, CacheWrite float64
}

// Allowance is how much the server's provider may spend for one workspace in a UTC calendar month
// (AI_SERVER_MONTHLY_ALLOWANCE), in US dollars: a flat amount, or an amount per active agent with
// another for a workspace on its trial (Colliedesk Cloud: 3 per agent, 2 for a trial).
type Allowance struct {
	PerWorkspace float64
	PerAgent     float64
	Trial        float64
}

// For is the month's allowance in micro-dollars for a workspace with this plan and agent count.
func (a *Allowance) For(access domain.Access, activeAgents int) int64 {
	dollars := a.PerWorkspace
	switch {
	case a.Trial > 0 && access.Plan == domain.PlanTrial:
		dollars = a.Trial
	case a.PerAgent > 0:
		dollars = a.PerAgent * float64(max(activeAgents, 1))
	}
	return int64(math.Round(dollars * 1e6))
}

func parsePrices(value string) (map[string]Price, error) {
	prices := map[string]Price{}
	if strings.TrimSpace(value) == "" {
		return prices, nil
	}
	for _, entry := range strings.Split(value, ",") {
		model, list, ok := strings.Cut(strings.TrimSpace(entry), "=")
		model = strings.TrimSpace(model)
		parts := strings.Split(list, ":")
		if !ok || model == "" || len(parts) < 2 || len(parts) > 4 {
			return nil, fmt.Errorf("AI_SERVER_PRICES must be written model=input:output[:cache_read[:cache_write]] in US dollars per million tokens, separated by commas")
		}
		var numbers [4]float64
		for index, part := range parts {
			number, err := strconv.ParseFloat(strings.TrimSpace(part), 64)
			if err != nil || number < 0 || math.IsNaN(number) || math.IsInf(number, 0) {
				return nil, fmt.Errorf("AI_SERVER_PRICES has a price for %s that is not a number of dollars", model)
			}
			numbers[index] = number
		}
		if _, seen := prices[model]; seen {
			return nil, fmt.Errorf("AI_SERVER_PRICES lists %s twice", model)
		}
		prices[model] = Price{Input: numbers[0], Output: numbers[1], CacheRead: numbers[2], CacheWrite: numbers[3]}
	}
	return prices, nil
}

func parseAllowance(value string) (*Allowance, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil, nil
	}
	allowance := &Allowance{}
	if amount, err := strconv.ParseFloat(value, 64); err == nil {
		if amount < 0 || math.IsNaN(amount) || math.IsInf(amount, 0) {
			return nil, fmt.Errorf("AI_SERVER_MONTHLY_ALLOWANCE must be a number of dollars")
		}
		allowance.PerWorkspace = amount
		return allowance, nil
	}
	for _, entry := range strings.Split(value, ",") {
		key, number, ok := strings.Cut(strings.TrimSpace(entry), "=")
		amount, err := strconv.ParseFloat(strings.TrimSpace(number), 64)
		if !ok || err != nil || amount < 0 || math.IsNaN(amount) || math.IsInf(amount, 0) {
			return nil, fmt.Errorf("AI_SERVER_MONTHLY_ALLOWANCE must be dollars per workspace (10) or per agent with a trial amount (agent=3,trial=2)")
		}
		switch strings.TrimSpace(key) {
		case "agent":
			allowance.PerAgent = amount
		case "trial":
			allowance.Trial = amount
		case "workspace":
			allowance.PerWorkspace = amount
		default:
			return nil, fmt.Errorf("AI_SERVER_MONTHLY_ALLOWANCE knows workspace, agent and trial, not %q", key)
		}
	}
	return allowance, nil
}

// costMicroUSD prices every hop of an answer at its own model's price. A model without a declared
// price (a refusal fallback the operator did not list) is charged the highest declared price of each
// kind, so an allowance is never exceeded by a model nobody priced; known is false then.
func costMicroUSD(prices map[string]Price, hops []aiprovider.Hop) (cost int64, known bool) {
	known = true
	var highest Price
	for _, price := range prices {
		highest = Price{Input: max(highest.Input, price.Input), Output: max(highest.Output, price.Output),
			CacheRead: max(highest.CacheRead, price.CacheRead), CacheWrite: max(highest.CacheWrite, price.CacheWrite)}
	}
	total := 0.0
	for _, hop := range hops {
		price, ok := prices[hop.Model]
		if !ok {
			price, known = highest, false
		}
		total += float64(hop.Usage.InputTokens)*price.Input + float64(hop.Usage.OutputTokens)*price.Output +
			float64(hop.Usage.CacheReadTokens)*price.CacheRead + float64(hop.Usage.CacheWriteTokens)*price.CacheWrite
	}
	return int64(math.Ceil(total)), known
}
