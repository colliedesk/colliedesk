package email_test

import (
	"io"
	"mime/quotedprintable"
	"strings"
	"testing"
)

// A help center article linked from the reply box is its title and its address on two lines
// (docs/user/knowledge-base.md). The customer's mail is text/plain, so it arrives as those two lines:
// no Markdown, and the address on a line of its own, whole once decoded, however long it is.
func TestComposeKeepsALinkedArticleAsItsTitleAndAddressOnLinesOfTheirOwn(t *testing.T) {
	address := "https://desk.example.com/help/acme/a/12-%C4%91%E1%BA%B7t-l%E1%BA%A1i-m%E1%BA%ADt-kh%E1%BA%A9u-c%E1%BB%A7a-b%E1%BA%A1n"
	message := outgoing()
	message.Body = "Hi Lan, these steps help:\nĐặt lại mật khẩu của bạn\n" + address + "\nBest regards"
	parsed := parseComposed(t, message)
	if got := parsed.Header.Get("Content-Type"); got != "text/plain; charset=utf-8" {
		t.Fatalf("Content-Type = %q", got)
	}
	decoded, err := io.ReadAll(quotedprintable.NewReader(parsed.Body))
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSuffix(string(decoded), "\r\n"), "\r\n")
	want := []string{"Hi Lan, these steps help:", "Đặt lại mật khẩu của bạn", address, "Best regards"}
	if strings.Join(lines, "|") != strings.Join(want, "|") {
		t.Fatalf("decoded lines = %q, want %q", lines, want)
	}
}
