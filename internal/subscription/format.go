package subscription

import (
	"errors"
	"strings"
)

type Format string

const (
	FormatBase64  Format = "base64"
	FormatSingBox Format = "sing-box"
	FormatClash   Format = "clash"
)

func NegotiateFormat(explicit, userAgent string) (Format, error) {
	if explicit != "" {
		switch Format(explicit) {
		case FormatBase64, FormatSingBox, FormatClash:
			return Format(explicit), nil
		default:
			return "", errors.New("subscription format is invalid")
		}
	}
	normalized := strings.ToLower(userAgent)
	if strings.Contains(normalized, "sing-box") {
		return FormatSingBox, nil
	}
	if strings.Contains(normalized, "mihomo") || strings.Contains(normalized, "clash") {
		return FormatClash, nil
	}
	return FormatBase64, nil
}
