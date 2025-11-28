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

	"github.com/box1bs/monocle/pkg/logger"
	"github.com/box1bs/monocle/pkg/parser"
	"golang.org/x/net/html/charset"
)

const (
	BaseXMLPageError = "sitemap page"
	sitemap = "sitemap"
)

func (ws *WebScraper) fetchPageRulesAndOffers(ctx context.Context, cur *url.URL, rules *parser.RobotsTxt) ([]*linkToken, error) {
	if r, err := parser.FetchRobotsTxt(ctx, cur.String(), ws.client); r != "" && err == nil {
		robotsTXT := parser.ParseRobotsTxt(r)
		rules = robotsTXT
		ws.rlMu.Lock()
		if lim := ws.rlMap[cur.Host]; (lim == nil || lim.R == DefaultDelay) && rules.Rules["*"].Delay > 0 {
			ws.rlMap[cur.Host] = NewRateLimiter(rules.Rules["*"].Delay)
		} else if lim == nil {
			ws.rlMap[cur.Host] = NewRateLimiter(DefaultDelay)
		}
		ws.rlMu.Unlock()
	}

	return ws.prepareSitemapLinks(cur)
}

func (ws *WebScraper) haveSitemap(url *url.URL) ([]string, error) {
	sitemapURL := url.String()
	if !strings.Contains(sitemapURL, sitemap) {
		sitemapURL = strings.TrimSuffix(url.String(), "/")
		sitemapURL = sitemapURL + "/" + sitemap + ".xml"
	}

	urls, err := ws.processSitemap(url, sitemapURL)
	if err != nil {
		return nil, err
	}

	return urls, err
}

func decodeSitemap(r io.Reader) ([]string, error) {
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
			}
		}
	}

	return urls, nil
}

func (ws *WebScraper) processSitemap(baseURL *url.URL, sitemapURL string) ([]string, error) {
	sitemap, err := getSitemapURLs(sitemapURL, ws.client)
	if err != nil {
		return nil, err
	}

	var nextUrls []string
	for _, item := range sitemap {
		abs, err := makeAbsoluteURL(item, baseURL)
		if abs == "" || err != nil {
			continue
		}
		nextUrls = append(nextUrls, abs)
	}

	return nextUrls, nil
}

func getSitemapURLs(URL string, cli *http.Client) ([]string, error) {
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
	return decodeSitemap(bytes.NewReader(bytes.TrimPrefix(body, []byte("\xef\xbb\xbf"))))
}

func (ws *WebScraper) prepareSitemapLinks(current *url.URL) ([]*linkToken, error) {
	links := make([]*linkToken, 0)
	var urls []string
	var err error
	if urls, err = ws.haveSitemap(current); err == nil && len(urls) > 0 {
		for _, link := range urls {
			parsed, err := url.Parse(link)
			if err != nil {
				ws.log.Write(logger.NewMessage(logger.SCRAPER_LAYER, logger.ERROR, "error parsing link: %v", err))
				continue
			}
			same := isSameOrigin(parsed, current)
			if !same && ws.cfg.OnlySameDomain {
				continue
			}
			links = append(links, &linkToken{Link: parsed, SameDomain: same})
		}
	}
	if err == nil {
		err = errors.New(BaseXMLPageError)
	}
	return links, err
}