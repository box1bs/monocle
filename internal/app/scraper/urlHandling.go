package scraper

import (
	"errors"
	"net/url"
	"strings"
)

func makeAbsoluteURL(rawURL string, baseURL *url.URL) (string, error) {
	if rawURL == "" {
		return "", errors.New("empty url")
	}

	u, err := url.Parse(rawURL)
	if err != nil {
		return "", err
	}

	if u.IsAbs() {
		return u.String(), nil
	}

	if u.Host == "" && !strings.HasPrefix(rawURL, "/") && !strings.HasPrefix(rawURL, "./") && !strings.HasPrefix(rawURL, "../") {
		u2, err := url.Parse("https://" + rawURL)
		if err == nil && u2.Host != "" {
			return u2.String(), nil
		}
	}

	if u.Host != "" && u.Scheme == "" {
		u.Scheme = "https"
		return u.String(), nil
	}

	resolved := baseURL.ResolveReference(u)
	return resolved.String(), nil
}

func normalizeUrl(rawUrl string) (string, error) {
	uri := urlRegex.ReplaceAllString(strings.TrimSpace(rawUrl), "")

	parsedUrl, err := url.Parse(uri)
	if err != nil {
		return "", err
	}

	parsedUrl.Host = strings.TrimPrefix(parsedUrl.Host, "www.")

	var normalized strings.Builder
	normalized.WriteString(strings.ToLower(parsedUrl.Host))
	path := strings.ToLower(strings.TrimSuffix(parsedUrl.Path, "/"))
    if !strings.HasPrefix(path, "/") && path != "" {
        normalized.WriteString("/" + path)
    }

	return normalized.String(), nil
}

func isSameOrigin(rawURL *url.URL, baseURL *url.URL) bool {
	return strings.Contains(baseURL.Hostname(), rawURL.Hostname())
}