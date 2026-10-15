package httpapi_test

import (
	"fmt"
	"testing"
)

// Anyone can post a customer's address to the web form. Those posts must not use up the customer's
// own allowance of real mail.
func TestWebFormPostsAsASenderDoNotBlockThatSendersRealEmail(t *testing.T) {
	f := newWebFormFixture(t, nil)
	f.enable(t, f.adminA)
	secret := connectEmail(t, f.api, f.adminA, "support@form-alpha.example.com")
	for i := 0; i < 10; i++ {
		wantAccepted(t, f.submit(f.slugA, `{"email":"victim@customer.example"}`, "", fmt.Sprintf("10.3.0.%d:1", i+1)))
	}
	result := inboundOK(t, f.api, secret, mail([]string{
		"From: victim@customer.example", "To: support@form-alpha.example.com", "Subject: My real question", "Message-ID: <victim-real@customer.example>",
	}, "Please help."))
	if result.Status != "created" {
		t.Fatalf("real email after 10 form posts as the same address: %+v, want created", result)
	}
}
