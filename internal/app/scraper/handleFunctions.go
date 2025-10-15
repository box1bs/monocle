package scraper

import (
	"bytes"
	"context"
	"encoding/xml"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"unicode"

	"golang.org/x/net/html/charset"
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

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	body = bytes.TrimPrefix(bytes.ReplaceAll(body, []byte(`xmlns="http://www.sitemaps.org/schemas/sitemap/0.9"`), []byte("")), []byte("\xef\xbb\xbf"))
	return decodeSitemap(bytes.NewReader(bytes.TrimPrefix(body, []byte("\xef\xbb\xbf"))), limiter)
}

func decodeSitemap(r io.Reader, limiter int) ([]string, error) {
	var urls []string
	dec := xml.NewDecoder(r)
	dec.CharsetReader = charset.NewReaderLabel
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

func isSameOrigin(rawURL *url.URL, baseURL *url.URL) bool {
	return strings.Contains(baseURL.Hostname(), rawURL.Hostname())
}

func checkContext(ctx context.Context) bool {
	select {
		case <-ctx.Done():
			return true
		default:
	}
	return false
}