package web

import (
	"encoding/xml"
	"errors"
	"io"
	"regexp"
	"unicode"

	"github.com/box1bs/Saturday/internal/app/index/tree"
	"github.com/box1bs/Saturday/internal/model"
	"github.com/box1bs/Saturday/pkg/parser"
	"github.com/box1bs/Saturday/pkg/workerPool"
	"golang.org/x/net/html"

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
var urlRegex = regexp.MustCompile(`^https?://`)

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
        rateLimiter:    rateLimiter,
	}
}

func (ws *webSpider) CrawlWithContext(ctx context.Context, canc context.CancelFunc, currentURL string, idx model.Indexer, parent *tree.TreeNode, depth int) {
	defer canc()
    select {
	case <-ctx.Done():
		//log.Println("task time exided")
		return
	default:
    }

    if depth >= ws.maxD {
        return
    }
    
    normalized, err := normalizeUrl(currentURL)
    if err != nil {
    	log.Printf("Error normalizing URL %s: %v\n", currentURL, err)
        return
    }
    
    if _, loaded := ws.visited.LoadOrStore(normalized, struct{}{}); loaded {
        return
    }
    
    //log.Println("Parsing: " + currentURL)

    if rules, err := parser.FetchRobotsTxt(ctx, currentURL, ws.client); rules != "" && err == nil {
        robotsTXT := parser.ParseRobotsTxt(rules)
        parent.SetRules(robotsTXT)
    } else if parent.GetRules() == nil {
        uri, err := url.Parse(currentURL)
		if err != nil {
			return
		}
		if rules, err = parser.FetchRobotsTxt(ctx, uri.Scheme + "://" + uri.Host + "/", ws.client); rules != "" && err == nil {
			robotsTXT := parser.ParseRobotsTxt(rules)
			parent.SetRules(robotsTXT)
		}
    }
    
    if urls, err := ws.haveSitemap(currentURL); urls != nil && err == nil {
        for _, link := range urls {
			select {
			case <-ctx.Done():
				//log.Println("task time exceeded")
				return
			default:
			}
            if normalized, err := normalizeUrl(link); err == nil && !ws.isVisited(normalized) {
                child := tree.NewNode(link)
                parent.AddChild(child)
				same, err := isSameOrigin(ws.baseURL, link)
				if err != nil {
					continue
				}
                if ws.onlySameDomain || same {
                    child.SetRules(parent.GetRules())
                }
				c, cancel := context.WithTimeout(idx.GetContext(), 90 * time.Second)
                ws.Pool.Submit(func() {
                    ws.CrawlWithContext(c, cancel, link, idx, child, depth+1)
                })
            }
        }
    }

    ws.rateLimiter.maybeGetToken()
    
    doc, err := ws.getHTML(ctx, currentURL)
    if err != nil || doc == "" {
		//log.Printf("error parsing page: %s\n", currentURL)
        return
    }
    
    idx.IncUrlsCounter()
	idx.Write(currentURL)

    document := &model.Document{
        Id: uuid.New(),
        URL: currentURL,
    }

	c, cancel := context.WithTimeout(ctx, time.Second * 15)
	defer cancel()
	var links []string
	var content string
    document.Description, links, content = parseHTMLStream(c, doc, currentURL, userAgent, ws.maxLinksInPage, ws.onlySameDomain, parent.GetRules())

	document.Sequence, err =  idx.HandleDocumentWords(content)
	if err != nil {
		return
	}
    idx.AddDocument(document)

    for _, link := range links {
		select {
		case <-ctx.Done():
			//log.Println("task time exided")
			return
		default:
		}
        if normalized, err := normalizeUrl(link); err == nil && !ws.isVisited(normalized) {
            child := tree.NewNode(link)
            parent.AddChild(child)
			same, err := isSameOrigin(ws.baseURL, link)
			if err != nil {
				continue
			}
            if ws.onlySameDomain || same {
                child.SetRules(parent.GetRules())
            }
            c, cancel := context.WithTimeout(idx.GetContext(), 90 * time.Second)
            ws.Pool.Submit(func() {
                ws.CrawlWithContext(c, cancel, link, idx, child, depth+1)
            })
        }
    }
}

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

func (ws *webSpider) haveSitemap(url string) ([]string, error) {
	sitemapURL := strings.TrimSuffix(url, "/")
	sitemapURL = sitemapURL + "/" + sitemap

	urls, err := ws.processSitemap(url, sitemapURL)
    if err != nil {
		return nil, err
    }

	return urls, err
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

    parsedUrl = cleanUTMParams(parsedUrl)
    parsedUrl.Host = strings.TrimPrefix(parsedUrl.Host, "www.")
    
    var normalized strings.Builder
    normalized.WriteString(strings.ToLower(parsedUrl.Host))
    normalized.WriteString(strings.ToLower(parsedUrl.Path))
    
    result := normalized.String()
    return strings.TrimSuffix(result, "/"), nil
}

func cleanUTMParams(rawURL *url.URL) *url.URL {
	query := rawURL.Query()
	for key := range query {
		if strings.HasPrefix(key, "utm_") {
			query.Del(key)
		}
	}
	rawURL.RawQuery = query.Encode()
	return rawURL
}

func parseHTMLStream(ctx context.Context, htmlContent, baseURL, userAgent string, maxLinks int, onlySameOrigin bool, rules *parser.RobotsTxt) (description string, links []string, fullText string) {
	tokenizer := html.NewTokenizer(strings.NewReader(htmlContent))
	var metaDesc, ogDesc, firstParagraph string
	var inParagraph, inScriptOrStyle bool
	var fullTextBuilder strings.Builder
	links = make([]string, 0, maxLinks)

	tokenCount := 0
    const checkContextEvery = 20

	for {
		tokenCount++
		if tokenCount % checkContextEvery == 0 {
			select {
			case <-ctx.Done():
				fullText = strings.TrimSpace(fullTextBuilder.String())
				
				if metaDesc != "" {
					description = metaDesc
				} else if ogDesc != "" {
					description = ogDesc
				} else {
					description = strings.TrimSpace(firstParagraph)
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
			log.Println("error parsing HTML with url: " + baseURL)
			break
		}

		switch tokenType {
		case html.StartTagToken:
			t := tokenizer.Token()
			tagName := strings.ToLower(t.Data)
			switch tagName {
			case "meta":
				var isDesc, isOG bool
				var content string
				for _, attr := range t.Attr {
					key := strings.ToLower(attr.Key)
					val := attr.Val
					if key == "name" && strings.ToLower(val) == "description" {
						isDesc = true
					}
					if key == "property" && strings.ToLower(val) == "og:description" {
						isOG = true
					}
					if key == "content" {
						content = attr.Val
					}
				}
				if isDesc && content != "" && metaDesc == "" {
					metaDesc = content
				}
				if isOG && content != "" && ogDesc == "" {
					ogDesc = content
				}
			case "p", "div", "br", "h1", "h2", "h3", "h4", "h5", "h6", "li":
                if firstParagraph == "" && tagName == "p" {
                    inParagraph = true
                }
			case "a":
				for _, attr := range t.Attr {
					if strings.ToLower(attr.Key) == "href" {
						link, err := makeAbsoluteURL(attr.Val, baseURL)
						if err != nil {
							break
						}
						if link != "" && len(links) < maxLinks {
							if rules != nil {
								uri, err := url.Parse(link)
								if err != nil {
									break
								}
								if !rules.IsAllowed(userAgent, uri.Path) {
									break
								}
							}
							if onlySameOrigin {
								same, err := isSameOrigin(link, baseURL)
								if err != nil {
									break
								}
								if same {
									links = append(links, link)
								}
								break
							}
							links = append(links, link)
						}
						break
					}
				}
			case "script", "style":
				inScriptOrStyle = true
			}
		case html.EndTagToken:
			t := tokenizer.Token()
			tagName := strings.ToLower(t.Data)
			if tagName == "p" && inParagraph {
				inParagraph = false
			} else if tagName == "script" || tagName == "style" {
				inScriptOrStyle = false
			}
		case html.TextToken:
			if inScriptOrStyle {
				continue
			}
			text := strings.TrimSpace(string(tokenizer.Text()))
			if text != "" {
				fullTextBuilder.WriteString(text + " ")
				if inParagraph && firstParagraph == "" {
					firstParagraph += text + " "
				}
			}
		}
	}

	fullText = strings.TrimSpace(fullTextBuilder.String())
    
	if metaDesc != "" {
		description = metaDesc
	} else if ogDesc != "" {
		description = ogDesc
	} else {
		description = strings.TrimSpace(firstParagraph)
	}
	return
}

func (ws *webSpider) processSitemap(baseURL, sitemapURL string) ([]string, error) {
    urls, err := getSitemapURLs(sitemapURL, ws.client, ws.maxLinksInPage)
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

func isSameOrigin(rawURL, baseURL string) (bool, error) {
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

func (ws *webSpider) isVisited(URL string) bool {
    _, exists := ws.visited.Load(URL)
    return exists
}

func (ws *webSpider) getHTML(ctx context.Context, URL string) (string, error) {
    ctx, cancel := context.WithTimeout(ctx, 25 * time.Second)
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