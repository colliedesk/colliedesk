package mcpserver

import "net/http"

const (
	themeColorSchema = `{"type":["string","null"],"pattern":"^#[0-9a-fA-F]{6}$"}`
	themeFontSchema  = `{"type":["string","null"],"enum":["system-sans","humanist-sans","geometric-sans","classic-serif","transitional-serif","rounded-sans","mono",null]}`
	themeColors      = `"colors":{"type":"object","additionalProperties":false,"description":"Each #rrggbb or null for the default; any color set makes the pages light only.","properties":{` +
		`"primary":` + themeColorSchema + `,"header":` + themeColorSchema + `,"header_text":` + themeColorSchema + `,"background":` + themeColorSchema + `,` +
		`"text":` + themeColorSchema + `,"link":` + themeColorSchema + `,"footer":` + themeColorSchema + `}}`

	themeInputSchema = `{"type":"object","additionalProperties":false,"properties":{` + themeInputProperties + `}}`
	// The check takes the theme and, to render a page with it, preview.
	themeCheckInputSchema = `{"type":"object","additionalProperties":false,"properties":{` + themeInputProperties +
		`,"preview":{"type":"string","enum":["home","article"],"description":"Also render this page of the help center with the theme, as the settings card shows it: html is a whole document with its own policy, the help center style sheet linked under /help/_/, the brand inline and no script."}}}`
	themeInputProperties = `"logo_media_id":{"type":["string","null"],"description":"An image of the media library (colliedesk_kb_media_list), outside its trash unless it is the logo the theme already has."},` +
		`"favicon_media_id":{"type":["string","null"],"description":"An image of the media library for the browser tab."},` +
		`"logo_alt":{"type":["string","null"],"maxLength":200,"description":"What the logo says, for screen readers; empty uses the title."},` +
		`"linkback_url":{"type":["string","null"],"maxLength":2000,"description":"Your website, absolute http or https."},` +
		`"contact_url":{"type":["string","null"],"maxLength":2000,"description":"An https address, or mailto: and one plain email address (no name, no ?subject= or ?bcc=, no %-encoding)."},` +
		`"contact_phone":{"type":["string","null"],"maxLength":40,"description":"Digits, spaces and + - ( )."},` +
		themeColors + `,"body_font":` + themeFontSchema + `,"heading_font":` + themeFontSchema +
		`,"custom_css":{"type":["string","null"],"maxLength":32768,"description":"CSS served after the colors and fonts on every public page; empty or null is none. At most 32,768 bytes, stored and served exactly as sent. ` +
		`Refused with its line and column when it could load anything or leave its style sheet: url() and image functions, expression(), behavior, -moz-binding, at-rules other than @media, @supports, @keyframes, @layer and @container, ` +
		`a backslash outside quotes, </, <!--, unbalanced brackets, unclosed strings or comments. The contrast checks do not cover it."}`

	themeCheckSchema = `{"type":"object","required":["pair","field","foreground","background","ratio","required","passes","suggestion"],"properties":{` +
		`"pair":{"type":"string"},"field":{"type":"string"},"foreground":{"type":"string"},"background":{"type":"string"},` +
		`"ratio":{"type":"number"},"required":{"type":"number"},"passes":{"type":"boolean"},"suggestion":{"type":["string","null"]}}}`

	themeSchema = `{"type":"object","required":["colors","content_version","checks"],"properties":{` +
		`"logo_media_id":{"type":["string","null"]},"favicon_media_id":{"type":["string","null"]},"logo_alt":{"type":"string"},` +
		`"linkback_url":{"type":["string","null"]},"contact_url":{"type":["string","null"]},"contact_phone":{"type":["string","null"]},` +
		themeColors + `,"body_font":` + themeFontSchema + `,"heading_font":` + themeFontSchema + `,"custom_css":{"type":"string"},` +
		`"content_version":{"type":"integer"},"updated_at":{"type":["string","null"]},"checks":{"type":"array","items":` + themeCheckSchema + `}}}`

	themeContrastRule = `Every color pair must meet WCAG AA: 4.5:1 for text, links, header text, button text on the primary color and footer text, 3:1 for the primary color against the background. `
)

func helpCenterThemeGetSpec() Spec {
	return Spec{
		Name:  "colliedesk_help_center_theme_get",
		Title: "Read the help center's brand",
		Description: `Return the brand of the public help center: logo and favicon (media library ids), logo text, website and contact links, colors, fonts and custom CSS, ` +
			`with the contrast of every color pair (checks). A null color or font is the default look.` + "\n\nTier: read.",
		Tier: "read", ReadOnly: true,
		REST:   []string{"GET /api/v1/help-center/theme"},
		Input:  noArguments,
		Output: `{"type":"object","required":["theme"],"properties":{"theme":` + themeSchema + `}}`,
		run:    helpCenterThemeGet,
	}
}

func helpCenterThemeCheckSpec() Spec {
	return Spec{
		Name:  "colliedesk_help_center_theme_check",
		Title: "Check a help center brand without saving it",
		Description: `Check a candidate brand the way colliedesk_help_center_theme_update would, and save nothing. ` + themeContrastRule +
			`ok is false when a pair fails; fields then says, per color, its ratio and the nearest color that passes. A field of the wrong shape or a logo that is not a library image is an error. ` +
			`With preview (home or article), html also holds that page rendered with the theme from the workspace's published articles, the way the settings card shows it.` + "\n\nTier: read.",
		Tier: "read", ReadOnly: true,
		REST:   []string{"POST /api/v1/help-center/theme/check", "POST /api/v1/help-center/theme/preview"},
		Input:  themeCheckInputSchema,
		Output: `{"type":"object","required":["ok","checks","fields"],"properties":{"ok":{"type":"boolean"},"checks":{"type":"array","items":` + themeCheckSchema + `},"fields":{"type":"object","additionalProperties":{"type":"string"}},"html":{"type":"string"}}}`,
		run:    helpCenterThemeCheck,
	}
}

func helpCenterThemeUpdateSpec() Spec {
	return Spec{
		Name:  "colliedesk_help_center_theme_update",
		Title: "Set the help center's brand",
		Description: `Replace the brand of the public help center: a field left out or null goes back to its default, so read it first with colliedesk_help_center_theme_get and send the whole theme. ` +
			themeContrastRule + `A failing pair is refused with the nearest passing color; colliedesk_help_center_theme_check tries a theme first. ` +
			`The logo and favicon must be images of the media library outside its trash; one the theme already has may stay while it is in the trash, so sending the theme back whole still saves. They are kept while the help center names them and kept no more once it names another. Fonts are stacks of the reader's own system fonts. ` +
			`Custom CSS follows the theme on every page; one that could load anything is refused with its line and column, and an empty one clears it. ` +
			`It is safe to repeat. ` + belowAdmin + "\n\nTier: admin.",
		Tier: "admin", Idempotent: true,
		REST:   []string{"PUT /api/v1/help-center/theme"},
		Input:  themeInputSchema,
		Output: `{"type":"object","required":["theme"],"properties":{"theme":` + themeSchema + `}}`,
		run:    helpCenterThemeUpdate,
	}
}

func helpCenterThemeGet(c *call, _ map[string]any) (any, error) {
	var theme map[string]any
	if err := c.get("/api/v1/help-center/theme", nil, &theme); err != nil {
		return nil, err
	}
	return map[string]any{"theme": theme}, nil
}

func helpCenterThemeCheck(c *call, arguments map[string]any) (any, error) {
	var checked map[string]any
	if page, ok := arguments["preview"]; ok {
		theme := make(map[string]any, len(arguments))
		for field, value := range arguments {
			if field != "preview" {
				theme[field] = value
			}
		}
		if err := c.send(http.MethodPost, "/api/v1/help-center/theme/preview", map[string]any{"page": page, "theme": theme}, &checked); err != nil {
			return nil, err
		}
		return checked, nil
	}
	if err := c.send(http.MethodPost, "/api/v1/help-center/theme/check", arguments, &checked); err != nil {
		return nil, err
	}
	return checked, nil
}

func helpCenterThemeUpdate(c *call, arguments map[string]any) (any, error) {
	var theme map[string]any
	if err := c.send(http.MethodPut, "/api/v1/help-center/theme", arguments, &theme); err != nil {
		return nil, err
	}
	return map[string]any{"theme": theme}, nil
}
