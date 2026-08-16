package telegram

import (
	"bytes"
	"errors"
	"net/url"
	"strings"

	qrcode "github.com/yeqown/go-qrcode/v2"
	"github.com/yeqown/go-qrcode/writer/standard"
)

const maxQRCodeContentBytes = 2048

type PNGQRCodeEncoder struct{}

type bufferWriteCloser struct{ bytes.Buffer }

func (*bufferWriteCloser) Close() error { return nil }

func (PNGQRCodeEncoder) Encode(content string) ([]byte, error) {
	if len(content) == 0 || len(content) > maxQRCodeContentBytes {
		return nil, errors.New("QR content length is invalid")
	}
	parsed, err := url.Parse(content)
	if err != nil || parsed.Scheme != "https" || parsed.Host == "" || parsed.User != nil || parsed.Fragment != "" || strings.TrimSpace(parsed.Hostname()) == "" {
		return nil, errors.New("QR content URL is invalid")
	}
	code, err := qrcode.NewWith(content, qrcode.WithErrorCorrectionLevel(qrcode.ErrorCorrectionMedium))
	if err != nil {
		return nil, errors.New("create QR code")
	}
	output := &bufferWriteCloser{}
	writer := standard.NewWithWriter(output,
		standard.WithBuiltinImageEncoder(standard.PNG_FORMAT),
		standard.WithQRWidth(8),
		standard.WithBorderWidth(4),
	)
	if err := code.Save(writer); err != nil {
		return nil, errors.New("render QR image")
	}
	return append([]byte(nil), output.Bytes()...), nil
}
