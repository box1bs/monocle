package scraper

import (
	"crypto/sha256"
	"io"
	"log"
	"regexp"

	"github.com/box1bs/monocle/internal/model"
	"github.com/box1bs/monocle/pkg/parser"

	"bufio"
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"golang.org/x/net/html"
)

var userAgent = "Mozilla/5.0 (Windows NT 10.0; Win64; x64)"
var urlRegex = regexp.MustCompile(`^https?://`)

type indexer interface {
    HandleDocumentWords(*model.Document, []model.Passage) error
	IsCrawledContent([32]byte, []model.Passage) (bool, error)
}

type workerPool interface {
	Submit(func())
	Wait()
	Stop()
}

type webScraper struct {
	client         	*http.Client
	visited        	*sync.Map
	mu 				*sync.Mutex
	cfg 		  	*ConfigData
	pool           	workerPool
	idx 			indexer
	globalCtx		context.Context
	pageRank 		map[string]int
	write 			func(string)
	vectorize		func(string, context.Context) ([][]float64, error)
}

type ConfigData struct {
	StartURLs     	[]string
	Depth       	int
	MaxLinksInPage 	int
	Rate           	int
	DocNGramCount 	int
	OnlySameDomain  bool
}

const sitemap = "sitemap.xml"

func NewScraper(mp *sync.Map, cfg *ConfigData, wp workerPool, idx indexer, c context.Context, pr map[string]int, write func(string), vectorize func(string, context.Context) ([][]float64, error)) *webScraper {
	return &webScraper{
		client: &http.Client{
			Timeout: 5 * time.Second,
			Transport: &http.Transport{
				IdleConnTimeout:   5 * time.Second,
				DisableKeepAlives: false,
				ForceAttemptHTTP2: true,
			},
		},
		visited:        mp,
		mu: 			new(sync.Mutex),
		cfg: 			cfg,
		pool:           wp,
		idx: 			idx,
		globalCtx:		c,
		pageRank: 		pr,
		write: 			write,
		vectorize:		vectorize,
	}
}

type linkToken struct {
	link 		string
	sameDomain 	bool
}

func (ws *webScraper) Run() {
	for _, url := range ws.cfg.StartURLs {
		ws.pool.Submit(func() {
			ctx, cancel := context.WithTimeout(ws.globalCtx, 90 * time.Second)
			defer cancel()
			ws.ScrapeWithContext(ctx, url, nil, nil, 0)
		})
	}
	ws.pool.Wait()
	log.Printf("waiting for stoppnig worker pool")
	ws.pool.Stop()
}

func (ws *webScraper) ScrapeWithContext(ctx context.Context, currentURL string, rules *parser.RobotsTxt, rl *rateLimiter, depth int) {
    if checkContext(ctx) {return}

    if depth >= ws.cfg.Depth {
        return
    }
    
    normalized, err := normalizeUrl(currentURL)
    if err != nil {
        return
    }
    
    if _, loaded := ws.visited.LoadOrStore(normalized, struct{}{}); loaded {
        return
    }

	if r, err := parser.FetchRobotsTxt(ctx, currentURL, ws.client); r != "" && err == nil {
		robotsTXT := parser.ParseRobotsTxt(r)
		rules = robotsTXT
	} else if rules == nil || len(rules.Rules) == 0 {
		uri, err := url.Parse(currentURL)
		if err != nil {
			log.Printf("error parsing url %s, with error: %v", currentURL, err)
			return
		}
		if r, err = parser.FetchRobotsTxt(ctx, uri.Scheme + "://" + uri.Host + "/", ws.client); r != "" && err == nil {
			robotsTXT := parser.ParseRobotsTxt(r)
			rules = robotsTXT
			rl = newRateLimiter(rules.Rules["*"].Delay)
		}
	}
		
	if urls, err := ws.haveSitemap(currentURL); err == nil && len(urls) > 0 {
		for _, link := range urls {
			if checkContext(ctx) {return}

			same, err := isSameOrigin(link, currentURL)
			if err != nil {
				continue
			}

			if !same && ws.cfg.OnlySameDomain {
				continue
			}

			rls := rules
			newRl := rl //? посмотреть не заменяет ли это действующий объект
			if !same {
				rls = nil
				rl = nil
			}

            ws.pool.Submit(func() {
				if checkContext(ws.globalCtx) {return}
                c, cancel := context.WithTimeout(ws.globalCtx, 90 * time.Second)
				defer cancel()
        		ws.ScrapeWithContext(c, link, rls, newRl, depth+1)
            })
        }
	}
    
	c, cancel := context.WithTimeout(ctx, time.Second * 30)
	defer cancel()
    doc, err := ws.getHTML(c, currentURL, rl)
    if err != nil || doc == "" {
		log.Printf("error parsing page: %s, with error: %v\n", currentURL, err)
        return
    }

	if checkContext(ctx) {return}
	//ws.write(currentURL)

    document := &model.Document{
        Id: sha256.Sum256([]byte(normalized)),
        URL: currentURL,
    }

	c, cancel = context.WithTimeout(ctx, time.Second * 20)
	defer cancel()
    links, passages := ws.parseHTMLStream(c, doc, currentURL, rules)

	if crawled, err := ws.idx.IsCrawledContent(document.Id, passages); err != nil || crawled {
		return
	}

	fullText := strings.Builder{}
	for _, passage := range passages {
		fullText.WriteString(passage.Text)
	}

	c, cancel = context.WithTimeout(ctx, time.Second * 20)
	defer cancel()
	
	document.WordVec, err = ws.vectorize(fullText.String(), c)
	if err != nil {
		log.Printf("error vectorizing page: %s with error %v\n", currentURL, err)
		return
	}

	if checkContext(ctx) {return}

	ws.mu.Lock()
	ws.pageRank[normalized]++
	ws.mu.Unlock()
	
    if err := ws.idx.HandleDocumentWords(document, passages); err != nil {
		log.Printf("error handling words for page: %s with error %v\n", currentURL, err)
		return
	}

	if len(links) == 0 {
		log.Printf("empty links in page %s\n", currentURL)
		return
	}

    for _, link := range links {
		if checkContext(ctx) {return}

		if ws.cfg.OnlySameDomain && !link.sameDomain {
			continue
		}

		rls := rules
		newRl := rl
        if !link.sameDomain {
            rls = nil
			newRl = nil
        }

        ws.pool.Submit(func() {
			if checkContext(ws.globalCtx) {return}
			c, cancel := context.WithTimeout(ws.globalCtx, 90 * time.Second)
			defer cancel()
        	ws.ScrapeWithContext(c, link.link, rls, newRl, depth+1)
        })
    }
}

func (ws *webScraper) haveSitemap(url string) ([]string, error) {
	sitemapURL := strings.TrimSuffix(url, "/")
	sitemapURL = sitemapURL + "/" + sitemap

	urls, err := ws.processSitemap(url, sitemapURL)
    if err != nil {
		return nil, err
    }

	return urls, err
}

func (ws *webScraper) parseHTMLStream(ctx context.Context, htmlContent, baseURL string, rules *parser.RobotsTxt) (links []*linkToken, pasages []model.Passage) {
	tokenizer := html.NewTokenizer(strings.NewReader(htmlContent))
	var tagStack [][2]byte
	var garbageTagStack []string
	links = make([]*linkToken, 0, ws.cfg.MaxLinksInPage)

	tokenCount := 0
    const checkContextEvery = 10

	for {
		tokenCount++
		if tokenCount % checkContextEvery == 0 {
			select {
			case <-ctx.Done():
				return
			default:
			}
		}

		tokenType := tokenizer.Next()
		if tokenType == html.ErrorToken {
			if tokenizer.Err() == io.EOF {
				break
			}
			ws.write("error parsing HTML with url: " + baseURL)
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
						if link != "" && len(links) < ws.cfg.MaxLinksInPage {
							normalized, err := normalizeUrl(link)
							if err != nil {
								log.Printf("error normalizing url: %s, with error: %v", link, err)
								break
							}
							if _, vis := ws.visited.Load(normalized); vis {
								break
							}
							if rules != nil {
								uri, err := url.Parse(link)
								if err != nil {
									log.Printf("error parsing url: %s, with error: %v", link, err)
									break
								}
								if !rules.IsAllowed(userAgent, uri.Path) {
									break
								}
							}
							same, err := isSameOrigin(link, baseURL)
							if err != nil {
								log.Printf("error with url: %s, with error: %v", link, err)
								break
							}
							links = append(links, &linkToken{link: link, sameDomain: same})
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
				if len(tagStack) > 0 && tagStack[len(tagStack) - 1][1] == tagName[1] {
					tagStack = tagStack[:len(tagStack) - 1]
				}
			}

			if len(garbageTagStack) > 0 && garbageTagStack[len(garbageTagStack) - 1] == tagName {
				garbageTagStack = garbageTagStack[:len(garbageTagStack) - 1]
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
	return
}

func (ws *webScraper) getHTML(ctx context.Context, URL string, rl *rateLimiter) (string, error) {
    req, err := http.NewRequestWithContext(ctx, "GET", URL, nil)
    if err != nil {
        return "", err
    }

    req.Header.Set("User-Agent", userAgent)
    req.Header.Set("Accept", "text/html")

    resp, err := ws.client.Do(req)
    if err != nil {
        return "", err
    }
    defer resp.Body.Close()

    if resp.StatusCode < 200 || resp.StatusCode >= 300 {
        return "", fmt.Errorf("non-2xx status code: %d", resp.StatusCode)
    }

	if rl != nil {
		rl.getToken()
	}
    ctype := resp.Header.Get("Content-Type")
    if !strings.Contains(strings.ToLower(ctype), "text/html") {
        return "", fmt.Errorf("unsupported content type: %s", ctype)
    }

	resultCh := make(chan struct {
        content string
        err     error
    }, 1)

	go func() {
        var builder strings.Builder
        scanner := bufio.NewScanner(resp.Body)
        scanner.Buffer(make([]byte, 64*1024), 10*1024*1024)

        for scanner.Scan() {
            builder.WriteString(scanner.Text())
            
            select {
            case <-ctx.Done():
                resultCh <- struct {
                    content string
                    err     error
                }{
                    content: builder.String(),
                    err:     ctx.Err(),
                }
                return
            default:
            }
        }

        resultCh <- struct {
            content string
            err     error
        }{
            content: builder.String(),
            err:     scanner.Err(),
        }
    }()

	r := <- resultCh
    return r.content, r.err
}

func (ws *webScraper) processSitemap(baseURL, sitemapURL string) ([]string, error) {
    urls, err := getSitemapURLs(sitemapURL, ws.client, ws.cfg.MaxLinksInPage)
	if err != nil {
		return nil, err
	}

    var nextUrls []string
	for _, url := range urls {
		abs, err := makeAbsoluteURL(url, baseURL)
		if abs == "" || err != nil {
			continue
		}
		nextUrls = append(nextUrls, abs)
	}

    return nextUrls, nil
}