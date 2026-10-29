package mcpserver

import (
	"bytes"
	"encoding/base64"
	"mime/multipart"
	"net/textproto"
	"strconv"
	"strings"
)

// inlineFilesLimit is the most file bytes one tool call carries, all files together: their base64
// (about 6.7 MB) stays under the MCP request limit. A message takes up to 10 MiB through REST; a
// person attaches more than this in the web app.
const (
	inlineFilesLimit      = 5 << 20
	inlineFilesLimitWords = "5 MiB"
)

// filesProperty is the files argument of a tool that writes a message.
const filesProperty = `"files":{"type":"array","minItems":1,"maxItems":10,"description":"Files to attach, at most 10 and ` + inlineFilesLimitWords +
	` in total (decoded) per call: about 7,000,000 characters of base64 for all files together. A larger call is refused before it reaches Colliedesk; send fewer files per call.","items":{"type":"object","additionalProperties":false,"required":["filename","data_base64"],"properties":{` +
	`"filename":{"type":"string","minLength":1,"maxLength":255,"description":"The name the recipient sees, with its extension."},` +
	`"content_type":{"type":"string","maxLength":200,"description":"The media type, for example application/pdf. Leave out for application/octet-stream."},` +
	`"data_base64":{"type":"string","minLength":4,"maxLength":7000000,"description":"The file's bytes, standard base64."}}}}`

// attachmentsSchema lists the files a written message carries.
const attachmentsSchema = `{"type":"array","items":{"type":"object","required":["id","filename","size"],"properties":{` +
	`"id":{"type":"string","description":"Read it back with colliedesk_attachment_get."},"filename":{"type":"string"},"size":{"type":"integer","description":"Bytes."}}}}`

// inlineFileArg is one entry of a files argument.
type inlineFileArg struct {
	Filename    string `json:"filename"`
	ContentType string `json:"content_type"`
	Data        string `json:"data_base64"`
}

// decodedFile is a file of a files argument, ready to send.
type decodedFile struct {
	filename, contentType string
	data                  []byte
}

// decodeFiles checks and decodes a files argument before anything is sent.
func decodeFiles(files []inlineFileArg) ([]decodedFile, error) {
	decoded := make([]decodedFile, 0, len(files))
	total := 0
	for index, file := range files {
		data, err := base64.StdEncoding.DecodeString(strings.TrimSpace(file.Data))
		if err != nil {
			return nil, &toolError{"files[" + strconv.Itoa(index) + "] (" + file.Filename + "): data_base64 is not standard base64. Nothing was sent or stored."}
		}
		// A line break in a name would end the part's header early; the server drops control characters anyway.
		file.Filename = strings.TrimSpace(strings.Map(func(r rune) rune {
			if r < 0x20 || r == 0x7f {
				return -1
			}
			return r
		}, file.Filename))
		if file.Filename == "" {
			return nil, &toolError{"files[" + strconv.Itoa(index) + "]: give the file a filename. Nothing was sent or stored."}
		}
		if len(data) == 0 {
			return nil, &toolError{"files[" + strconv.Itoa(index) + "] (" + file.Filename + ") is empty. Nothing was sent or stored."}
		}
		total += len(data)
		if total > inlineFilesLimit {
			return nil, &toolError{"The files of one call can be at most " + inlineFilesLimitWords + " in total. Nothing was sent or stored. " +
				"Attach larger files in the Colliedesk web app (a message takes up to 10 MiB), or send fewer files."}
		}
		contentType := file.ContentType
		if contentType == "" {
			contentType = "application/octet-stream"
		}
		decoded = append(decoded, decodedFile{filename: file.Filename, contentType: contentType, data: data})
	}
	return decoded, nil
}

// messageForm encodes a message as the multipart/form-data body REST takes: every field first, so a
// refusal that does not depend on the files comes before any is stored, then the files.
func messageForm(fields [][2]string, files []decodedFile) (string, []byte) {
	var body bytes.Buffer
	form := multipart.NewWriter(&body)
	for _, field := range fields {
		_ = form.WriteField(field[0], field[1]) // writing to a bytes.Buffer does not fail
	}
	for _, file := range files {
		header := textproto.MIMEHeader{}
		header.Set("Content-Disposition", `form-data; name="files"; filename="`+quoteEscaper.Replace(file.filename)+`"`)
		header.Set("Content-Type", file.contentType)
		part, _ := form.CreatePart(header)
		_, _ = part.Write(file.data)
	}
	_ = form.Close()
	return form.FormDataContentType(), body.Bytes()
}

// quoteEscaper escapes a file name inside the quoted filename of a part, as mime/multipart does.
var quoteEscaper = strings.NewReplacer("\\", "\\\\", `"`, "\\\"")

type attachmentRowOut struct {
	ID       string `json:"id"`
	Filename string `json:"filename"`
	Size     int64  `json:"size"`
}

func attachmentRows(message restMessage) []attachmentRowOut {
	if len(message.Attachments) == 0 {
		return nil
	}
	rows := make([]attachmentRowOut, 0, len(message.Attachments))
	for _, file := range message.Attachments {
		rows = append(rows, attachmentRowOut{ID: file.ID, Filename: file.Filename, Size: file.Size})
	}
	return rows
}

// filesNeedApproval words REST's refusal of files from a token that would only file a request.
const filesNeedApproval = "A token below this action's tier gets its call filed for a person's approval, and a call with files cannot wait for one, so nothing was sent or stored. " +
	"Call again without files: the text is filed for a person to approve. Then a person attaches the files in the web app after approval, or a token with the tier attaches them in a new message."
