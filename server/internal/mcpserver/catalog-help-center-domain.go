package mcpserver

import "net/http"

const (
	helpCenterDomainPath = "/api/v1/help-center/domain"

	helpCenterDomainSchema = `{"type":"object","required":["hostname","status","records","checked_at","check_error"],"properties":{` +
		`"hostname":{"type":["string","null"]},"status":{"type":"string","enum":["none","pending","active"]},` +
		`"records":{"type":"array","items":{"type":"object","required":["type","name","value","purpose"],"properties":{` +
		`"type":{"type":"string","enum":["CNAME","A","AAAA","TXT"]},"name":{"type":"string"},"value":{"type":"string"},"purpose":{"type":"string","enum":["route_and_verify","route","verify"]}}}},` +
		`"created_at":{"type":["string","null"]},"verified_at":{"type":["string","null"]},"checked_at":{"type":["string","null"]},` +
		`"check_error":{"type":["string","null"]},"certificate_requested_at":{"type":["string","null"]},` +
		`"failing_since":{"type":["string","null"]},"pending_reason":{"type":["string","null"],"enum":["proof_lost","taken_over",null]}}}`
	helpCenterDomainOutput = `{"type":"object","required":["domain"],"properties":{"domain":` + helpCenterDomainSchema + `}}`

	helpCenterDomainRule = `The domain becomes active once DNS proves the workspace owns it: a CNAME of the host name to this claim's own target hc-<claim>.<HELP_CENTER_DOMAIN> ` +
		`shown in records (when the server has a help center domain; it never changes while the domain is set), or the TXT record _help-center-verify.<host name> ` +
		`holding the token shown in records. A or AAAA records never prove it. `
)

func helpCenterDomainGetSpec() Spec {
	return Spec{
		Name:  "colliedesk_help_center_domain_get",
		Title: "Read the help center's custom domain",
		Description: `Return the public help center's custom domain (such as help.example.com): status none, pending or active, the DNS records to set ` +
			`(records: type, name, value, purpose), when it was verified and last checked, and which record the last check did not find (check_error). ` +
			`An active domain whose checks fail shows failing_since; after 7 days without proof it goes back to pending with pending_reason proof_lost, ` +
			`or taken_over when another workspace proved the host name while this domain's records did not.` + "\n\nTier: read.",
		Tier: "read", ReadOnly: true,
		REST:   []string{"GET " + helpCenterDomainPath},
		Input:  noArguments,
		Output: helpCenterDomainOutput,
		run:    helpCenterDomainGet,
	}
}

func helpCenterDomainSetSpec() Spec {
	return Spec{
		Name:  "colliedesk_help_center_domain_set",
		Title: "Set the help center's custom domain",
		Description: `Set or replace the custom domain the public help center serves on. A new host name is pending with a new token and CNAME target; sending the current one again changes nothing. ` +
			helpCenterDomainRule + `Then set the records it returns and call colliedesk_help_center_domain_check. ` +
			`Refused: an IP address, a single label, the app's own host and names below the server's help center domain (validation_failed), ` +
			`A host name another workspace's help center serves on can be claimed: it waits, pending, until DNS proves this claim and no longer proves the other one. ` + belowAdmin + "\n\nTier: admin.",
		Tier: "admin", Idempotent: true,
		REST:   []string{"PUT " + helpCenterDomainPath},
		Input:  `{"type":"object","additionalProperties":false,"required":["hostname"],"properties":{"hostname":{"type":"string","minLength":1,"maxLength":253,"description":"A host name such as help.example.com; Unicode names are stored in punycode."}}}`,
		Output: helpCenterDomainOutput,
		run:    helpCenterDomainSet,
	}
}

func helpCenterDomainCheckSpec() Spec {
	return Spec{
		Name:  "colliedesk_help_center_domain_check",
		Title: "Check the help center's custom domain now",
		Description: `Look up the custom domain's DNS now and record the result. ` + helpCenterDomainRule +
			`A pending domain that passes becomes active; a failed check says which record to set in check_error. An active domain keeps serving while checks fail, ` +
			`and goes back to pending after 7 days without proof. When another workspace serves on the host name, a passing check takes it over if DNS no longer proves ` +
			`that workspace's claim, else domain_taken. One workspace may check 10 times an hour, people and agents together. ` + belowAdmin + "\n\nTier: admin.",
		Tier: "admin", Idempotent: true,
		REST:   []string{"POST " + helpCenterDomainPath + "/check"},
		Input:  noArguments,
		Output: helpCenterDomainOutput,
		run:    helpCenterDomainCheck,
	}
}

func helpCenterDomainRemoveSpec() Spec {
	return Spec{
		Name:  "colliedesk_help_center_domain_remove",
		Title: "Remove the help center's custom domain",
		Description: `Remove the custom domain: the help center stops serving on it at once and its slug address serves again. ` +
			`Setting it again needs a new check with a new CNAME target and TXT token. Removing when there is none changes nothing. ` + belowAdmin + "\n\nTier: admin.",
		Tier: "admin", Idempotent: true,
		REST:   []string{"DELETE " + helpCenterDomainPath},
		Input:  noArguments,
		Output: helpCenterDomainOutput,
		run:    helpCenterDomainRemove,
	}
}

func helpCenterDomainGet(c *call, _ map[string]any) (any, error) {
	var current map[string]any
	if err := c.get(helpCenterDomainPath, nil, &current); err != nil {
		return nil, err
	}
	return map[string]any{"domain": current}, nil
}

func helpCenterDomainSet(c *call, arguments map[string]any) (any, error) {
	return helpCenterDomainSend(c, http.MethodPut, helpCenterDomainPath, arguments)
}

func helpCenterDomainCheck(c *call, _ map[string]any) (any, error) {
	return helpCenterDomainSend(c, http.MethodPost, helpCenterDomainPath+"/check", nil)
}

func helpCenterDomainRemove(c *call, _ map[string]any) (any, error) {
	return helpCenterDomainSend(c, http.MethodDelete, helpCenterDomainPath, nil)
}

func helpCenterDomainSend(c *call, method, path string, body any) (any, error) {
	var current map[string]any
	if err := c.send(method, path, body, &current); err != nil {
		return nil, err
	}
	return map[string]any{"domain": current}, nil
}
