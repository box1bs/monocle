package scraper

import (
	"encoding/xml"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"unicode"
)

func makeAbsoluteURL(rawURL, baseURL string) (string, error) {
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

	base, err := url.Parse(baseURL)
	if err != nil {
		return "", err
	}
	resolved := base.ResolveReference(u)
	return resolved.String(), nil
}

func normalizeUrl(rawUrl string) (string, error) {
	cleanUrl := strings.Map(func(r rune) rune {
		if unicode.IsSpace(r) || unicode.IsControl(r) {
			return -1
		}
		return r
	}, rawUrl)

	uri := urlRegex.ReplaceAllString(cleanUrl, "")

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

func getSitemapURLs(URL string, cli *http.Client, limiter int) ([]string, error) {
	resp, err := cli.Get(URL)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	return decodeSitemap(resp.Body, limiter)
}

func decodeSitemap(r io.Reader, limiter int) ([]string, error) {
	var urls []string
	dec := xml.NewDecoder(r)
	for {
		token, err := dec.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}

		if element, ok := token.(xml.StartElement); ok {
			if element.Name.Local == "loc" {
				var url string
				if err := dec.DecodeElement(&url, &element); err != nil {
					continue
				}
				urls = append(urls, url)
				if len(urls) >= limiter {
					return urls, nil
				}
			}
		}
	}

	return urls, nil
}

func isSameOrigin(rawURL string, baseURL string) (bool, error) {
	parsedURL, err := url.Parse(rawURL)
	if err != nil {
		return false, err
	}

	parsedBaseURL, err := url.Parse(baseURL)
	if err != nil || !strings.Contains(parsedBaseURL.Hostname(), parsedURL.Hostname()) {
		return false, err
	}

	return true, nil
}