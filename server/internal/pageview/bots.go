package pageview

import "strings"

// botMarks are the parts of a user agent, lower case, that only bots, crawlers, link previews,
// monitors and scripts send. A word ending in "bot" (Googlebot, bingbot, GPTBot, Slackbot) is a bot
// too, except the words in notBots. One list for every help center counter: add a bot here, not in
// a consumer.
var botMarks = []string{
	"crawl", "spider", "slurp", "scrape", "headless", "lighthouse", "pagespeed", "phantomjs", "selenium", "puppeteer",
	"facebookexternalhit", "facebookcatalog", "meta-externalagent", "whatsapp", "skypeuripreview", "embedly", "preview",
	"mediapartners-google", "google-inspectiontool", "googleother", "google-extended", "feedfetcher", "apis-google",
	"chatgpt-user", "claude-user", "claude-web", "anthropic-ai", "cohere-ai", "perplexity", "bytespider", "yandex",
	"monitor", "uptime", "pingdom", "statuscake", "checkly",
	"curl/", "wget/", "httpie/", "python-requests", "python-urllib", "aiohttp", "httpx", "go-http-client", "java/",
	"okhttp", "axios/", "node-fetch", "undici", "libwww", "lwp-", "scrapy", "postman", "insomnia",
}

// notBots are words that end in "bot" in browsers' user agents: Cubot phones.
var notBots = map[string]bool{"cubot": true}

// IsBot reports whether a user agent is a known bot, crawler, link preview, monitor or script, or
// empty.
func IsBot(userAgent string) bool {
	agent := strings.ToLower(strings.TrimSpace(userAgent))
	if agent == "" {
		return true
	}
	for _, mark := range botMarks {
		if strings.Contains(agent, mark) {
			return true
		}
	}
	for _, word := range strings.FieldsFunc(agent, func(r rune) bool { return (r < 'a' || r > 'z') && (r < '0' || r > '9') }) {
		if strings.HasSuffix(word, "bot") && !notBots[word] {
			return true
		}
	}
	return false
}
