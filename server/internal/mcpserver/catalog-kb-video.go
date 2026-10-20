package mcpserver

// Videos of the knowledge base go up in parts: a video is up to 200 MiB, far more than one tool call
// carries. Start the upload, send the parts in order (5 MiB of base64 each), then complete it; the
// server checks the file and removes location and other data in place. A file never waits for
// approval, so a read or draft token cannot upload.

const (
	kbUploadIDProperty = `"upload_id":{"type":"string",` + uuid + `,"description":"From colliedesk_kb_video_upload_start."}`

	kbUploadSchema = `{"type":"object","required":["upload_id","size","offset","expires_at"],"properties":{"upload_id":{"type":"string"},"size":{"type":"integer"},` +
		`"offset":{"type":"integer","description":"Bytes received; the next part starts here."},"expires_at":{"type":"string"},` +
		`"media_id":{"type":["string","null"],"description":"Set once complete."}}}`

	kbVideoNotice = `A file never waits for approval: a read or draft token cannot upload.`
)

func kbVideoUploadStartSpec() Spec {
	return Spec{
		Name:  "colliedesk_kb_video_upload_start",
		Title: "Start uploading a knowledge base video",
		Description: `Start an upload of a video of size bytes: MP4 (H.264 or AV1 with AAC sound, QuickTime .mov included) or WebM (VP8, VP9 or AV1 with Opus or Vorbis), at most 200 MiB unless the server says otherwise. ` +
			`H.265 (HEVC), the default of iPhones, is refused at completion: export it as H.264 first. Then send the bytes in order with colliedesk_kb_video_upload_part and finish with colliedesk_kb_video_upload_complete. ` +
			`The plan's storage and the server's disk must have room first. At most 3 uploads are in progress in a workspace; the parts are thrown away after 24 hours. ` + kbVideoNotice + "\n\nTier: send.",
		Tier: "send",
		REST: []string{"POST /api/v1/kb/media-uploads"},
		Input: `{"type":"object","additionalProperties":false,"required":["size"],"properties":{"size":{"type":"integer","minimum":1},"name":{"type":"string","maxLength":200},` +
			`"alt":{"type":"string","maxLength":500,"description":"The video's title for screen readers."},"keep":{"type":"boolean","description":"Keep it in the library even if no article ever uses it."}}}`,
		Output: kbUploadSchema,
		run:    kbVideoUploadStart,
	}
}

func kbVideoUploadPartSpec() Spec {
	return Spec{
		Name:  "colliedesk_kb_video_upload_part",
		Title: "Send a part of a knowledge base video",
		Description: `Send the next bytes of an upload, at most 5 MiB (given as base64), starting at offset, which must be the upload's offset (the answer of the previous part). ` +
			`A part that does not start there is refused and says where the upload stands; send that part again. ` + kbVideoNotice + "\n\nTier: send.",
		Tier: "send",
		REST: []string{"PUT /api/v1/kb/media-uploads/{id}"},
		Input: `{"type":"object","additionalProperties":false,"required":["upload_id","offset","data_base64"],"properties":{` + kbUploadIDProperty + `,` +
			`"offset":{"type":"integer","minimum":0},"data_base64":{"type":"string","minLength":4,"maxLength":7000000,"description":"Up to 5 MiB of the video's bytes, standard base64."}}}`,
		Output: kbUploadSchema,
		run:    kbVideoUploadPart,
	}
}

func kbVideoUploadStatusSpec() Spec {
	return Spec{
		Name:        "colliedesk_kb_video_upload_status",
		Title:       "See where a knowledge base video upload stands",
		Description: `Say how many bytes of an upload arrived (offset), so you can go on after an interruption.` + "\n\nTier: read.",
		Tier:        "read", ReadOnly: true,
		REST:   []string{"GET /api/v1/kb/media-uploads/{id}"},
		Input:  `{"type":"object","additionalProperties":false,"required":["upload_id"],"properties":{` + kbUploadIDProperty + `}}`,
		Output: kbUploadSchema,
		run:    kbVideoUploadStatus,
	}
}

func kbVideoUploadCancelSpec() Spec {
	return Spec{
		Name:        "colliedesk_kb_video_upload_cancel",
		Title:       "Cancel a knowledge base video upload",
		Description: `Cancel an upload in progress and throw its parts away.` + "\n\nTier: send.",
		Tier:        "send", Idempotent: true,
		REST:   []string{"DELETE /api/v1/kb/media-uploads/{id}"},
		Input:  `{"type":"object","additionalProperties":false,"required":["upload_id"],"properties":{` + kbUploadIDProperty + `}}`,
		Output: `{"type":"object","required":["cancelled"],"properties":{"cancelled":{"type":"boolean"}}}`,
		run:    kbVideoUploadCancel,
	}
}

func kbVideoUploadCompleteSpec() Spec {
	return Spec{
		Name:  "colliedesk_kb_video_upload_complete",
		Title: "Finish uploading a knowledge base video",
		Description: `Once every byte arrived, check and store the video: the server reads its length, size and codecs and removes location, titles and data tracks in place. ` +
			`A video it refuses is thrown away with its upload, and the answer says why. The same bytes give the item that holds them. ` +
			`Put it in an article as <video controls src="URL"></video> (url is in the answer), on a line of its own. ` + kbVideoNotice + "\n\n" + kbMediaTextNotice + "\n\nTier: send.",
		Tier: "send", Idempotent: true,
		REST:   []string{"POST /api/v1/kb/media-uploads/{id}/complete"},
		Input:  `{"type":"object","additionalProperties":false,"required":["upload_id"],"properties":{` + kbUploadIDProperty + `}}`,
		Output: `{"type":"object","required":["media"],"properties":{"media":` + kbMediaSchema + `}}`,
		run:    kbVideoUploadComplete,
	}
}

func kbVideoPosterSetSpec() Spec {
	return Spec{
		Name:  "colliedesk_kb_video_poster_set",
		Title: "Set the poster of a knowledge base video",
		Description: `Give a video its poster, the still picture shown before it plays: a PNG, JPEG, GIF or WebP image of at most 5 MiB, given as base64. ` +
			`It replaces the poster the video had, counts toward storage with the video and becomes its thumbnail in the library. ` + kbVideoNotice + "\n\nTier: send.",
		Tier: "send", Idempotent: true,
		REST: []string{"PUT /api/v1/kb/media/{id}/poster"},
		Input: `{"type":"object","additionalProperties":false,"required":["media_id","data_base64"],"properties":{` + kbMediaIDProperty + `,` +
			`"data_base64":{"type":"string","minLength":4,"maxLength":7000000,"description":"The image bytes, standard base64."}}}`,
		Output: `{"type":"object","required":["media"],"properties":{"media":` + kbMediaSchema + `}}`,
		run:    kbVideoPosterSet,
	}
}
