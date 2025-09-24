package scraper

import (
	"crypto/sha256"
	"io"
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
    HandleDocumentWords(*model.Document, []model.Passage, [][32]byte) error
	IsCrawledContent([32]byte, []model.Passage) (bool, error)
	GetSimilarHashes([][32]byte) ([][][32]byte, error)
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
    rateLimiter    	*rateLimiter
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
	OnlySameDomain  bool
	DocNGramCount 	int
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
        rateLimiter:    newRateLimiter(cfg.Rate),
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
	defer ws.rateLimiter.shutdown()
	for _, url := range ws.cfg.StartURLs {
		ws.pool.Submit(func() {
			ctx, cancel := context.WithTimeout(ws.globalCtx, 30 * time.Second)
			defer cancel()
			ws.ScrapeWithContext(ctx, url, nil, 0)
		})
	}
	ws.pool.Wait()
	ws.pool.Stop()
}

func (ws *webScraper) ScrapeWithContext(ctx context.Context, currentURL string, rules *parser.RobotsTxt, depth int) {
    select {
	case <-ctx.Done():
		return
	default:
    }

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

	urls := []string{}

	errCh := make(chan error)
	go func() {
		defer close(errCh)
		if r, err := parser.FetchRobotsTxt(ctx, currentURL, ws.client); r != "" && err == nil {
			robotsTXT := parser.ParseRobotsTxt(r)
			*rules = *robotsTXT
		} else if rules == nil {
			uri, err := url.Parse(currentURL)
			if err != nil {
				errCh <- err
				return
			}
			if r, err = parser.FetchRobotsTxt(ctx, uri.Scheme + "://" + uri.Host + "/", ws.client); r != "" && err == nil {
				robotsTXT := parser.ParseRobotsTxt(r)
				*rules = *robotsTXT
			}
		}
		
		if urls, err = ws.haveSitemap(currentURL); err != nil {
			errCh <- err
		}
	}()

    ws.rateLimiter.getToken()
    
	c, cancel := context.WithTimeout(ctx, time.Second * 5)
	defer cancel()
    doc, err := ws.getHTML(c, currentURL)
    if err != nil || doc == "" {
		ws.write(fmt.Sprintf("error parsing page: %s\n", currentURL))
        return
    }
    
	ws.write(currentURL)

    document := &model.Document{
        Id: sha256.Sum256([]byte(normalized)),
        URL: currentURL,
    }

	if err = <- errCh; err != nil {
		ws.write(fmt.Sprintf("error fetching robots.txt or sitemap.xml for page: %s with error %v\n", currentURL, err))
	}

	c, cancel = context.WithTimeout(ctx, time.Second * 5)
	defer cancel()
    links, passages := ws.parseHTMLStream(c, doc, currentURL, rules)

	if crawled, err := ws.idx.IsCrawledContent(document.Id, passages); err != nil || crawled {
		return
	}

	fullText := strings.Builder{}
	for _, passage := range passages {
		fullText.WriteString(passage.Text)
	}

	linkFilled := make(chan struct{})
	go func() {
		defer close(linkFilled)
		for _, link := range urls {
			s, err := isSameOrigin(link, currentURL)
			if err != nil {
				continue
			}
			links = append(links, &linkToken{link: link, sameDomain: s})
		}
	}()

	c, cancel = context.WithTimeout(ctx, time.Second * 5)
	defer cancel()
	
	document.WordVec, err = ws.vectorize(fullText.String(), c)
	if err != nil {
		ws.write(fmt.Sprintf("error vectorizing page: %s with error %v\n", currentURL, err))
		return
	}

	hash := ws.breakToShingles(ws.cfg.DocNGramCount, fullText.String())

	similarHashes, err := ws.idx.GetSimilarHashes(hash)
	if err != nil {
		return
	}

	cash := map[[32]byte]struct{}{}
	for _, bytes := range hash {
		cash[bytes] = struct{}{}
	}

	for _, docHash := range similarHashes {
		if ws.checkSimilarity(cash, docHash) > 0.7 {
			return
		}
	}

	ws.mu.Lock()
	ws.pageRank[normalized]++
	ws.mu.Unlock()

    if err := ws.idx.HandleDocumentWords(document, passages, hash); err != nil {
		ws.write(fmt.Sprintf("error handling words for page: %s with error %v\n", currentURL, err))
		return
	}

	<- linkFilled
	if len(links) == 0 {
		return
	}

    for _, link := range links {
		select {
		case <-ctx.Done():
			return
		default:
		}

		if ws.cfg.OnlySameDomain && !link.sameDomain {
			continue
		}
        if !(ws.cfg.OnlySameDomain || link.sameDomain) {
            rules = nil
        }

        ws.pool.Submit(func() {
			c, cancel := context.WithTimeout(ws.globalCtx, 20 * time.Second)
			defer cancel()
        	ws.ScrapeWithContext(c, link.link, rules, depth+1)
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
								break
							}
							if ws.isVisited(normalized) {
								break
							}
							if rules != nil {
								uri, err := url.Parse(link)
								if err != nil {
									break
								}
								if !rules.IsAllowed(userAgent, uri.Path) {
									break
								}
							}
							same, err := isSameOrigin(link, baseURL)
							if err != nil {
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

func (ws *webScraper) isVisited(URL string) bool {
    _, exists := ws.visited.Load(URL)
    return exists
}

func (ws *webScraper) getHTML(ctx context.Context, URL string) (string, error) {
    ctx, cancel := context.WithTimeout(ctx, 5 * time.Second)
    defer cancel()

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

    ctype := resp.Header.Get("Content-Type")
    if !strings.HasPrefix(strings.ToLower(ctype), "text/html") {
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
                    content: "",
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

    select {
    case result := <-resultCh:
        return result.content, result.err
    case <-ctx.Done():
        return "", ctx.Err()
    }
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

func (ws *webScraper) breakToShingles(count int, str string) [][32]byte {
	input := [][32]byte{}
	for i := range len(str) - count {
		input = append(input, sha256.Sum256([]byte(str[i:i + count])))
	}

	return input
}

func (ws *webScraper) checkSimilarity(original map[[32]byte]struct{}, hash [][32]byte) float64 {
	cnt := 0.0
	for _, bytes := range hash {
		if _, ex := original[bytes]; ex {
			cnt++
		}
	}
	
	orLen := float64(len(original))
	lenH := float64(len(hash))

	return (cnt / orLen + cnt / lenH) / 2.0
}