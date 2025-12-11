package scraper

import (
	"errors"
	"net/url"
	"strings"
)

func makeAbsoluteURL(rawURL string, baseURL *url.URL) (string, error) {
	rawURL = strings.TrimSpace(rawURL)
	if rawURL == "" {
		return "", errors.New("empty url")
	}

	if strings.HasPrefix(rawURL, "#") || strings.HasPrefix(strings.ToLower(rawURL), "javascript:") {
		return "", errors.New("ignored url type")
	}

	u, err := url.Parse(rawURL)
	if err != nil {
		return "", err
	}

	resolved := baseURL.ResolveReference(u)
	resolved.Fragment = ""
	if resolved.Scheme != "https" {
		return "", errors.New("invalid protocol scheme: " + resolved.Scheme)
	}
	return resolved.String(), nil
}

func normalizeUrl(rawUrl string) (string, error) {
	p, err := url.Parse(rawUrl)
	if err != nil {
		return "", err
	}

	host := strings.TrimPrefix(strings.ToLower(p.Hostname()), "www.")

	path := p.Path
	if strings.Contains(path, "//") {
		path = strings.ReplaceAll(path, "//", "/")
	}
	path = strings.TrimSuffix(path, "/")

	query := p.Query().Encode()
	var sb strings.Builder
	sb.WriteString(host)
	sb.WriteString(path)
	if query != "" {
		sb.WriteString("?")
		sb.WriteString(query)
	}
	return sb.String(), nil
}

func isSameOrigin(rawURL *url.URL, baseURL *url.URL) bool {
	return strings.Contains(baseURL.Hostname(), rawURL.Hostname())
}