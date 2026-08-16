package telegram

import (
	"bytes"
	"context"
	"testing"
)

func TestPNGQRCodeEncoderGeneratesPNGForHTTPSSubscription(t *testing.T) {
	png, err := (PNGQRCodeEncoder{}).Encode("https://vpn.example.com/sub/private-token")
	if err != nil {
		t.Fatalf("Encode() error = %v", err)
	}
	if !bytes.HasPrefix(png, []byte("\x89PNG\r\n\x1a\n")) || len(png) < 100 {
		t.Fatalf("Encode() returned invalid PNG, bytes=%d", len(png))
	}
}

func TestPNGQRCodeEncoderRejectsNonHTTPSOrCredentialSizedPayload(t *testing.T) {
	for _, value := range []string{"", "http://vpn.example.com/sub/token", "https://vpn.example.com/" + string(make([]byte, 5000))} {
		if _, err := (PNGQRCodeEncoder{}).Encode(value); err == nil {
			t.Fatalf("Encode(%q) error = nil", value)
		}
	}
}

type qrEncoderStub struct {
	png     []byte
	content string
}

func (stub *qrEncoderStub) Encode(content string) ([]byte, error) {
	stub.content = content
	return append([]byte(nil), stub.png...), nil
}

func TestRunnerSendsStatusQRCodePrivately(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	client := &runnerClientStub{
		updates: []Update{{UpdateID: 15, Message: &APIMessage{Chat: Chat{ID: 99, Type: ChatPrivate}, From: &User{ID: 12345}, Text: "/status"}}},
		cancel:  cancel,
	}
	encoder := &qrEncoderStub{png: []byte("png")}
	handler := runnerHandlerStub{reply: Reply{Text: "status", QRContent: "https://vpn.example.com/sub/private"}, handled: true}
	runner := NewRunner(client, handler, nil).WithQRCodeEncoder(encoder)

	if err := runner.Run(ctx); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if encoder.content != handler.reply.QRContent || len(client.photos) != 1 || client.photos[0].chatID != 99 || client.photos[0].caption != "status" {
		t.Fatalf("encoder=%q photos=%#v", encoder.content, client.photos)
	}
	if len(client.sent) != 0 {
		t.Fatalf("text replies = %#v, want photo caption only", client.sent)
	}
}
