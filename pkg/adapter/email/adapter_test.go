package email

import (
	"bufio"
	"context"
	"io"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/labring/sealos-notify/pkg/adapter"
)

func TestNewDefaultsAndValidation(t *testing.T) {
	a, err := New(map[string]interface{}{
		"host":   "smtp.example.com",
		"from":   "notify@example.com",
		"useTLS": false,
	})
	if err != nil {
		t.Fatalf("New returned error: %v", err)
	}
	if a.config.Port != 25 {
		t.Fatalf("Port = %d, want 25", a.config.Port)
	}
	if a.config.ContentType != "text/html" {
		t.Fatalf("ContentType = %q, want text/html", a.config.ContentType)
	}
	if a.config.TLSMode != "none" {
		t.Fatalf("TLSMode = %q, want none", a.config.TLSMode)
	}
	if err := a.Validate(); err != nil {
		t.Fatalf("Validate returned error: %v", err)
	}
}

func TestNewRequiresRequiredFields(t *testing.T) {
	tests := []struct {
		name string
		data map[string]interface{}
	}{
		{name: "missing host", data: map[string]interface{}{"from": "notify@example.com"}},
		{name: "missing from", data: map[string]interface{}{"host": "smtp.example.com"}},
		{name: "invalid from", data: map[string]interface{}{"host": "smtp.example.com", "from": "bad"}},
		{name: "password missing", data: map[string]interface{}{"host": "smtp.example.com", "from": "notify@example.com", "username": "notify"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := New(tt.data); err == nil {
				t.Fatal("expected error")
			}
		})
	}
}

func TestBuildMIMEMessage(t *testing.T) {
	msg, err := buildMIMEMessage(Config{
		From:        "notify@example.com",
		FromName:    "Sealos Notify",
		ContentType: "text/html",
	}, "alice@example.com", "Maintenance Notice", "<p>Hello</p>")
	if err != nil {
		t.Fatalf("buildMIMEMessage returned error: %v", err)
	}
	got := string(msg)
	for _, want := range []string{
		"From: \"Sealos Notify\" <notify@example.com>",
		"To: alice@example.com",
		"Subject: Maintenance Notice",
		`Content-Type: text/html; charset="UTF-8"`,
		"<p>Hello</p>",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("message missing %q:\n%s", want, got)
		}
	}
}

func TestSendUsesSMTP(t *testing.T) {
	server := newFakeSMTPServer(t)
	defer server.close()

	a, err := New(map[string]interface{}{
		"host":           server.host,
		"port":           server.port,
		"from":           "notify@example.com",
		"fromName":       "Sealos Notify",
		"tlsMode":        "none",
		"timeoutSeconds": 2,
	})
	if err != nil {
		t.Fatalf("New returned error: %v", err)
	}

	resp, err := a.Send(context.Background(), &adapter.SendRequest{
		RecipientValue: "alice@example.com",
		Subject:        "Maintenance",
		Body:           "<p>body</p>",
	})
	if err != nil {
		t.Fatalf("Send returned error: %v", err)
	}
	if resp == nil || !resp.Success {
		t.Fatalf("Send response = %#v", resp)
	}
	if !strings.Contains(server.message(), "<p>body</p>") {
		t.Fatalf("SMTP server did not receive body: %s", server.message())
	}
}

type fakeSMTPServer struct {
	listener net.Listener
	host     string
	port     int
	done     chan struct{}
	body     strings.Builder
}

func newFakeSMTPServer(t *testing.T) *fakeSMTPServer {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	addr, ok := listener.Addr().(*net.TCPAddr)
	if !ok {
		_ = listener.Close()
		t.Fatalf("listener address type = %T, want *net.TCPAddr", listener.Addr())
	}
	server := &fakeSMTPServer{
		listener: listener,
		host:     "127.0.0.1",
		port:     addr.Port,
		done:     make(chan struct{}),
	}
	go server.serve(t)
	return server
}

func (s *fakeSMTPServer) serve(t *testing.T) {
	defer close(s.done)
	conn, err := s.listener.Accept()
	if err != nil {
		return
	}
	defer conn.Close()
	reader := bufio.NewReader(conn)
	writeLine(t, conn, "220 fake smtp")

	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			return
		}
		cmd := strings.ToUpper(strings.TrimSpace(line))
		switch {
		case strings.HasPrefix(cmd, "EHLO"):
			writeLine(t, conn, "250-fake")
			writeLine(t, conn, "250 OK")
		case strings.HasPrefix(cmd, "MAIL FROM:"):
			writeLine(t, conn, "250 OK")
		case strings.HasPrefix(cmd, "RCPT TO:"):
			writeLine(t, conn, "250 OK")
		case cmd == "DATA":
			writeLine(t, conn, "354 End data with <CR><LF>.<CR><LF>")
			for {
				dataLine, err := reader.ReadString('\n')
				if err != nil {
					return
				}
				if strings.TrimSpace(dataLine) == "." {
					break
				}
				s.body.WriteString(dataLine)
			}
			writeLine(t, conn, "250 OK")
		case cmd == "QUIT":
			writeLine(t, conn, "221 Bye")
			return
		default:
			writeLine(t, conn, "250 OK")
		}
	}
}

func (s *fakeSMTPServer) close() {
	_ = s.listener.Close()
	select {
	case <-s.done:
	case <-time.After(time.Second):
	}
}

func (s *fakeSMTPServer) message() string {
	return s.body.String()
}

func writeLine(t *testing.T, writer io.Writer, line string) {
	t.Helper()
	if _, err := writer.Write([]byte(line + "\r\n")); err != nil {
		t.Errorf("write SMTP line: %v", err)
	}
}
