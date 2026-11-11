package helpcenter_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/colliedesk/colliedesk/server/internal/helpcenter"
	"github.com/colliedesk/colliedesk/server/internal/netguard"
)

// A custom domain an admin points at the server's own network must not turn the check into a probe of
// it: the transport refuses loopback and private addresses before it connects, and the cause the
// workspace reads names no address.
func TestAddressCheckRefusesInternalAddresses(t *testing.T) {
	reached := false
	internal := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { reached = true }))
	t.Cleanup(internal.Close)
	err := helpcenter.CheckAddress(context.Background(), helpcenter.NewAddressTransport(), internal.URL, "workspace", "help.acme.example")
	if err == nil || !errors.Is(err, netguard.ErrBlockedAddress) || reached {
		t.Fatalf("check of a loopback address = %v (reached %t), want refused before connecting", err, reached)
	}
	if strings.Contains(err.Error(), "refusing") || !strings.Contains(err.Error(), "could not be reached") {
		t.Fatalf("the cause the workspace reads = %q", err.Error())
	}
}

// The marker is the workspace's and the host name's: another workspace's help center, or the same one on
// another name, does not answer for it.
func TestMarkerIsTheWorkspacesOnThatHostName(t *testing.T) {
	mine := helpcenter.Marker("workspace-a", "help.acme.example")
	if mine == "" || mine != helpcenter.Marker("workspace-a", "help.acme.example") {
		t.Fatal("the marker is not stable")
	}
	if mine == helpcenter.Marker("workspace-b", "help.acme.example") || mine == helpcenter.Marker("workspace-a", "help.other.example") {
		t.Fatal("the marker does not tell workspaces or host names apart")
	}
}
