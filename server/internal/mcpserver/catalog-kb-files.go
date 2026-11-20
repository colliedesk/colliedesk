package mcpserver

// Files an article lists for download under its text (PDF, Office, CSV, ZIP...). A file is an item
// of the media library like an image; the article holds an ordered list of them. A published
// article keeps a new list in its draft until the draft is published.

const (
	kbFileTypes = `PDF; doc, docx, xls, xlsx, ppt, pptx; odt, ods, odp; CSV, TXT, RTF; ZIP, 7z, tar.gz`

	// kbAttachmentsSchema is the ordered files of an article, a draft or a translation.
	kbAttachmentsSchema = `{"type":"array","description":"Files listed for download under the text, in order (colliedesk_kb_article_attachments_set).",` +
		`"items":{"type":"object","required":["id","content_type","size","team_text"],"properties":{"id":{"type":"string"},"content_type":{"type":"string"},` +
		`"size":{"type":"integer","description":"Bytes."},"team_text":{"type":"object","required":["name"],"properties":{"name":{"type":"string","description":"The file name customers download."}}}}}}`
)

func kbFileUploadSpec() Spec {
	return Spec{
		Name:  "colliedesk_kb_file_upload",
		Title: "Upload a file to list under knowledge base articles",
		Description: `Store a file of at most 5 MiB (given as base64) in the media library, to list under articles with colliedesk_kb_article_attachments_set. Allowed: ` + kbFileTypes + `. ` +
			`The type is judged from the bytes and the extension of name; executables, scripts, HTML and SVG are refused. For a file of 5 to 25 MiB use colliedesk_kb_video_upload_start with kind file, then its part and complete tools. ` +
			`The same bytes give the same file. Files count toward the plan's storage.` + "\n\n" + kbMediaTextNotice,
		Idempotent: true,
		REST:       []string{"POST /api/v1/kb/media-uploads", "PUT /api/v1/kb/media-uploads/{id}", "POST /api/v1/kb/media-uploads/{id}/complete"},
		Input: `{"type":"object","additionalProperties":false,"required":["name","data_base64"],"properties":{"name":{"type":"string","minLength":1,"maxLength":200,"description":"The file name with its extension, as customers download it."},` +
			`"data_base64":{"type":"string","minLength":4,"maxLength":7000000,"description":"The file's bytes, standard base64."},"keep":{"type":"boolean","description":"Keep it in the library even if no article ever lists it."}}}`,
		Output: `{"type":"object","required":["media"],"properties":{"media":` + kbMediaSchema + `}}`,
		run:    kbFileUpload,
	}
}

func kbArticleAttachmentsSetSpec() Spec {
	return Spec{
		Name:  "colliedesk_kb_article_attachments_set",
		Title: "Set the files listed under a knowledge base article",
		Description: `Replace the files an article (or, with locale, one translation) lists for download under its text with media_ids, in that order; [] removes them all and the files stay in the library. ` +
			`On a published article the list goes into its draft until colliedesk_kb_article_draft_publish; a translation changes at once, so a published one also needs kb_articles:publish. ` +
			`Find files with colliedesk_kb_media_list kind file. At most 50 files, 25 MiB each and 100 MiB together.` + "\n\n" + kbTextNotice,
		Idempotent: true,
		REST:       []string{"PUT /api/v1/kb/articles/{id}/attachments", "PUT /api/v1/kb/articles/{id}/translations/{locale}/attachments"},
		Input: `{"type":"object","additionalProperties":false,"required":["article_id","media_ids"],"properties":{` + kbArticleIDProperty + `,` +
			`"media_ids":{"type":"array","maxItems":50,"items":{"type":"string",` + uuid + `},"description":"Ids of files of the media library, in the order to list them."},` +
			kbLocaleProperty + `,` + kbUpdatedAtProperty + `}}`,
		Output: `{"type":"object","properties":{"article":` + kbArticleSchema + `,"translation":` + kbTranslationSchema + `}}`,
		run:    kbArticleAttachmentsSet,
	}
}
