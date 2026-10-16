package emailoutbox_test

import (
	"context"
	"net/mail"
	"reflect"
	"strings"
	"testing"
	"time"
)

func (f fixture) replyCopying(t *testing.T, cc ...string) string {
	t.Helper()
	message, err := f.store.CreateMessageWithCC(context.Background(), f.workspaceID, f.conversationID, f.agentID, "outgoing", "Copying your colleagues.", cc)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(message.CC, cc) {
		t.Fatalf("stored cc = %v, want %v", message.CC, cc)
	}
	return message.ID
}

func TestDeliveryReachesTheContactAndEveryCopiedAddressAndNoOne(t *testing.T) {
	f := newFixture(t)
	server := startSMTPServer(t)
	clock := time.Now().UTC().Add(time.Second)
	worker := newWorker(f, server, &clock)
	reply := f.replyCopying(t, "boss@customer.example", "ops@customer.example")
	process(t, worker)
	if delivery := f.delivery(t, reply); delivery.Status != "sent" {
		t.Fatalf("delivery = %+v", delivery)
	}
	received := server.mail()
	if len(received) != 1 {
		t.Fatalf("mail received = %d", len(received))
	}
	if want := []string{"olive@customer.example", "boss@customer.example", "ops@customer.example"}; !reflect.DeepEqual(received[0].To, want) {
		t.Fatalf("RCPT TO = %v, want %v", received[0].To, want)
	}
	parsed, err := mail.ReadMessage(strings.NewReader(received[0].Data))
	if err != nil {
		t.Fatal(err)
	}
	cc, err := parsed.Header.AddressList("Cc")
	if err != nil || len(cc) != 2 || cc[0].Address != "boss@customer.example" || cc[1].Address != "ops@customer.example" {
		t.Fatalf("Cc header = %v, err=%v", cc, err)
	}
	if parsed.Header.Get("Bcc") != "" {
		t.Fatal("a Bcc header leaked into the message")
	}
}

func TestRetryAfterATemporaryFailureSendsToTheSameStoredList(t *testing.T) {
	f := newFixture(t)
	server := startSMTPServer(t, "451 4.3.0 Try again later")
	clock := time.Now().UTC().Add(time.Second)
	worker := newWorker(f, server, &clock)
	reply := f.replyCopying(t, "boss@customer.example")
	process(t, worker)
	if len(server.mail()) != 0 {
		t.Fatal("the first attempt was meant to fail")
	}
	clock = clock.Add(time.Minute)
	process(t, worker)
	received := server.mail()
	if len(received) != 1 || !reflect.DeepEqual(received[0].To, []string{"olive@customer.example", "boss@customer.example"}) {
		t.Fatalf("after the retry: %+v", received)
	}
	if f.delivery(t, reply).Status != "sent" {
		t.Fatal("not sent")
	}
}

func TestAReplyWithoutCopiesStillGoesToTheContactAlone(t *testing.T) {
	f := newFixture(t)
	server := startSMTPServer(t)
	clock := time.Now().UTC().Add(time.Second)
	f.write(t, "outgoing", "Just you.")
	process(t, newWorker(f, server, &clock))
	received := server.mail()
	if len(received) != 1 || !reflect.DeepEqual(received[0].To, []string{"olive@customer.example"}) || strings.Contains(received[0].Data, "\r\nCc:") {
		t.Fatalf("received = %+v", received)
	}
}

func TestARefusedCopyStillDeliversTheReplyToTheContact(t *testing.T) {
	f := newFixture(t)
	server := startSMTPServer(t)
	server.refuseRecipient("gone@customer.example", "550 5.1.1 No such user")
	clock := time.Now().UTC().Add(time.Second)
	reply := f.replyCopying(t, "gone@customer.example", "ops@customer.example")
	process(t, newWorker(f, server, &clock))
	delivery := f.delivery(t, reply)
	if delivery.Status != "sent" {
		t.Fatalf("one refused Cc failed the whole reply: %+v", delivery)
	}
	if delivery.LastError == nil || !strings.Contains(*delivery.LastError, "gone@customer.example") {
		t.Fatalf("the refused copy must be reported on the delivery: %+v", delivery)
	}
	received := server.mail()
	if len(received) != 1 || !reflect.DeepEqual(received[0].To, []string{"olive@customer.example", "ops@customer.example"}) {
		t.Fatalf("received = %+v", received)
	}
}

func TestARefusedContactStillFailsTheReply(t *testing.T) {
	f := newFixture(t)
	server := startSMTPServer(t)
	server.refuseRecipient("olive@customer.example", "550 5.1.1 No such user")
	clock := time.Now().UTC().Add(time.Second)
	reply := f.replyCopying(t, "ops@customer.example")
	process(t, newWorker(f, server, &clock))
	if delivery := f.delivery(t, reply); delivery.Status != "failed" {
		t.Fatalf("a reply the customer cannot receive must fail: %+v", delivery)
	}
	if len(server.mail()) != 0 {
		t.Fatal("nothing may be sent when the contact is refused")
	}
}
