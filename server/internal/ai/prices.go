package ai

import (
	"errors"
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

// Allowance is how many credits the server's provider gives one workspace (AI_SERVER_MONTHLY_CREDITS):
// PerWorkspace each UTC calendar month, or Trial for the whole trial when it is set. One credit is one
// result: a reply draft, a triage suggestion or an intake decision, whatever the model costs.
type Allowance struct {
	PerWorkspace int64
	Trial        int64
}

// For is the number of credits of a workspace with this plan.
func (a *Allowance) For(access domain.Access) int64 {
	if a.Trial > 0 && access.Plan == domain.PlanTrial {
		return a.Trial
	}
	return a.PerWorkspace
}

// The cache prices of a model whose AI_SERVER_PRICES entry leaves them out, as shares of its input price.
const (
	cacheReadShare  = 0.1
	cacheWriteShare = 1.25
)

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
		// A cache price left out is the providers' usual share of the input price (a read a tenth, a
		// write of five minutes a quarter more), never free: a limit in dollars must count it.
		if len(parts) < 3 {
			numbers[2] = numbers[0] * cacheReadShare
		}
		if len(parts) < 4 {
			numbers[3] = numbers[0] * cacheWriteShare
		}
		prices[model] = Price{Input: numbers[0], Output: numbers[1], CacheRead: numbers[2], CacheWrite: numbers[3]}
	}
	return prices, nil
}

const creditsFormat = "ai.server.monthly_credits must be a whole number of credits a month (1000), with a trial's own number if it has one (1000,trial=500)"

func parseAllowance(value string) (*Allowance, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil, nil
	}
	allowance := &Allowance{}
	for index, entry := range strings.Split(value, ",") {
		key, number, ok := strings.Cut(strings.TrimSpace(entry), "=")
		if !ok {
			key, number = "workspace", key
			if index > 0 {
				return nil, errors.New(creditsFormat)
			}
		}
		amount, err := strconv.ParseInt(strings.TrimSpace(number), 10, 64)
		if err != nil || amount < 0 {
			return nil, errors.New(creditsFormat)
		}
		switch strings.TrimSpace(key) {
		case "workspace":
			allowance.PerWorkspace = amount
		case "trial":
			allowance.Trial = amount
		default:
			return nil, errors.New(creditsFormat)
		}
	}
	return allowance, nil
}

// estimateMicroUSD is the most a call of model with that many tokens in and out can cost: a model
// without a declared price, like a refusal fallback, at the highest declared prices.
func estimateMicroUSD(prices map[string]Price, model string, input, output int64) int64 {
	price, ok := prices[model]
	if !ok {
		for _, each := range prices {
			price = Price{Input: max(price.Input, each.Input), Output: max(price.Output, each.Output)}
		}
	}
	return int64(math.Ceil(float64(input)*price.Input + float64(output)*price.Output))
}

// costMicroUSD prices every hop of an answer at its own model's price. A model without a declared
// price (a refusal fallback the operator did not list) is charged the highest declared price of each
// kind, so a spending limit is never exceeded by a model nobody priced; known is false then.
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
