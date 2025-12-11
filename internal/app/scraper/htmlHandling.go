package scraper

import (
	"bufio"
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/box1bs/monocle/internal/model"
	"github.com/box1bs/monocle/pkg/logger"
	"github.com/box1bs/monocle/pkg/parser"
	"golang.org/x/net/html"
)

type linkToken struct {
	Link 		*url.URL
	SameDomain 	bool
}

func (ws *WebScraper) fetchHTMLcontent(cur *url.URL, ctx context.Context, norm string, rls *parser.RobotsTxt, gd int) ([]*linkToken, error) {
	ws.rlMu.RLock()
	rl := ws.rlMap[cur.Host]
	ws.rlMu.RUnlock()
	doc, err := ws.getHTML(cur.String(), rl, numOfTries)
    if err != nil {
		ws.log.Write(logger.NewMessage(logger.SCRAPER_LAYER, logger.ERROR, "error getting html: %s, with error: %v", cur, err))
        return nil, err
    }
	if doc == "" {
		ws.log.Write(logger.NewMessage(logger.SCRAPER_LAYER, logger.DEBUG, "empty html content on page: %s", cur))
        return nil, fmt.Errorf("empty html content on page: %s", cur)
	}
	
	hashed := sha256.Sum256([]byte(norm))
    document := &model.Document{
        Id: hashed,
        URL: cur.String(),
    }

	c, cancel := context.WithTimeout(ctx, deadlineTime)
	defer cancel()
    links, passages := ws.parseHTMLStream(c, doc, cur, rls, gd)
	if len(links) != 0 {
		ws.lru.Put(hashed, links)
	}

	fullText := strings.Builder{}
	for _, passage := range passages {
		fullText.WriteString(passage.Text)
	}
	
	var ok bool
	select {
	case document.WordVec, ok = <-ws.putDocReq(fullText.String(), ws.globalCtx):
		if !ok {
			ws.log.Write(logger.NewMessage(logger.SCRAPER_LAYER, logger.CRITICAL_ERROR, "error vectorizing document for page: %s", cur))
			return nil, fmt.Errorf("error vectoriing document for page: %s", cur)
		}
	case <-ws.globalCtx.Done():
		ws.log.Write(logger.NewMessage(logger.SCRAPER_LAYER, logger.CRITICAL_ERROR, "timeout vectorizing document for page: %s", cur))
		return nil, fmt.Errorf("context canceled")
	}

	return links, ws.idx.HandleDocumentWords(document, passages)
}

func (ws *WebScraper) parseHTMLStream(ctx context.Context, htmlContent string, baseURL *url.URL, rules *parser.RobotsTxt, currentDeep int) (links []*linkToken, pasages []model.Passage) {
	tokenizer := html.NewTokenizer(strings.NewReader(htmlContent))
	var tagStack [][2]byte
	var garbageTagStack []string
	links = make([]*linkToken, 0)
	visit := make([]*linkToken, 0)
	
	tokenCount := 0
	const checkContextEvery = 10

	for {
		tokenCount++
		if tokenCount % checkContextEvery == 0 {
			select {
			case <-ctx.Done():
				if len(visit) != 0 {
					links = append(links, visit...)
				}
				return
			default:
			}
		}

		tokenType := tokenizer.Next()
		if tokenType == html.ErrorToken {
			if tokenizer.Err() == io.EOF {
				break
			}
			ws.log.Write(logger.NewMessage(logger.SCRAPER_LAYER, logger.ERROR, "error parsing HTML with url: %s", baseURL.String()))
			break
		}

		switch tokenType {
		case html.StartTagToken:
			if len(garbageTagStack) > 0 {
				continue
			}

			t := tokenizer.Token()
			tagName := strings.ToLower(t.Data)
			switch tagName {
			case "h1", "h2":
				tagStack = append(tagStack, [2]byte{'h', tagName[1]})

			case "div":
				for _, attr := range t.Attr {
					if attr.Key == "class" || attr.Key == "id" {
						val := strings.ToLower(attr.Val)
						if strings.Contains(val, "ad") || strings.Contains(val, "banner") || strings.Contains(val, "promo") {
							garbageTagStack = append(garbageTagStack, tagName)
							break
						}
					}
				}

			case "a":
				for _, attr := range t.Attr {
					if strings.ToLower(attr.Key) == "href" {
						link, err := makeAbsoluteURL(attr.Val, baseURL)
						if err != nil {
							break
						}
						if link != "" {
							normalized, err := normalizeUrl(link)
							if err != nil {
								ws.log.Write(logger.NewMessage(logger.SCRAPER_LAYER, logger.ERROR, "error normalizing url: %s, with error: %v", link, err))
								break
							}
							uri, err := url.Parse(link)
							if err != nil || uri == nil {
								ws.log.Write(logger.NewMessage(logger.SCRAPER_LAYER, logger.ERROR, "error parsing link: %v", err))
								break
							}
							if rules != nil {
								if !rules.IsAllowed(userAgent, uri.Path) {
									break
								}
							}
							same := isSameOrigin(uri, baseURL)
							if depth, vis := ws.visited.Load(normalized); vis {
								if depth.(int) > currentDeep {
									visit = append(visit, &linkToken{Link: uri, SameDomain: same})
								}
								break
							}
							links = append(links, &linkToken{Link: uri, SameDomain: same})
						}
						break
					}
				}

			case "script", "style", "iframe", "aside", "nav", "footer":
				garbageTagStack = append(garbageTagStack, tagName)

			}

		case html.EndTagToken:
			t := tokenizer.Token()
			tagName := strings.ToLower(t.Data)
			if tagName[0] == 'h' {
				if len(tagStack) > 0 && len(tagName) > 1 && tagStack[len(tagStack)-1][1] == tagName[1] {
					tagStack = tagStack[:len(tagStack)-1]
				}
			}

			if len(garbageTagStack) > 0 && garbageTagStack[len(garbageTagStack)-1] == tagName {
				garbageTagStack = garbageTagStack[:len(garbageTagStack)-1]
			}

		case html.TextToken:
			if len(garbageTagStack) > 0 {
				continue
			}

			if len(tagStack) > 0 {
				text := strings.TrimSpace(string(tokenizer.Text()))
				if text != "" {
					pasages = append(pasages, model.NewTypeTextObj[model.Passage]('h', text, 0))
				}
				continue
			}

			text := strings.TrimSpace(string(tokenizer.Text()))
			if text != "" {
				pasages = append(pasages, model.NewTypeTextObj[model.Passage]('b', text, 0))
			}

		}
	}
	if len(visit) != 0 {
		links = append(links, visit...)
	}
	return
}

func (ws *WebScraper) getHTML(URL string, rl *rateLimiter, try int) (string, error) {
	if try <= 0 {
		return "", fmt.Errorf("http status code: 419, and max amount of tries was reached")
	}

	req, err := http.NewRequest("GET", URL, nil)
	if err != nil {
		return "", err
	}

	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Accept", "text/html")

	rl.GetToken(ws.globalCtx) // не должно ложить приложение, но в целом по желанию
	resp, err := ws.client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		if resp.StatusCode == http.StatusTooManyRequests && !ws.checkContext(ws.globalCtx, URL) {
			<-time.After(deadlineTime)
			return ws.getHTML(URL, rl, try-1)
		} else {
			return "", fmt.Errorf("non-200 status code: %d", resp.StatusCode)
		}
	}

	if ws.checkContext(ws.globalCtx, URL) {
		return "", fmt.Errorf("context canceled")
	}

	ctype := resp.Header.Get("Content-Type")
	if !strings.Contains(strings.ToLower(ctype), "text/html") {
		return "", fmt.Errorf("unsupported content type: %s", ctype)
	}

	var builder strings.Builder
	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 64*1024), 10*1024*1024)

	for scanner.Scan() {
		builder.WriteString(scanner.Text())

		select {
		case <-ws.globalCtx.Done():
			return builder.String(), nil
		default:
			builder.WriteString(scanner.Text())
		}
	}
	return builder.String(), scanner.Err()
}