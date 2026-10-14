package email

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"net/smtp"
	"net/textproto"
	"strconv"
	"strings"
	"time"
)

const defaultSMTPTimeout = 60 * time.Second

// SMTPConfig is the outbound relay. Host empty means outbound email is disabled.
type SMTPConfig struct {
	Host     string
	Port     int
	Username string
	Password string
	StartTLS bool
}

// Configured reports whether an SMTP relay is set.
func (c SMTPConfig) Configured() bool { return c.Host != "" }

// SMTPConfigFromEnv reads SMTP_HOST, SMTP_PORT (587), SMTP_USERNAME, SMTP_PASSWORD and SMTP_STARTTLS (true).
func SMTPConfigFromEnv(getenv func(string) string) (SMTPConfig, error) {
	config := SMTPConfig{Host: strings.TrimSpace(getenv("SMTP_HOST")), Port: 587, Username: getenv("SMTP_USERNAME"),
		Password: getenv("SMTP_PASSWORD"), StartTLS: true}
	if value := getenv("SMTP_PORT"); value != "" {
		port, err := strconv.Atoi(value)
		if err != nil || port < 1 || port > 65535 {
			return SMTPConfig{}, fmt.Errorf("SMTP_PORT must be a TCP port, got %q", value)
		}
		if port == 465 {
			return SMTPConfig{}, fmt.Errorf("SMTP_PORT 465 (implicit TLS) is not supported; use port 587 with STARTTLS")
		}
		config.Port = port
	}
	if value := getenv("SMTP_STARTTLS"); value != "" {
		enabled, err := strconv.ParseBool(value)
		if err != nil {
			return SMTPConfig{}, fmt.Errorf("SMTP_STARTTLS must be true or false, got %q", value)
		}
		config.StartTLS = enabled
	}
	return config, nil
}

// SMTPSender delivers composed messages through the configured relay.
type SMTPSender struct {
	Config SMTPConfig
}

// Send delivers one message. Errors keep the server reply so IsPermanent can classify them.
func (s SMTPSender) Send(ctx context.Context, from string, to []string, message []byte) (err error) {
	address := net.JoinHostPort(s.Config.Host, strconv.Itoa(s.Config.Port))
	dialer := net.Dialer{Timeout: 15 * time.Second}
	conn, err := dialer.DialContext(ctx, "tcp", address)
	if err != nil {
		return fmt.Errorf("connect %s: %w", address, err)
	}
	deadline, ok := ctx.Deadline()
	if !ok {
		deadline = time.Now().Add(defaultSMTPTimeout)
	}
	if err := conn.SetDeadline(deadline); err != nil {
		_ = conn.Close()
		return fmt.Errorf("set SMTP deadline: %w", err)
	}
	client, err := smtp.NewClient(conn, s.Config.Host)
	if err != nil {
		_ = conn.Close()
		return fmt.Errorf("SMTP greeting: %w", err)
	}
	defer func() {
		if closeErr := client.Close(); err == nil && closeErr != nil && !errors.Is(closeErr, net.ErrClosed) {
			err = fmt.Errorf("close SMTP connection: %w", closeErr)
		}
	}()
	if err := client.Hello(Domain(from)); err != nil {
		return fmt.Errorf("SMTP EHLO: %w", err)
	}
	if s.Config.StartTLS {
		if supported, _ := client.Extension("STARTTLS"); !supported {
			return fmt.Errorf("SMTP server %s does not offer STARTTLS", address)
		}
		if err := client.StartTLS(&tls.Config{ServerName: s.Config.Host, MinVersion: tls.VersionTLS12}); err != nil {
			return fmt.Errorf("SMTP STARTTLS: %w", err)
		}
	}
	if s.Config.Username != "" {
		if err := client.Auth(smtp.PlainAuth("", s.Config.Username, s.Config.Password, s.Config.Host)); err != nil {
			return fmt.Errorf("SMTP AUTH: %w", err)
		}
	}
	if err := client.Mail(from); err != nil {
		return fmt.Errorf("SMTP MAIL FROM: %w", err)
	}
	for _, recipient := range to {
		if err := client.Rcpt(recipient); err != nil {
			return fmt.Errorf("SMTP RCPT TO: %w", err)
		}
	}
	writer, err := client.Data()
	if err != nil {
		return fmt.Errorf("SMTP DATA: %w", err)
	}
	if _, err := writer.Write(message); err != nil {
		return fmt.Errorf("SMTP write: %w", err)
	}
	if err := writer.Close(); err != nil {
		return fmt.Errorf("SMTP end of data: %w", err)
	}
	return client.Quit()
}

// IsPermanent reports a 5xx SMTP reply: retrying the same message will not help.
func IsPermanent(err error) bool {
	var reply *textproto.Error
	return errors.As(err, &reply) && reply.Code >= 500
}
