package httpapi_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/colliedesk/colliedesk/server/internal/attachments"
	"github.com/colliedesk/colliedesk/server/internal/httpapi"
	"github.com/colliedesk/colliedesk/server/internal/store"
)

// cardFile stores bytes as file idx of the card's item, the way the channel's fetch leaves a stored file, and
// returns the item's id. status other than stored writes the row without bytes.
func (d levelDesk) cardFile(t *testing.T, storage attachments.Storage, workspace, card string, idx int, name, contentType, status string, body []byte) string {
	t.Helper()
	var key, sum *string
	var size *int64
	if status == "stored" {
		object, err := storage.Put(context.Background(), workspace, bytes.NewReader(body), int64(len(body)))
		if err != nil {
			t.Fatal(err)
		}
		key, sum, size = &object.Key, &object.SHA256, &object.Size
	}
	var failure *string
	if status == "failed" {
		reason := "the file is larger than this channel accepts (27020613 bytes)"
		failure = &reason
	}
	return queryString(t, d.pool, `WITH e AS (SELECT id FROM channel_inbound_events WHERE workspace_id=$1 AND gathering_id=$2::uuid LIMIT 1),
			f AS (INSERT INTO channel_inbound_files(workspace_id,event_id,idx,name,content_type,status,storage_key,sha256,size,error)
				SELECT $1,e.id,$3,$4,$5,$6,$7,$8,$9,$10 FROM e RETURNING event_id)
		SELECT i.id::text FROM channel_thread_items i, f WHERE i.workspace_id=$1 AND i.inbound_event_id=f.event_id`,
		workspace, card, idx, name, contentType, status, key, sum, size, failure)
}

// A person sees what a customer sent on a card before deciding it: the card lists its files and each stored one
// reads back, a range at a time, only on its own card and only in its workspace.
func TestACardsFilesReadBackOnlyFromTheirCardAndWorkspace(t *testing.T) {
	var storage attachments.Storage
	d := newLevelDesk(t, func(config *httpapi.Config) { storage = config.Attachments })
	picture := []byte("\x89PNG\r\n\x1a\n-a picture of the parcel-")
	card := d.heldCard(t, d.workspaceA, d.chatA, "u1", "[picture] the parcel came broken")
	item := d.cardFile(t, storage, d.workspaceA, card, 0, "image-1", "image/png", "stored", picture)
	d.cardFile(t, storage, d.workspaceA, card, 1, "video-1", "video/mp4", "failed", nil)
	other := d.heldCard(t, d.workspaceA, d.chatA, "u2", "another customer's card")
	path := "/api/v1/intake/gatherings/" + card + "/items/" + item + "/files/"

	list := request(t, d.api, http.MethodGet, "/api/v1/intake/gatherings", nil, d.adminA, nil)
	var page struct {
		Items []struct {
			ID    string                 `json:"id"`
			Files []store.IntakeCardFile `json:"files"`
		} `json:"items"`
	}
	if err := json.Unmarshal(list.Body.Bytes(), &page); err != nil {
		t.Fatal(err)
	}
	var files []store.IntakeCardFile
	for _, entry := range page.Items {
		if entry.ID == card {
			files = entry.Files
		}
	}
	if len(files) != 2 || files[0].ItemID != item || files[0].Index != 0 || files[0].Status != "stored" || files[0].ContentType != "image/png" ||
		files[1].Index != 1 || files[1].Status != "failed" || files[1].Error == nil || !strings.Contains(*files[1].Error, "larger than this channel accepts") {
		t.Fatalf("card files = %+v", files)
	}

	whole := request(t, d.api, http.MethodGet, path+"0", nil, d.adminA, nil)
	if whole.Code != http.StatusOK || !bytes.Equal(whole.Body.Bytes(), picture) || whole.Header().Get("Content-Type") != "application/octet-stream" ||
		!strings.HasPrefix(whole.Header().Get("Content-Disposition"), "attachment;") {
		t.Fatalf("file: %d %q %v", whole.Code, whole.Body.String(), whole.Header())
	}
	ranged := newRequest(t, http.MethodGet, path+"0", nil, d.adminA, nil)
	ranged.Header.Set("Range", "bytes=0-7")
	part := serve(d.api, ranged)
	if part.Code != http.StatusPartialContent || !bytes.Equal(part.Body.Bytes(), picture[:8]) {
		t.Fatalf("range: %d %q", part.Code, part.Body.String())
	}
	for name, call := range map[string]struct {
		path, token string
	}{
		"another workspace":      {path + "0", d.adminB},
		"a file not fetched":     {path + "1", d.adminA},
		"an index past the last": {path + "2", d.adminA},
		"another card's path":    {"/api/v1/intake/gatherings/" + other + "/items/" + item + "/files/0", d.adminA},
		"no number":              {path + "first", d.adminA},
		"no uuid":                {"/api/v1/intake/gatherings/" + card + "/items/nope/files/0", d.adminA},
	} {
		if response := request(t, d.api, http.MethodGet, call.path, nil, call.token, nil); response.Code != http.StatusNotFound {
			t.Errorf("%s: %d %s", name, response.Code, response.Body.String())
		}
	}
	// A token without messages:read reads no customer file.
	refused(t, d.api, http.MethodGet, path+"0", nil, customToken(t, d.api, d.adminA, "No messages", "conversations:read"), "messages:read")
	if response := request(t, d.api, http.MethodGet, path+"0", nil, customToken(t, d.api, d.adminA, "Reads messages", "messages:read"), nil); response.Code != http.StatusOK {
		t.Fatalf("token with messages:read: %d %s", response.Code, response.Body.String())
	}
}

// The list query and IntakeCardFiles say the same most files of a card.
func TestIntakeCardFilesLimitMatches(t *testing.T) {
	if store.IntakeCardFiles != 8 {
		t.Fatalf("IntakeCardFiles = %d: change intakeCardFilesSQL and the contract's maxItems with it", store.IntakeCardFiles)
	}
}
