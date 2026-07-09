// Package email provides an SMTP adapter for email notifications.
package email

import (
	"bytes"
	"context"
	"crypto/tls"
	"fmt"
	"mime"
	"mime/quotedprintable"
	"net"
	"net/mail"
	"net/smtp"
	"strconv"
	"strings"
	"time"

	"github.com/labring/sealos-notify/pkg/adapter"
)

const defaultTimeout = 10 * time.Second

// Config holds SMTP adapter configuration.
type Config struct {
	Host        string
	Port        int
	Username    string
	Password    string
	From        string
	FromName    string
	TLSMode     string
	ContentType string
	Timeout     time.Duration
}

// Adapter implements the email notification channel through SMTP.
type Adapter struct {
	config Config
}

// New creates a new SMTP email adapter from provider data.
// Expected keys: host, port, username, password, from, fromName, tlsMode,
// useTLS, contentType, timeoutSeconds.
func New(data map[string]interface{}) (*Adapter, error) {
	cfg := Config{
		Host:        getString(data, "host"),
		Port:        getInt(data, "port"),
		Username:    getString(data, "username"),
		Password:    getString(data, "password"),
		From:        getString(data, "from"),
		FromName:    firstNonEmpty(getString(data, "fromName"), getString(data, "from_name")),
		TLSMode:     getString(data, "tlsMode"),
		ContentType: getString(data, "contentType"),
		Timeout:     defaultTimeout,
	}
	if cfg.Port == 0 {
		cfg.Port = 25
	}
	if cfg.ContentType == "" {
		cfg.ContentType = "text/html"
	}
	if timeoutSeconds := getInt(data, "timeoutSeconds"); timeoutSeconds > 0 {
		cfg.Timeout = time.Duration(timeoutSeconds) * time.Second
	}
	if cfg.TLSMode == "" {
		if useTLS, ok := getBool(data, "useTLS"); ok {
			if useTLS {
				cfg.TLSMode = "auto"
			} else {
				cfg.TLSMode = "none"
			}
		}
	}

	a := &Adapter{config: cfg}
	if err := a.Validate(); err != nil {
		return nil, err
	}
	return a, nil
}

// Send delivers the pre-rendered subject/body to one email recipient.
func (a *Adapter) Send(ctx context.Context, req *adapter.SendRequest) (*adapter.SendResponse, error) {
	if strings.TrimSpace(req.RecipientValue) == "" {
		err := fmt.Errorf("email: recipient address is required")
		return &adapter.SendResponse{Success: false, Error: err}, nil
	}
	if _, err := mail.ParseAddress(req.RecipientValue); err != nil {
		err = fmt.Errorf("email: invalid recipient address %q: %w", req.RecipientValue, err)
		return &adapter.SendResponse{Success: false, Error: err}, nil
	}

	message, err := buildMIMEMessage(a.config, req.RecipientValue, req.Subject, req.Body)
	if err != nil {
		return &adapter.SendResponse{Success: false, Error: err}, nil
	}

	addr := net.JoinHostPort(a.config.Host, strconv.Itoa(a.config.Port))
	if err := a.send(ctx, addr, req.RecipientValue, message); err != nil {
		return &adapter.SendResponse{Success: false, Error: err}, nil
	}

	return &adapter.SendResponse{
		Success: true,
		Details: map[string]interface{}{
			"recipient": req.RecipientValue,
			"host":      a.config.Host,
			"port":      a.config.Port,
			"tls_mode":  normalizeTLSMode(a.config.TLSMode, a.config.Port),
		},
	}, nil
}

func (a *Adapter) send(ctx context.Context, addr string, to string, message []byte) error {
	result := make(chan error, 1)
	go func() {
		switch normalizeTLSMode(a.config.TLSMode, a.config.Port) {
		case "implicit":
			result <- a.sendImplicitTLS(addr, to, message)
		case "starttls":
			result <- a.sendSMTP(addr, to, message, true, true)
		case "opportunistic":
			result <- a.sendSMTP(addr, to, message, true, false)
		case "none":
			result <- a.sendSMTP(addr, to, message, false, false)
		default:
			result <- fmt.Errorf("email: unsupported tlsMode %q", a.config.TLSMode)
		}
	}()

	select {
	case <-ctx.Done():
		return ctx.Err()
	case err := <-result:
		return err
	}
}

func (a *Adapter) sendImplicitTLS(addr string, to string, message []byte) error {
	conn, err := tls.DialWithDialer(&net.Dialer{Timeout: a.config.Timeout}, "tcp", addr, &tls.Config{
		ServerName: a.config.Host,
		MinVersion: tls.VersionTLS12,
	})
	if err != nil {
		return fmt.Errorf("email: connect SMTP over TLS: %w", err)
	}
	client, err := smtp.NewClient(conn, a.config.Host)
	if err != nil {
		_ = conn.Close()
		return fmt.Errorf("email: create SMTP client: %w", err)
	}
	return a.sendWithClient(client, to, message, false, false)
}

func (a *Adapter) sendSMTP(addr string, to string, message []byte, allowStartTLS bool, requireStartTLS bool) error {
	conn, err := (&net.Dialer{Timeout: a.config.Timeout}).Dial("tcp", addr)
	if err != nil {
		return fmt.Errorf("email: connect SMTP: %w", err)
	}
	client, err := smtp.NewClient(conn, a.config.Host)
	if err != nil {
		_ = conn.Close()
		return fmt.Errorf("email: create SMTP client: %w", err)
	}
	return a.sendWithClient(client, to, message, allowStartTLS, requireStartTLS)
}

func (a *Adapter) sendWithClient(client *smtp.Client, to string, message []byte, allowStartTLS bool, requireStartTLS bool) error {
	defer client.Close()

	if ok, _ := client.Extension("STARTTLS"); allowStartTLS && ok {
		if err := client.StartTLS(&tls.Config{ServerName: a.config.Host, MinVersion: tls.VersionTLS12}); err != nil {
			return fmt.Errorf("email: start TLS: %w", err)
		}
	} else if requireStartTLS {
		return fmt.Errorf("email: SMTP server does not advertise STARTTLS")
	}

	if a.config.Username != "" {
		if ok, _ := client.Extension("AUTH"); ok {
			auth := smtp.PlainAuth("", a.config.Username, a.config.Password, a.config.Host)
			if err := client.Auth(auth); err != nil {
				return fmt.Errorf("email: authenticate SMTP: %w", err)
			}
		}
	}

	if err := client.Mail(a.config.From); err != nil {
		return fmt.Errorf("email: set sender: %w", err)
	}
	if err := client.Rcpt(to); err != nil {
		return fmt.Errorf("email: set recipient: %w", err)
	}
	writer, err := client.Data()
	if err != nil {
		return fmt.Errorf("email: open DATA: %w", err)
	}
	if _, err := writer.Write(message); err != nil {
		_ = writer.Close()
		return fmt.Errorf("email: write message: %w", err)
	}
	if err := writer.Close(); err != nil {
		return fmt.Errorf("email: finish DATA: %w", err)
	}
	if err := client.Quit(); err != nil {
		return fmt.Errorf("email: quit SMTP: %w", err)
	}
	return nil
}

// Name returns the adapter name.
func (a *Adapter) Name() string { return "email" }

// ChannelType returns the channel type.
func (a *Adapter) ChannelType() adapter.ChannelType { return adapter.ChannelTypeEmail }

// Validate validates SMTP adapter configuration.
func (a *Adapter) Validate() error {
	if strings.TrimSpace(a.config.Host) == "" {
		return fmt.Errorf("email: host is required")
	}
	if a.config.Port <= 0 {
		return fmt.Errorf("email: port is required")
	}
	if strings.TrimSpace(a.config.From) == "" {
		return fmt.Errorf("email: from is required")
	}
	if _, err := mail.ParseAddress(a.config.From); err != nil {
		return fmt.Errorf("email: invalid from address %q: %w", a.config.From, err)
	}
	if a.config.Username != "" && a.config.Password == "" {
		return fmt.Errorf("email: password is required when username is set")
	}
	switch normalizeTLSMode(a.config.TLSMode, a.config.Port) {
	case "implicit", "starttls", "opportunistic", "none":
		return nil
	default:
		return fmt.Errorf("email: unsupported tlsMode %q", a.config.TLSMode)
	}
}

func buildMIMEMessage(cfg Config, to string, subject string, body string) ([]byte, error) {
	fromAddress := mail.Address{Name: cfg.FromName, Address: cfg.From}
	var buf bytes.Buffer
	headers := []string{
		"From: " + fromAddress.String(),
		"To: " + to,
		"Subject: " + mime.QEncoding.Encode("UTF-8", subject),
		"MIME-Version: 1.0",
		fmt.Sprintf(`Content-Type: %s; charset="UTF-8"`, cfg.ContentType),
		"Content-Transfer-Encoding: quoted-printable",
	}
	for _, header := range headers {
		if _, err := fmt.Fprintf(&buf, "%s\r\n", header); err != nil {
			return nil, err
		}
	}
	if _, err := fmt.Fprint(&buf, "\r\n"); err != nil {
		return nil, err
	}
	qp := quotedprintable.NewWriter(&buf)
	if _, err := qp.Write([]byte(body)); err != nil {
		_ = qp.Close()
		return nil, err
	}
	if err := qp.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func normalizeTLSMode(mode string, port int) string {
	mode = strings.TrimSpace(strings.ToLower(mode))
	switch mode {
	case "", "auto":
		if port == 465 {
			return "implicit"
		}
		return "opportunistic"
	case "tls", "ssl", "implicit":
		return "implicit"
	case "starttls":
		return "starttls"
	case "opportunistic":
		return "opportunistic"
	case "plain", "none", "off":
		return "none"
	default:
		return mode
	}
}

func getString(data map[string]interface{}, key string) string {
	if v, ok := data[key]; ok {
		if s, ok := v.(string); ok {
			return strings.TrimSpace(s)
		}
	}
	return ""
}

func getInt(data map[string]interface{}, key string) int {
	v, ok := data[key]
	if !ok {
		return 0
	}
	switch value := v.(type) {
	case int:
		return value
	case int64:
		return int(value)
	case float64:
		return int(value)
	case string:
		parsed, _ := strconv.Atoi(strings.TrimSpace(value))
		return parsed
	default:
		return 0
	}
}

func getBool(data map[string]interface{}, key string) (bool, bool) {
	v, ok := data[key]
	if !ok {
		return false, false
	}
	switch value := v.(type) {
	case bool:
		return value, true
	case string:
		switch strings.ToLower(strings.TrimSpace(value)) {
		case "1", "true", "yes", "y", "on":
			return true, true
		case "0", "false", "no", "n", "off":
			return false, true
		default:
			return false, false
		}
	default:
		return false, false
	}
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}
