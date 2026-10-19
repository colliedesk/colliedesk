package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"mime"
	"net/http"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/colliedesk/colliedesk/server/internal/store"
)

const problemReportPath = "/api/v1/support/problem-reports"

// problemReportsPerHour is how many reports one agent may file per hour.
const problemReportsPerHour = 5

// problemReport is a request after reading and validating.
type problemReport struct {
	description string
	diagnostics diagnosticsInput
	screenshots []screenshot
}

type screenshot struct {
	filename, contentType string
	data                  []byte
}

// createProblemReport sends an agent's "Report a problem" to the support desk configured with
// PROBLEM_REPORT_WORKSPACE as a conversation. Without it, or when it names no workspace, the answer is 404
// not_configured and nothing leaves the server. Only the description, the browser's diagnostics and facts the
// server knows about the session are written; no text from the reporter's workspace is read.
func (a *API) createProblemReport(response http.ResponseWriter, request *http.Request) {
	ctx := request.Context()
	desk, ok, err := a.problemReportDesk(ctx)
	if err != nil {
		writeInternalError(response, err)
		return
	}
	if !ok {
		writeError(response, http.StatusNotFound, "not_configured", "This server does not receive problem reports.")
		return
	}
	report, fields, err := a.readProblemReport(response, request)
	if err != nil {
		a.writeUploadError(response, err)
		return
	}
	if len(fields) > 0 {
		writeJSON(response, http.StatusUnprocessableEntity, map[string]any{"error": errorBody(response, map[string]any{
			"code": "validation_failed", "message": "Check the highlighted fields.", "fields": fields})})
		return
	}
	who := principal(request)
	if allowed, wait := a.problemReports.allow(who.Agent.ID, a.now()); !allowed {
		response.Header().Set("Retry-After", strconv.Itoa(int(wait.Seconds())+1))
		writeError(response, http.StatusTooManyRequests, "rate_limited", fmt.Sprintf("You can send %d problem reports an hour. Try again later.", problemReportsPerHour))
		return
	}
	requestID := requestIDOf(ctx)
	name := who.Agent.DisplayName
	if name == "" {
		name = who.Agent.Email
	}
	input := store.ProblemReport{Subject: "Problem report: " + oneLine(who.Workspace.Name, 200), ReporterEmail: who.Agent.Email, ReporterName: oneLine(name, 200),
		Body: a.reportBody(who, report.description, report.diagnostics, requestID, a.now())}
	var stored []store.NewAttachment
	defer func() { a.discardUnreferenced(context.WithoutCancel(ctx), desk, stored) }()
	for _, shot := range report.screenshots {
		if err := a.checkDiskSpace(); err != nil {
			a.writeUploadError(response, err)
			return
		}
		object, err := a.config.Attachments.Put(ctx, desk, strings.NewReader(string(shot.data)), int64(len(shot.data)))
		if err != nil {
			a.writeUploadError(response, err)
			return
		}
		stored = append(stored, store.NewAttachment{Object: object})
		input.Files = append(input.Files, store.EmailFile{Filename: shot.filename, ContentType: shot.contentType, Object: object})
	}
	if _, err := a.store.CreateProblemReport(ctx, desk, input); err != nil {
		writeInternalError(response, err)
		return
	}
	stored = nil // referenced now: keep the bytes
	writeJSON(response, http.StatusCreated, map[string]string{"reference": requestID})
}

// problemReportDesk resolves the configured support desk. A slug that names no workspace is a
// misconfiguration: it is logged and reported to the caller as not configured.
func (a *API) problemReportDesk(ctx context.Context) (string, bool, error) {
	slug := a.config.ProblemReportWorkspace
	if slug == "" {
		return "", false, nil
	}
	id, found, err := a.store.WorkspaceForMailSlug(ctx, slug)
	if err != nil {
		return "", false, err
	}
	if !found {
		slog.Error("PROBLEM_REPORT_WORKSPACE names no workspace on this server", "slug", slug)
	}
	return id, found, nil
}

// readProblemReport reads a JSON or multipart body. It returns field messages for input the caller can fix
// and an error for a body it could not read at all.
func (a *API) readProblemReport(response http.ResponseWriter, request *http.Request) (problemReport, map[string]string, error) {
	media, _, _ := mime.ParseMediaType(request.Header.Get("Content-Type"))
	var report problemReport
	var raw diagnosticsInput
	fields := map[string]string{}
	if media == "multipart/form-data" {
		request.Body = http.MaxBytesReader(response, request.Body, maxProblemShotBytes+multipartOverhead)
		description, diagnostics, shots, err := readProblemMultipart(request, fields)
		if err != nil {
			return report, nil, err
		}
		report.description, report.screenshots = description, shots
		if diagnostics != "" {
			decoder := json.NewDecoder(strings.NewReader(diagnostics))
			decoder.DisallowUnknownFields()
			if decoder.Decode(&raw) != nil {
				fields["diagnostics"] = "Send diagnostics as a JSON object."
			}
		}
	} else {
		var input struct {
			Description string           `json:"description"`
			Diagnostics diagnosticsInput `json:"diagnostics"`
		}
		if err := decodeJSON(request, &input); err != nil {
			return report, nil, &uploadError{http.StatusBadRequest, "invalid_request", "The body must be a JSON object with a description."}
		}
		report.description, raw = input.Description, input.Diagnostics
	}
	report.description = strings.TrimSpace(strings.ReplaceAll(report.description, "\x00", ""))
	switch count := utf8.RuneCountInString(report.description); {
	case count == 0:
		fields["description"] = "Describe the problem."
	case count > maxProblemDescription:
		fields["description"] = fmt.Sprintf("Keep the description under %d characters.", maxProblemDescription)
	}
	if _, bad := fields["diagnostics"]; !bad {
		var reason string
		if report.diagnostics, reason = raw.clean(); reason != "" {
			fields["diagnostics"] = reason
		}
	}
	return report, fields, nil
}

// readProblemMultipart reads description, diagnostics (a JSON string) and up to three screenshots. The
// bytes stay in memory (5 MB at most) until the whole report is valid, so a refusal stores nothing.
func readProblemMultipart(request *http.Request, fields map[string]string) (string, string, []screenshot, error) {
	reader, err := request.MultipartReader()
	if err != nil {
		return "", "", nil, &uploadError{http.StatusBadRequest, "invalid_request", "The multipart request could not be read."}
	}
	var description, diagnostics string
	var shots []screenshot
	remaining := int64(maxProblemShotBytes)
	for {
		part, err := reader.NextPart()
		if errors.Is(err, io.EOF) {
			return description, diagnostics, shots, nil
		}
		if err != nil {
			return "", "", nil, invalidUpload(err)
		}
		switch name := part.FormName(); {
		case name == "description" || name == "diagnostics":
			text, err := readField(part)
			if err != nil {
				return "", "", nil, err
			}
			if name == "description" {
				description = text
			} else {
				diagnostics = text
			}
		case name == "screenshots" && part.FileName() != "":
			if len(shots) >= maxProblemScreenshots {
				fields["screenshots"] = fmt.Sprintf("Attach at most %d screenshots.", maxProblemScreenshots)
				return description, diagnostics, nil, nil
			}
			data, err := io.ReadAll(io.LimitReader(part, remaining+1))
			if err != nil {
				return "", "", nil, invalidUpload(err)
			}
			if remaining -= int64(len(data)); remaining < 0 {
				fields["screenshots"] = fmt.Sprintf("Screenshots can be %d MB in total.", maxProblemShotBytes>>20)
				return description, diagnostics, nil, nil
			}
			kind := http.DetectContentType(data)
			if kind != "image/png" && kind != "image/jpeg" && kind != "image/webp" {
				fields["screenshots"] = "Screenshots must be PNG, JPEG or WebP images."
				return description, diagnostics, nil, nil
			}
			shots = append(shots, screenshot{filename: screenshotName(part.FileName(), kind, len(shots)), contentType: kind, data: data})
		default:
			return "", "", nil, &uploadError{http.StatusBadRequest, "invalid_request", "Send description, diagnostics and screenshots only; every screenshot needs a filename."}
		}
	}
}

func screenshotName(sent, kind string, index int) string {
	if name := cleanFilename(sent); name != "" {
		return name
	}
	return "screenshot-" + strconv.Itoa(index+1) + "." + strings.TrimPrefix(kind, "image/")
}
