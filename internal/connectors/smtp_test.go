package connectors

import (
	"bufio"
	"context"
	"fmt"
	"net"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestSMTPSendsNativeMessage(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	messageBody := make(chan string, 1)
	serverError := make(chan error, 1)
	go serveTestSMTP(listener, messageBody, serverError)

	address := listener.Addr().(*net.TCPAddr)
	registry := NewRegistry(nil)
	err = registry.Send(context.Background(), "smtp", map[string]any{
		"host": "127.0.0.1", "port": float64(address.Port), "security": "none",
		"from": "Marvo <marvo@example.test>", "to": "reader@example.test",
	}, Message{
		DeliveryID: strings.Repeat("a", 32), ActivityID: strings.Repeat("b", 32),
		Title: "研究完成", Content: "结果已整理。", CreatedAt: time.Now().UTC(),
	})
	if err != nil {
		t.Fatal(err)
	}
	select {
	case body := <-messageBody:
		if !strings.Contains(body, "Subject: =?UTF-8?") || !strings.Contains(body, "Message-ID: <"+strings.Repeat("a", 32)+"@marvo.local>") {
			t.Fatalf("SMTP message headers = %q", body)
		}
	case err := <-serverError:
		t.Fatal(err)
	case <-time.After(2 * time.Second):
		t.Fatal("SMTP message was not received")
	}
}

func serveTestSMTP(listener net.Listener, received chan<- string, failed chan<- error) {
	connection, err := listener.Accept()
	if err != nil {
		failed <- err
		return
	}
	defer connection.Close()
	reader := bufio.NewReader(connection)
	writer := bufio.NewWriter(connection)
	write := func(response string) bool {
		if _, err := writer.WriteString(response + "\r\n"); err != nil {
			failed <- err
			return false
		}
		if err := writer.Flush(); err != nil {
			failed <- err
			return false
		}
		return true
	}
	if !write("220 localhost ESMTP") {
		return
	}
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			failed <- err
			return
		}
		command := strings.ToUpper(strings.TrimSpace(line))
		switch {
		case strings.HasPrefix(command, "EHLO"):
			if !write("250 localhost") {
				return
			}
		case strings.HasPrefix(command, "MAIL FROM:"), strings.HasPrefix(command, "RCPT TO:"):
			if !write("250 OK") {
				return
			}
		case command == "DATA":
			if !write("354 End data with <CR><LF>.<CR><LF>") {
				return
			}
			var body strings.Builder
			for {
				line, err := reader.ReadString('\n')
				if err != nil {
					failed <- err
					return
				}
				if line == ".\r\n" {
					break
				}
				body.WriteString(line)
			}
			if !write("250 queued as " + strconv.Itoa(body.Len())) {
				return
			}
			received <- body.String()
		case command == "QUIT":
			_ = write("221 bye")
			return
		default:
			failed <- fmt.Errorf("unexpected SMTP command %q", command)
			return
		}
	}
}
