package pipeline

import (
	"bytes"
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/channels"
	"github.com/colliedesk/colliedesk/server/internal/netguard"
	"github.com/colliedesk/colliedesk/server/internal/store"
)

const (
	defaultFileBytes  = 25 << 20
	defaultEventBytes = 100 << 20
	fetchAttempts     = 3
	fetchTimeout      = 60 * time.Second
)

// FetchClient is the HTTP client that fetches channel media: https only, never to the host's own
// network (netguard checks every connection, redirects included). allow and tlsConfig are for tests.
func FetchClient(allow map[string]bool, tlsConfig *tls.Config) *http.Client {
	dialer := &net.Dialer{Timeout: 30 * time.Second, Control: netguard.Control(allow)}
	transport := &http.Transport{DialContext: dialer.DialContext, TLSClientConfig: tlsConfig, TLSHandshakeTimeout: 10 * time.Second,
		ResponseHeaderTimeout: 30 * time.Second, MaxIdleConns: 10, IdleConnTimeout: 30 * time.Second}
	return &http.Client{Transport: transport, Timeout: fetchTimeout, CheckRedirect: func(request *http.Request, via []*http.Request) error {
		if request.URL.Scheme != "https" {
			return errors.New("a channel file link may redirect to https only")
		}
		if len(via) >= 5 {
			return errors.New("too many redirects")
		}
		return nil
	}}
}

// WithFetchClient returns the pipeline fetching channel media with client.
func (p *Pipeline) WithFetchClient(client *http.Client) *Pipeline {
	copy := *p
	copy.fetch = client
	return &copy
}

// fetchFiles fetches the files of an event its type lists, once: a file already stored or failed is
// left as it is, so a replay or a second run never fetches again.
func (p *Pipeline) fetchFiles(ctx context.Context, adapter channels.Adapter, channel channels.Channel, event store.InboundEvent) error {
	source, ok := adapter.Inbound().(channels.MediaSource)
	if !ok {
		return nil
	}
	refs, err := source.Files(channels.Raw{ExternalEventID: event.ExternalEventID, JSON: event.Raw, ReceivedAt: event.ReceivedAt})
	if err != nil || len(refs) == 0 {
		return err
	}
	states, err := p.store.RecordInboundFiles(ctx, channel.WorkspaceID, event.ID, refs)
	if err != nil {
		return err
	}
	limits := adapter.Definition().FileLimits
	perFile, perEvent := limits.PerFile, limits.PerEvent
	if perFile <= 0 {
		perFile = defaultFileBytes
	}
	if perEvent <= 0 {
		perEvent = defaultEventBytes
	}
	var total int64
	for _, state := range states {
		if state.Status != "pending" || state.Index >= len(refs) {
			continue
		}
		ref := refs[state.Index]
		data, attempts, err := p.download(ctx, ref, min(perFile, perEvent-total))
		if err == nil {
			err = p.store.CheckChannelFileRoom(ctx, channel.WorkspaceID, int64(len(data)))
		}
		if err != nil {
			if errors.Is(err, context.Canceled) {
				return err
			}
			if finishErr := p.store.FinishInboundFile(ctx, channel.WorkspaceID, event.ID, state.Index, nil, state.Attempts+attempts, err.Error()); finishErr != nil {
				return finishErr
			}
			continue
		}
		storage := p.store.Objects()
		if storage == nil {
			return errors.New("attachment storage is not configured; channel files cannot be kept")
		}
		object, err := storage.Put(ctx, channel.WorkspaceID, bytes.NewReader(data), int64(len(data)))
		if err != nil {
			return fmt.Errorf("store channel file: %w", err)
		}
		total += object.Size
		if err := p.store.FinishInboundFile(ctx, channel.WorkspaceID, event.ID, state.Index, &object, state.Attempts+attempts, ""); err != nil {
			return err
		}
	}
	return nil
}

// errTooLarge is permanent: trying again fetches the same bytes.
var errTooLarge = errors.New("the file is larger than this channel accepts")

// download fetches one link, up to fetchAttempts times for a failure that may pass.
func (p *Pipeline) download(ctx context.Context, ref channels.FileRef, limit int64) ([]byte, int, error) {
	link, err := url.Parse(ref.URL)
	if err != nil || link.Scheme != "https" || link.Host == "" {
		return nil, 1, errors.New("the file link is not an https URL")
	}
	if ref.Size > limit {
		return nil, 1, fmt.Errorf("%w (%d bytes)", errTooLarge, ref.Size)
	}
	var last error
	for attempt := 1; attempt <= fetchAttempts; attempt++ {
		data, retry, err := p.downloadOnce(ctx, link.String(), limit)
		if err == nil {
			return data, attempt, nil
		}
		last = err
		if !retry {
			return nil, attempt, err
		}
		select {
		case <-ctx.Done():
			return nil, attempt, ctx.Err()
		case <-time.After(time.Duration(attempt) * 200 * time.Millisecond):
		}
	}
	return nil, fetchAttempts, last
}

func (p *Pipeline) downloadOnce(ctx context.Context, link string, limit int64) ([]byte, bool, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, link, nil)
	if err != nil {
		return nil, false, err
	}
	response, err := p.fetch.Do(request)
	if err != nil {
		return nil, !errors.Is(err, netguard.ErrBlockedAddress), fmt.Errorf("fetch the file: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		retry := response.StatusCode == http.StatusTooManyRequests || response.StatusCode >= 500
		return nil, retry, fmt.Errorf("the file link answered %d", response.StatusCode)
	}
	if response.ContentLength > limit {
		return nil, false, fmt.Errorf("%w (%d bytes)", errTooLarge, response.ContentLength)
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, limit+1))
	if err != nil {
		return nil, true, fmt.Errorf("read the file: %w", err)
	}
	if int64(len(data)) > limit {
		return nil, false, errTooLarge
	}
	return data, false, nil
}
