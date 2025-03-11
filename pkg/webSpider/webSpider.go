package webSpider

import (
	handle "github.com/box1bs/Saturday/pkg/handleTools"
	parser "github.com/box1bs/Saturday/pkg/robots_parser"
	tree "github.com/box1bs/Saturday/pkg/treeIndex"
	"github.com/box1bs/Saturday/pkg/workerPool"

	"bufio"
	"context"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
)

var userAgent = "Mozilla/5.0 (Windows NT 10.0; Win64; x64)"

// Indexer defines the minimal interface required by the spider.
type Indexer interface {
    Write(string)
    TokenizeAndStem(string) []string
    AddDocument(*handle.Document, []string)
    IncUrlsCounter()
}

type webSpider struct {
	baseURL        string
	client         *http.Client
	visited        *sync.Map
	maxD           int
	maxLinksInPage int
	Pool           *workerPool.WorkerPool
    onlySameDomain bool
    rateLimiter    *RateLimiter
}

const sitemap = "sitemap.xml"

func NewSpider(baseURL string, maxDepth, maxLinksInPage int, mp *sync.Map, wp *workerPool.WorkerPool, onlySameDomain bool, rateLimiter *RateLimiter) *webSpider {
	return &webSpider{
		baseURL: baseURL,
		client: &http.Client{
			Timeout: 10 * time.Second,
			Transport: &http.Transport{
				IdleConnTimeout:   10 * time.Second,
				DisableKeepAlives: false,
				ForceAttemptHTTP2: true,
			},
		},
		visited:        mp,
		maxD:           maxDepth,
		maxLinksInPage: maxLinksInPage,
		Pool:           wp,
        onlySameDomain: onlySameDomain,
        rateLimiter: rateLimiter,
	}
}

func (ws *webSpider) CrawlWithContext(ctx context.Context, currentURL string, idx Indexer, parent *tree.TreeNode, depth int) {
    select {
    case <-ctx.Done():
        return
    default:
    }

    if depth >= ws.maxD {
        return
    }
    
    normalized, err := handle.NormalizeUrl(currentURL)
    if err != nil {
        log.Printf("Error normalizing URL %s: %v\n", currentURL, err)
        return
    }
    
    if _, loaded := ws.visited.LoadOrStore(normalized, struct{}{}); loaded {
        return
    }
    
    log.Println("Parsing: " + currentURL)

    if rules, err := parser.FetchRobotsTxt(currentURL); rules != "" && err == nil {
        robotsTXT := parser.ParseRobotsTxt(rules)
        parent.SetRules(robotsTXT)
    } else if parent.GetRules() == nil {
        uri, _ := url.Parse(currentURL)
        if rules, _ = parser.FetchRobotsTxt(uri.Scheme + "://" + uri.Host + "/"); rules != "" {
            robotsTXT := parser.ParseRobotsTxt(rules)
            parent.SetRules(robotsTXT)
        }
    }
    
    if urls, err := ws.haveSitemap(currentURL); urls != nil && err == nil {
        for _, link := range urls {
            if normalized, err := handle.NormalizeUrl(link); err == nil && !ws.isVisited(normalized) {
                child := tree.NewNode(link)
                parent.AddChild(child)
                if ws.onlySameDomain || handle.SameDomain(ws.baseURL, link) {
                    child.SetRules(parent.GetRules())
                }
                go ws.Pool.Submit(func() {
                    ws.CrawlWithContext(ctx, link, idx, child, depth+1)
                })
            }
        }
    }

    ws.rateLimiter.maybeGetToken()
    
    doc, err := ws.getHTML(currentURL)
    if err != nil || doc == "" {
		log.Printf("error parsing page: %s\n", currentURL)
        return
    }
    
    idx.IncUrlsCounter()
	idx.Write(currentURL)

    description, content, links, lineCount := handle.ParseHTMLStream(doc, currentURL, userAgent, ws.maxLinksInPage, ws.onlySameDomain, parent.GetRules())

    document := &handle.Document{
        Id: uuid.New(),
        URL: currentURL,
        Description: description,
        FullText: content,
        LineCount: lineCount,
    }
    words := idx.TokenizeAndStem(content)
    document.WordsCount = len(words)
    idx.AddDocument(document, words)

    for _, link := range links {
        if normalized, err := handle.NormalizeUrl(link); err == nil && !ws.isVisited(normalized) {
            child := tree.NewNode(link)
            parent.AddChild(child)
            if ws.onlySameDomain || handle.SameDomain(ws.baseURL, link) {
                child.SetRules(parent.GetRules())
            }
            go ws.Pool.Submit(func() {
                ws.CrawlWithContext(ctx, link, idx, child, depth+1)
            })
        }
    }
}

func (ws *webSpider) haveSitemap(url string) ([]string, error) {
	sitemapURL := strings.TrimSuffix(url, "/")
	sitemapURL = sitemapURL + "/" + sitemap

	urls, err := ws.ProcessSitemap(url, sitemapURL)
    if err != nil {
		return nil, err
    }

	return urls, err
}

func (ws *webSpider) ProcessSitemap(baseURL, sitemapURL string) ([]string, error) {
    urls, err := handle.GetSitemapURLs(sitemapURL, ws.client, ws.maxLinksInPage)
	if err != nil {
		return nil, err
	}

    var nextUrls []string
	for _, url := range urls {
		abs := handle.MakeAbsoluteURL(baseURL, url)
		if abs == "" {
			continue
		}
		nextUrls = append(nextUrls, abs)
	}

    return nextUrls, nil
}

func (ws *webSpider) isVisited(URL string) bool {
    _, exists := ws.visited.Load(URL)
    return exists
}

func (ws *webSpider) getHTML(URL string) (string, error) {
    ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
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

    var builder strings.Builder
    scanner := bufio.NewScanner(resp.Body)
    scanner.Buffer(make([]byte, 64*1024), 10*1024*1024)

    for scanner.Scan() {
        builder.WriteString(scanner.Text())
    }
    if err := scanner.Err(); err != nil {
        return "", err
    }

    return builder.String(), nil
}