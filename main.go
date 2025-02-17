package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/xml"
	"fmt"
	"io"
	"log"
	"math"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode"

	"SE/stemmer"

	"github.com/google/uuid"
	"golang.org/x/net/html"
)

type Document struct {
	Id      	uuid.UUID
	URL     	string
	Description string
	Score		float32
}

type SearchIndex struct {
	index 		map[string]map[uuid.UUID]int
	docs		map[uuid.UUID]*Document
	mu			*sync.RWMutex
	stemmer 	stemmer.Stemmer
	stopWords 	*stemmer.StopWords
	logger 		*logger
}

type webSpider struct {
	baseURL 		string
	client  		*http.Client
	visited 		map[string]struct{}
	mu				*sync.RWMutex
	wg				*sync.WaitGroup
	maxD			int
	maxLinksInPage 	int
	//workerPool		chan struct{}
}

type logger struct {
	file *os.File
}

func NewLogger(fileName string) (*logger, error) {
	file, err := os.OpenFile(fileName, os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0666)
	if err != nil {
		return nil, err
	}
	return &logger{
		file: file,
	}, nil
}

const sitemap = "sitemap.xml"

func NewSpider(baseURL string, maxDepth int) *webSpider {
	return &webSpider{
		baseURL: baseURL,
		client: &http.Client{
            Timeout: 10 * time.Second,
            Transport: &http.Transport{
                MaxIdleConns:        100,
                MaxIdleConnsPerHost: 10,
                IdleConnTimeout:     90 * time.Second,
                DisableKeepAlives:   false,
                ForceAttemptHTTP2:   true,
            },
        },
		visited: make(map[string]struct{}),
		mu: new(sync.RWMutex),
		wg: new(sync.WaitGroup),
		maxD: maxDepth,
		maxLinksInPage: 10,
		//workerPool: make(chan struct{}, 1000),
	}
}

func NewSearchIndex(Stemmer stemmer.Stemmer, l *logger) *SearchIndex {
	return &SearchIndex{
		index: make(map[string]map[uuid.UUID]int),
		docs: make(map[uuid.UUID]*Document),
		mu: new(sync.RWMutex),
		stopWords: stemmer.NewStopWords(),
		stemmer: Stemmer,
		logger: l,
	}
}

func main() {
	logger, err := NewLogger("crawled.txt")
	if err != nil {
		panic(err)
	}
	defer logger.file.Close()

	urls, err := getLocalConfigUrls(sitemap)
	if err != nil {
		panic(err)
	}
	i := NewSearchIndex(stemmer.NewEnglishStemmer(), logger)
	i.Start(urls, 4)

	var query string
	for {
		fmt.Scan(&query)
		Present(i.Search(query))
	}
}

func Present(docs []*Document) {
	for _, doc := range docs {
		fmt.Printf("URL: %s\nDescription: %s\nScore: %f\n", doc.URL, doc.Description, doc.Score)
	}
}

func (idx *SearchIndex) Start(baseURLs []string, depth int) error {
	var wg sync.WaitGroup
	for _, url := range baseURLs {
		spider := NewSpider(url, depth)
		wg.Add(1)
		//spider.workerPool <- struct{}{}
		go func (u string, w *webSpider)  {
			defer func() {
				//<- w.workerPool
				wg.Done()
			}()
			w.wg.Add(1)
			//w.workerPool <- struct{}{}
			go w.Crawl(u, idx, 0)
			w.wg.Wait()
		}(url, spider)
	}
	wg.Wait()

	return nil
}

func (ws *webSpider) Crawl(currentURL string, idx *SearchIndex, depth int) {
	defer func() {
		//<- ws.workerPool
		ws.wg.Done()
	}()
	if depth >= ws.maxD {
		return
	}

	normalized, err := NormalizeUrl(currentURL)
	if err != nil {
        log.Printf("Error normalizing URL %s: %v\n", currentURL, err)
        return
    }
	
	if ws.isVisited(normalized) {
		return
	}
	ws.markAsVisited(normalized)

	log.Println("Parsing: " + currentURL)
	
	if urls, err := ws.haveSitemap(currentURL); urls != nil && err == nil {
		for _, link := range urls {
			ws.wg.Add(1)
			//ws.workerPool <- struct{}{}
			go ws.Crawl(link, idx, depth + 1)
		}
		return
	}

	if err := idx.logger.write(currentURL); err != nil {
		log.Fatal(err)
	}

	doc, err := ws.getHTML(currentURL)
	if err != nil || doc == "" {
		log.Printf("error parsing page: %s\n", currentURL)
		return
	}

	node, err := html.Parse(strings.NewReader(doc))
	if err != nil {
		return
	}

	var document Document
	document.Id = uuid.New()
	document.URL = currentURL
	document.Description = getPageDescription(node)
	words := idx.tokenizeAndStem(extractText(node))
		
	idx.addDocument(&document, words)

	links := ws.extractLinks(node)
	for _, link := range links {
		ws.wg.Add(1)
		//ws.workerPool <- struct{}{}
		go ws.Crawl(link, idx, depth + 1)
	}
}

func (idx *SearchIndex) Search(query string) []*Document {
	idx.mu.RLock()
	defer idx.mu.RUnlock()

	result := make([]*Document, 0)
	tf := make(map[uuid.UUID]float32)

	words := idx.tokenizeAndStem(query)
	for _, word := range words {
		idf := float32(math.Log(float64(len(idx.docs)) / float64(len(idx.index[word]))))

		for docID, freq := range idx.index[word] {
			tf[docID] += float32(freq) * idf
		}
	}

	for id, tf_idf := range tf {
		doc := idx.docs[id]
		doc.Score = tf_idf
		result = append(result, doc)
	}

	sort.Slice(result, func(i, j int) bool {
		return result[i].Score > result[j].Score
	})

	return result
}

func (l *logger) write(data string) error {
	writer := bufio.NewWriter(l.file)
	_, err := writer.WriteString(data + "\n")
	if err != nil {
		return err
	}
	return writer.Flush()
}

func (idx *SearchIndex) addDocument(doc *Document, words []string) {
    idx.mu.Lock()
    defer idx.mu.Unlock()
	
    idx.docs[doc.Id] = doc

    for _, word := range words {
        if idx.index[word] == nil {
            idx.index[word] = make(map[uuid.UUID]int)
        }
        idx.index[word][doc.Id]++
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
    urls, err := getSitemapURLs(sitemapURL, ws.client, ws.maxLinksInPage)
	if err != nil {
		return nil, err
	}

    var nextUrls []string
	for _, url := range urls {
		abs := makeAbsoluteURL(baseURL, url)
		if abs == "" {
			continue
		}
		nextUrls = append(nextUrls, abs)
	}

    return nextUrls, nil
}

func (ws *webSpider) isVisited(URL string) bool {
	ws.mu.RLock()
	defer ws.mu.RUnlock()
	_, ex := ws.visited[URL]
	return ex
}

func (ws *webSpider) markAsVisited(URL string) {
	ws.mu.Lock()
	defer ws.mu.Unlock()
	ws.visited[URL] = struct{}{}
}

func getLocalConfigUrls(path string) ([]string, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	return decodeSitemap(file, 20)
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

func getPageDescription(doc *html.Node) string {
    if desc := extractMetaDescription(doc); desc != "" {
        return desc
    }
    
    if desc := extractOGDescription(doc); desc != "" {
        return desc
    }
    
    return extractFirstParagraph(doc)
}

func extractMetaDescription(doc *html.Node) string {
	var description string
	var findMeta func(*html.Node)

	findMeta = func(n *html.Node) {
		if n.Type == html.ElementNode && n.Data == "meta" {
			var isDescription, hasContent bool
			var content string

			for _, attr := range n.Attr {
				if attr.Key == "name" && attr.Val == "description" {
					isDescription = true
				}
				if attr.Key == "content" {
					hasContent = true
					content = attr.Val
				}
			}

			if isDescription && hasContent {
				description = content
			}
		}

		for c := n.FirstChild; c != nil; c = c.NextSibling {
			findMeta(c)
		}
	}

	findMeta(doc)
	return description
}

func extractOGDescription(doc *html.Node) string {
    var description string
    var findOG func(*html.Node)
    
    findOG = func(n *html.Node) {
        if n.Type == html.ElementNode && n.Data == "meta" {
            var isOGDescription, hasContent bool
            var content string
            
            for _, attr := range n.Attr {
                if attr.Key == "property" && attr.Val == "og:description" {
                    isOGDescription = true
                }
                if attr.Key == "content" {
                    hasContent = true
                    content = attr.Val
                }
            }
            
            if isOGDescription && hasContent {
                description = content
            }
        }
        
        for c := n.FirstChild; c != nil; c = c.NextSibling {
            findOG(c)
        }
    }
    
    findOG(doc)
    return description
}

func extractFirstParagraph(doc *html.Node) string {
    var paragraph string
    var findParagraph func(*html.Node)
    
    findParagraph = func(n *html.Node) {
        if n.Type == html.ElementNode && n.Data == "p" {
            var text string
            for c := n.FirstChild; c != nil; c = c.NextSibling {
                if c.Type == html.TextNode {
                    text += c.Data
                }
            }
            
            if paragraph == "" && strings.TrimSpace(text) != "" {
                paragraph = text
            }
        }
        
        if paragraph == "" {
            for c := n.FirstChild; c != nil; c = c.NextSibling {
                findParagraph(c)
            }
        }
    }
    
    findParagraph(doc)
    return strings.TrimSpace(paragraph)
}

func (ws *webSpider) extractLinks(doc *html.Node) []string {
	links := make([]string, 0)
	var findLinks func(n *html.Node)

	findLinks = func(n *html.Node) {
		if n.Type == html.ElementNode && n.Data == "a" {
			for _, attr := range n.Attr {
				if attr.Key == "href" {
					url := makeAbsoluteURL(ws.baseURL, attr.Val)
					if url == "" {
						break
					}
					links = append(links, url)
					if len(links) >= ws.maxLinksInPage {
						return
					}
					break
				}
			}
		}

		for c := n.FirstChild; c != nil; c = c.NextSibling {
			findLinks(c)
		}
	}

	findLinks(doc)
	return links
}

func extractText(n *html.Node) string {
    var buf bytes.Buffer
    
    var extract func(*html.Node)
    extract = func(n *html.Node) {
        if n.Type == html.ElementNode {
            if n.Data == "script" || n.Data == "style" {
                return
            }
        }
        
        if n.Type == html.TextNode {
            buf.WriteString(n.Data + " ")
        }
        
        for c := n.FirstChild; c != nil; c = c.NextSibling {
            extract(c)
        }
    }
    
    extract(n)
    return buf.String()
}

func (idx *SearchIndex) tokenizeAndStem(text string) []string {
    text = strings.ToLower(text)
    
    var tokens []string
    var currentToken strings.Builder
    
    for _, r := range text {
        if unicode.IsLetter(r) || unicode.IsNumber(r) {
            currentToken.WriteRune(r)
        } else if currentToken.Len() > 0 {
            token := currentToken.String()
            if !idx.stopWords.IsStopWord(token) {
                stemmed := idx.stemmer.Stem(token)
                tokens = append(tokens, stemmed)
            }
            currentToken.Reset()
        }
    }
    
    if currentToken.Len() > 0 {
        token := currentToken.String()
        if !idx.stopWords.IsStopWord(token) {
            stemmed := idx.stemmer.Stem(token)
            tokens = append(tokens, stemmed)
        }
    }
    
    return tokens
}

func makeAbsoluteURL(baseURL, href string) string {
    if strings.HasPrefix(href, "http://") || strings.HasPrefix(href, "https://") {
        return href
    }
    
    if strings.HasPrefix(href, "/") {
        return baseURL + href
    }
    
    return ""
}

func NormalizeUrl(rawUrl string) (string, error) {
    // Pre-compile URL parsing regex
    var urlRegex = regexp.MustCompile(`^https?://`)
    
    // Remove protocol if present
    uri := urlRegex.ReplaceAllString(rawUrl, "")
    
    // Parse remaining URL
    parsedUrl, err := url.Parse(uri)
    if err != nil {
        return "", err
    }
    
    // Use strings.Builder for efficient string manipulation
    var normalized strings.Builder
    normalized.Grow(len(parsedUrl.Host) + len(parsedUrl.Path))
    normalized.WriteString(strings.ToLower(parsedUrl.Host))
    normalized.WriteString(strings.ToLower(parsedUrl.Path))
    
    result := normalized.String()
    return strings.TrimSuffix(result, "/"), nil
}

func (ws *webSpider) getHTML(URL string) (string, error) {
    ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
    defer cancel()

    req, err := http.NewRequestWithContext(ctx, "GET", URL, nil)
    if err != nil {
        return "", err
    }

    req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36")
    req.Header.Set("Accept", "text/html")
    
    resp, err := ws.client.Do(req)
    if err != nil {
        return "", err
    }
    defer resp.Body.Close()

    var builder strings.Builder
    scanner := bufio.NewScanner(resp.Body)
    scanner.Buffer(make([]byte, 64*1024), 10*1024*1024) // Set buffer size

    for scanner.Scan() {
        builder.WriteString(scanner.Text())
    }

    if err := scanner.Err(); err != nil {
        return "", err
    }

    return builder.String(), nil
}