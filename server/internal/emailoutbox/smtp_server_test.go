package emailoutbox_test

import (
	"bufio"
	"net"
	"strconv"
	"strings"
	"sync"
	"testing"
)

// smtpServer is a minimal in-process SMTP server bound to 127.0.0.1. It answers each DATA with
// the next scripted reply ("250 ..." when the script is exhausted) and records accepted mail.
type smtpServer struct {
	listener net.Listener
	mu       sync.Mutex
	replies  []string
	received []receivedMail
	// refused maps a recipient address to the reply its RCPT TO gets instead of 250.
	refused map[string]string
}

type receivedMail struct {
	From string
	To   []string
	Data string
}

func startSMTPServer(t *testing.T, replies ...string) *smtpServer {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	server := &smtpServer{listener: listener, replies: replies}
	go server.accept()
	t.Cleanup(func() { _ = listener.Close() })
	return server
}

func (s *smtpServer) port() int {
	return s.listener.Addr().(*net.TCPAddr).Port
}

func (s *smtpServer) mail() []receivedMail {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]receivedMail(nil), s.received...)
}

func (s *smtpServer) accept() {
	for {
		conn, err := s.listener.Accept()
		if err != nil {
			return
		}
		go s.handle(conn)
	}
}

func (s *smtpServer) handle(conn net.Conn) {
	defer conn.Close()
	reader := bufio.NewReader(conn)
	write := func(line string) { _, _ = conn.Write([]byte(line + "\r\n")) }
	write("220 127.0.0.1 test SMTP")
	var current receivedMail
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			return
		}
		command := strings.ToUpper(strings.TrimSpace(line))
		switch {
		case strings.HasPrefix(command, "EHLO"), strings.HasPrefix(command, "HELO"):
			write("250 127.0.0.1")
		case strings.HasPrefix(command, "MAIL FROM:"):
			current = receivedMail{From: strings.Trim(strings.TrimSpace(line)[10:], "<>")}
			write("250 OK")
		case strings.HasPrefix(command, "RCPT TO:"):
			recipient := strings.Trim(strings.TrimSpace(line)[8:], "<>")
			if reply := s.refusal(recipient); reply != "" {
				write(reply)
				continue
			}
			current.To = append(current.To, recipient)
			write("250 OK")
		case command == "DATA":
			write("354 End data with <CR><LF>.<CR><LF>")
			var data strings.Builder
			for {
				part, err := reader.ReadString('\n')
				if err != nil {
					return
				}
				if part == ".\r\n" {
					break
				}
				data.WriteString(strings.TrimPrefix(part, "."))
			}
			current.Data = data.String()
			write(s.reply(current))
		case command == "RSET", command == "NOOP":
			write("250 OK")
		case command == "QUIT":
			write("221 Bye")
			return
		default:
			write("502 Command not implemented")
		}
	}
}

// refuseRecipient makes RCPT TO for address answer reply.
func (s *smtpServer) refuseRecipient(address, reply string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.refused == nil {
		s.refused = map[string]string{}
	}
	s.refused[address] = reply
}

func (s *smtpServer) refusal(address string) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.refused[address]
}

func (s *smtpServer) reply(mail receivedMail) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.replies) > 0 {
		reply := s.replies[0]
		s.replies = s.replies[1:]
		if code, err := strconv.Atoi(reply[:3]); err == nil && code >= 400 {
			return reply
		}
	}
	s.received = append(s.received, mail)
	return "250 Queued"
}
