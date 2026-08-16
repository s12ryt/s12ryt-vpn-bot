package subscription

import (
	"encoding/base64"
	"errors"
	"net/url"
	"strings"
)

type LinkBuilder struct {
	baseURL string
}

func NewLinkBuilder(baseURL string) (LinkBuilder, error) {
	baseURL = strings.TrimSpace(baseURL)
	parsed, err := url.Parse(baseURL)
	if err != nil || !parsed.IsAbs() || !strings.EqualFold(parsed.Scheme, "https") || parsed.Hostname() == "" {
		return LinkBuilder{}, errors.New("subscription base URL must be an absolute HTTPS URL")
	}
	if parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return LinkBuilder{}, errors.New("subscription base URL contains forbidden components")
	}
	parsed.Scheme = "https"
	parsed.Path = strings.TrimRight(parsed.Path, "/")
	parsed.RawPath = ""
	return LinkBuilder{baseURL: parsed.String()}, nil
}

func (builder LinkBuilder) SubscriptionURL(token string) (string, error) {
	decoded, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil || len(decoded) != 32 || base64.RawURLEncoding.EncodeToString(decoded) != token {
		return "", errors.New("subscription token is invalid")
	}
	link, err := url.JoinPath(builder.baseURL, "sub", token)
	if err != nil {
		return "", errors.New("build subscription URL")
	}
	return link, nil
}
