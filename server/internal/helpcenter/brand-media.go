package helpcenter

import (
	"errors"
	"io"
	"log"
	"net/http"
	"strconv"
	"strings"

	"github.com/colliedesk/colliedesk/server/internal/domain"
	"github.com/colliedesk/colliedesk/server/internal/store"
)

// brandAsset answers below /_/ of one help center: its brand style sheet, and the logo and favicon its
// theme names. Anything else there, an earlier brand sheet included, is the help center's 404.
func (s *Site) brandAsset(response http.ResponseWriter, request *http.Request, v view, name string) {
	theme := v.site.Theme
	switch {
	case v.brand.sheetName != "" && name == v.brand.sheetName:
		s.writeStyle(response, request, v.brand.sheet)
	case strings.HasPrefix(name, logoPath):
		s.brandImage(response, request, v, theme.LogoMediaID, strings.TrimPrefix(name, logoPath))
	case strings.HasPrefix(name, iconPath):
		s.brandImage(response, request, v, theme.FaviconMediaID, strings.TrimPrefix(name, iconPath))
	default:
		s.writeSiteNotFound(response, v)
	}
}

// Below /_/, the logo is at logo/<id> and the favicon at icon/<id>.
const (
	logoPath = "logo/"
	iconPath = "icon/"
)

// brandImage serves the image the theme names in one field (named, nil when the page shows none),
// by the rules of article images: an image of this workspace, outside the library's trash, that the
// help center still names when the store reads it.
func (s *Site) brandImage(response http.ResponseWriter, request *http.Request, v view, named *string, id string) {
	if named == nil || *named != id || s.config.Objects == nil {
		s.writeSiteNotFound(response, v)
		return
	}
	image, err := s.config.Store.HelpCenterBrandImage(request.Context(), v.site.WorkspaceID, id)
	// Storage keys start with the owning workspace id; a row pointing at another workspace's bytes is never served.
	if errors.Is(err, domain.ErrKBImageNotFound) || err == nil && !strings.HasPrefix(image.StorageKey, v.site.WorkspaceID+"/") {
		s.writeSiteNotFound(response, v)
		return
	}
	if err != nil {
		s.writeError(response, err)
		return
	}
	s.streamImage(response, request, v, image)
}

// streamImage sends a library image. Its id always names the same bytes, so any cache may keep it.
func (s *Site) streamImage(response http.ResponseWriter, request *http.Request, v view, image store.StoredKBImage) {
	body, err := s.config.Objects.Open(request.Context(), image.StorageKey)
	if err != nil {
		log.Printf("help center image %s: open stored bytes: %v", image.ID, err)
		s.writeSiteNotFound(response, v)
		return
	}
	defer body.Close()
	header := response.Header()
	header.Set("Content-Type", image.ContentType)
	header.Set("Content-Length", strconv.FormatInt(image.Size, 10))
	header.Set("X-Content-Type-Options", "nosniff")
	// Nothing the image holds may run or load, even if a browser opened it as a page.
	header.Set("Content-Security-Policy", "default-src 'none'; sandbox")
	header.Set("Cache-Control", "public, max-age=31536000, immutable")
	response.WriteHeader(http.StatusOK)
	if request.Method == http.MethodHead {
		return
	}
	if _, err := io.Copy(response, body); err != nil {
		log.Printf("help center image %s: stream: %v", image.ID, err)
	}
}
