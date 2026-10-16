package polar_test

import (
	"context"
	"errors"
	"net/http"
	"reflect"
	"sort"
	"testing"

	"github.com/colliedesk/colliedesk/server/internal/polar"
)

func TestCountSlotHoldersAsksForLiveSubscriptionsOfOneProduct(t *testing.T) {
	fake := newFakePolar(t, http.StatusOK, `{"items":[{"id":"sub_1"}],"pagination":{"total_count":7,"max_page":7}}`)
	taken, err := fake.client().CountSlotHolders(context.Background(), "prod-founding")
	if err != nil || taken != 7 {
		t.Fatalf("taken=%d err=%v, want 7", taken, err)
	}
	if fake.method != http.MethodGet || fake.path != "/v1/subscriptions/" || fake.auth != "Bearer token-for-test" {
		t.Fatalf("request=%s %s auth=%q", fake.method, fake.path, fake.auth)
	}
	statuses := append([]string(nil), fake.query["status"]...)
	sort.Strings(statuses)
	if !reflect.DeepEqual(statuses, []string{"active", "past_due", "trialing"}) {
		t.Fatalf("status=%v, want active, past_due and trialing as repeated parameters", fake.query["status"])
	}
	if fake.query.Get("product_id") != "prod-founding" || fake.query.Get("limit") != "1" {
		t.Fatalf("query=%v", fake.query)
	}
}

func TestCountSlotHoldersFailsOnPolarErrorsAndOddReplies(t *testing.T) {
	for name, fake := range map[string]*fakePolar{
		"server error":     newFakePolar(t, http.StatusInternalServerError, `{}`),
		"no pagination":    newFakePolar(t, http.StatusOK, `{"items":[]}`),
		"negative total":   newFakePolar(t, http.StatusOK, `{"pagination":{"total_count":-1}}`),
		"not json":         newFakePolar(t, http.StatusOK, `nope`),
		"unauthorized key": newFakePolar(t, http.StatusUnauthorized, `{"detail":"bad"}`),
	} {
		t.Run(name, func(t *testing.T) {
			if taken, err := fake.client().CountSlotHolders(context.Background(), "prod-founding"); err == nil {
				t.Fatalf("taken=%d, want an error", taken)
			}
		})
	}
}

func TestProductArchivedReadsTheProduct(t *testing.T) {
	for reply, want := range map[string]bool{`{"id":"prod-1","is_archived":true}`: true, `{"id":"prod-1","is_archived":false}`: false} {
		fake := newFakePolar(t, http.StatusOK, reply)
		got, err := fake.client().ProductArchived(context.Background(), "prod-1")
		if err != nil || got != want {
			t.Fatalf("reply %s: archived=%v err=%v, want %v", reply, got, err, want)
		}
		if fake.method != http.MethodGet || fake.path != "/v1/products/prod-1" || fake.auth != "Bearer token-for-test" {
			t.Fatalf("request=%s %s auth=%q", fake.method, fake.path, fake.auth)
		}
	}
}

func TestProductArchivedFailsWithoutTheFlagOrOnErrors(t *testing.T) {
	if _, err := newFakePolar(t, http.StatusOK, `{"id":"prod-1"}`).client().ProductArchived(context.Background(), "prod-1"); err == nil {
		t.Fatal("want an error when the reply has no is_archived")
	}
	var status *polar.StatusError
	_, err := newFakePolar(t, http.StatusNotFound, `{}`).client().ProductArchived(context.Background(), "prod-1")
	if !errors.As(err, &status) || status.Code != http.StatusNotFound {
		t.Fatalf("err=%v, want a 404 StatusError", err)
	}
}

func TestArchiveProductPatchesIsArchived(t *testing.T) {
	fake := newFakePolar(t, http.StatusOK, `{"id":"prod-1","is_archived":true}`)
	if err := fake.client().ArchiveProduct(context.Background(), "prod-1"); err != nil {
		t.Fatal(err)
	}
	if fake.method != http.MethodPatch || fake.path != "/v1/products/prod-1" || fake.auth != "Bearer token-for-test" {
		t.Fatalf("request=%s %s auth=%q", fake.method, fake.path, fake.auth)
	}
	if len(fake.body) != 1 || fake.body["is_archived"] != true {
		t.Fatalf("body=%v, want only is_archived=true", fake.body)
	}
}

func TestArchiveProductReportsRefusal(t *testing.T) {
	fake := newFakePolar(t, http.StatusForbidden, `{"detail":"missing scope"}`)
	if err := fake.client().ArchiveProduct(context.Background(), "prod-1"); err == nil {
		t.Fatal("want an error when Polar refuses the archive")
	}
}
