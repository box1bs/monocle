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

func (ws *webScraper) fetchPageRulesAndOffers(ctx context.Context, cur *url.URL, rules *parser.RobotsTxt, depth, localDepth int) {
	if r, err := parser.FetchRobotsTxt(ctx, cur.String(), ws.client); r != "" && err == nil {
		robotsTXT := parser.ParseRobotsTxt(r)
		rules = robotsTXT
		ws.rlMu.Lock()
		if ex := ws.rlMap[cur.Host]; (ex == nil || ex.R == DefaultDelay) && rules.Rules["*"].Delay > 0 {
			ws.rlMap[cur.Host] = NewRateLimiter(rules.Rules["*"].Delay)
		} else if ex == nil {
			ws.rlMap[cur.Host] = NewRateLimiter(DefaultDelay)
		}
		ws.rlMu.Unlock()
	}
		
	ws.scrapeThroughtSitemap(ws.globalCtx, cur, rules, depth, localDepth)
}

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

func (ws *webScraper) scrapeThroughtSitemap(ctx context.Context, current *url.URL, rules *parser.RobotsTxt, dg, dl int) {
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
			n, err := normalizeUrl(link)
			if err != nil {
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

			localVisDepth := 0
			if _, v := ws.visited.Load(n); v {
				localVisDepth = dl + 1
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
				ws.ScrapeWithContext(c, parsed, rls, dg+1, localVisDepth)
			})
		}
	}
}