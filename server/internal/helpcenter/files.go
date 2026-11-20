package helpcenter

import (
	"errors"
	"net/http"
	"strings"

	"github.com/colliedesk/colliedesk/server/internal/attachments"
	"github.com/colliedesk/colliedesk/server/internal/domain"
)

// filesPath leads the address of a file an article lists: /files/{media id}/{slug of its name}.
const filesPath = "/files/"

// fileLink is a file listed under an article's text, as its page shows it.
type fileLink struct {
	Name, Type, Size, URL string
}

// fileLinks are the article's files by their addresses below the help center (every language shares them).
func (v view) fileLinks(items domain.KBAttachments) []fileLink {
	links := make([]fileLink, 0, len(items))
	for _, item := range items {
		links = append(links, fileLink{Name: item.Name, Type: domain.KBFileLabel(item.Name), Size: domain.KBFileSize(item.Size),
			URL: v.base + filesPath + item.ID + "/" + domain.KBFileSlug(item.Name)})
	}
	return links
}

// markdownFiles is a Markdown list of the files, each line led by indent, linked by their absolute addresses.
func (v view) markdownFiles(items domain.KBAttachments, indent string) string {
	var out strings.Builder
	for _, item := range items {
		out.WriteString(indent + "- [" + markdownText.Replace(item.Name) + "](" + v.address + filesPath + item.ID + "/" + domain.KBFileSlug(item.Name) +
			") (" + domain.KBFileLabel(item.Name) + ", " + domain.KBFileSize(item.Size) + ")\n")
	}
	return out.String()
}

// file hands out a file a published article lists, always as a download that cannot run on this
// origin. Any other id, a draft's file or another workspace's, gets the help center's 404 page. Only
// the id finds the file; the slug after it is for people. Each answer revalidates, so a file stops
// being served the moment no published article lists it, and a replaced file sends its new bytes.
func (s *Site) file(response http.ResponseWriter, request *http.Request, v view, address string) {
	site := v.site
	id, _, _ := strings.Cut(address, "/")
	if s.config.Objects == nil {
		s.writeSiteNotFound(response, v)
		return
	}
	file, err := s.config.Store.HelpCenterFile(request.Context(), site.WorkspaceID, id)
	if errors.Is(err, domain.ErrKBMediaNotFound) || err == nil && !strings.HasPrefix(file.StorageKey, site.WorkspaceID+"/") {
		s.writeSiteNotFound(response, v)
		return
	}
	if err != nil {
		s.writeError(response, err)
		return
	}
	// A file is up to 25 MiB: it shares the video streams' slots, so one address cannot hold the bandwidth.
	client := s.client(request)
	if !s.videos.enter(client) {
		securityHeaders(response.Header())
		response.Header().Set("Cache-Control", "no-store")
		response.Header().Set("Retry-After", "5")
		http.Error(response, "Too many downloads at once; try again in a moment.", http.StatusServiceUnavailable)
		return
	}
	defer s.videos.leave(client)
	served := attachments.Served{Key: file.StorageKey, ContentType: file.ContentType, Size: file.Size, ETag: file.SHA256,
		Cache: "public, no-cache", Disposition: "attachment; filename*=UTF-8''" + attachments.EncodeFilename(file.Name)}
	if !attachments.Serve(response, request, s.config.Objects, served) {
		s.writeSiteNotFound(response, v)
	}
}
