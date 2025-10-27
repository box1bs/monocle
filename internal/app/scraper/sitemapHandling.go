package scraper

import (
	"bytes"
	"context"
	"encoding/xml"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/box1bs/monocle/pkg/logger"
	"github.com/box1bs/monocle/pkg/parser"
	"golang.org/x/net/html/charset"
)

func (ws *webScraper) haveSitemap(url *url.URL) ([]string, error) {
	sitemapURL := url.String()
	if !strings.Contains(sitemapURL, sitemap) {
		sitemapURL = strings.TrimSuffix(url.String(), "/")
		sitemapURL = sitemapURL + "/" + sitemap
	}

	urls, err := ws.processSitemap(url, sitemapURL)
	if err != nil {
		return nil, err
	}

	return urls, err
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

func (ws *webScraper) processSitemap(baseURL *url.URL, sitemapURL string) ([]string, error) {
	sitemap, err := getSitemapURLs(sitemapURL, ws.client, ws.cfg.MaxLinksInPage)
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

func (ws *webScraper) scrapeThroughtSitemap(ctx context.Context, current *url.URL, rules *parser.RobotsTxt, d int) {
	if urls, err := ws.haveSitemap(current); err == nil && len(urls) > 0 {
		for _, link := range urls {
			if ws.checkContext(ctx, current.String()) {
				return
			}

			parsed, err := url.Parse(link)
			if err != nil {
				ws.log.Write(logger.NewMessage(logger.SCRAPER_LAYER, logger.ERROR, "error parsing link: %v", err))
				continue
			}
			same := isSameOrigin(parsed, current)

			if !same && ws.cfg.OnlySameDomain {
				continue
			}

			rls := rules
			if !same {
				rls = nil
			}

			ws.pool.Submit(func() {
				if ws.checkContext(ws.globalCtx, current.String()) {
					return
				}
				c, cancel := context.WithTimeout(ws.globalCtx, crawlTime)
				defer cancel()
				ws.rlMu.Lock()
				if ws.rlMap[parsed.Host] == nil {
					ws.rlMap[parsed.Host] = NewRateLimiter(DefaultDelay)
				}
				ws.rlMu.Unlock()
				ws.ScrapeWithContext(c, parsed, rls, d+1)
			})
		}
	}
}