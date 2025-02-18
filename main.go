package main

import (
	"bufio"
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
	"sync/atomic"
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
	logger 		*AsyncLogger
}

type webSpider struct {
	baseURL 		string
	client  		*http.Client
	visited 		*sync.Map
	mu				*sync.RWMutex
	maxD			int
	maxLinksInPage 	int
	pool 			*WorkerPool
}

type AsyncLogger struct {
	file *os.File
	ch   chan string
	wg   sync.WaitGroup
}

func NewAsyncLogger(fileName string) (*AsyncLogger, error) {
	file, err := os.OpenFile(fileName, os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0666)
	if err != nil {
		return nil, err
	}
	al := &AsyncLogger{
		file: file,
		ch:   make(chan string, 1000),
	}
	al.wg.Add(1)
	go func() {
		defer al.wg.Done()
		writer := bufio.NewWriter(al.file)
		for msg := range al.ch {
			_, err := writer.WriteString(msg + "\n")
			if err != nil {
				// Можно добавить обработку ошибки записи
			}
			writer.Flush()
		}
	}()
	return al, nil
}

func (l *AsyncLogger) Write(data string) {
	select {
	case l.ch <- data:
	default:
		l.ch <- data
	}
}

func (l *AsyncLogger) Close() error {
	close(l.ch)
	l.wg.Wait()
	return l.file.Close()
}

const sitemap = "sitemap.xml"
var urlRegex = regexp.MustCompile(`^https?://`)

func NewSpider(baseURL string, maxDepth, poolSize int) *webSpider {
	return &webSpider{
		baseURL: baseURL,
		client: &http.Client{
			Timeout: 10 * time.Second,
			Transport: &http.Transport{
				IdleConnTimeout:   90 * time.Second,
				DisableKeepAlives: false,
				ForceAttemptHTTP2: true,
			},
		},
		visited:        new(sync.Map),
		mu:             new(sync.RWMutex),
		maxD:           maxDepth,
		maxLinksInPage: 10,
		pool: NewWorkerPool(poolSize, 60000),
	}
}


func NewSearchIndex(Stemmer stemmer.Stemmer, l *AsyncLogger) *SearchIndex {
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
	logger, err := NewAsyncLogger("crawled.txt")
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
		spider := NewSpider(url, depth, 150)
		wg.Add(1)

		go func (u string, w *webSpider)  {
			defer func() {
                w.pool.Stop()
                wg.Done()
            }()

			w.pool.Submit(func() {
				w.Crawl(u, idx, 0)
			})

			w.pool.Wait()
		}(url, spider)
	}
	wg.Wait()

	return nil
}

func (ws *webSpider) Crawl(currentURL string, idx *SearchIndex, depth int) {
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
            if normalized, err := NormalizeUrl(link); err == nil && !ws.isVisited(normalized) {
                ws.pool.Submit(func() {
                    ws.Crawl(link, idx, depth+1)
                })
            }
        }
        return
    }
    
    idx.logger.Write(currentURL)
    
    doc, err := ws.getHTML(currentURL)
    if err != nil || doc == "" {
        log.Printf("error parsing page: %s\n", currentURL)
        return
    }
    
    description, content := parseHTMLStream(doc)

    document := &Document{
        Id: uuid.New(),
        URL: currentURL,
        Description: description,
    }
    words := idx.tokenizeAndStem(content)
    idx.addDocument(document, words)
        
    links := extractLinksStream(doc, currentURL, ws.maxLinksInPage)
    for _, link := range links {
        if normalized, err := NormalizeUrl(link); err == nil && !ws.isVisited(normalized) {
            ws.pool.Submit(func() {
                ws.Crawl(link, idx, depth+1)
            })
        }
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

func parseHTMLStream(htmlContent string) (description string, fullText string) {
	tokenizer := html.NewTokenizer(strings.NewReader(htmlContent))
	var metaDesc, ogDesc, firstParagraph string
	var inParagraph, inScriptOrStyle bool
	var fullTextBuilder strings.Builder

	for {
		tokenType := tokenizer.Next()
		if tokenType == html.ErrorToken {
			if tokenizer.Err() == io.EOF {
				break
			}
			// При возникновении ошибки можно выйти или обработать её отдельно
			break
		}

		switch tokenType {
		case html.StartTagToken:
			t := tokenizer.Token()
			tagName := strings.ToLower(t.Data)
			if tagName == "meta" {
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
			} else if tagName == "p" {
				// Начинаем собирать первый абзац, если он ещё не найден
				if firstParagraph == "" {
					inParagraph = true
				}
			} else if tagName == "script" || tagName == "style" {
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
	// Приоритет описания: meta > og > первый абзац
	if metaDesc != "" {
		description = metaDesc
	} else if ogDesc != "" {
		description = ogDesc
	} else {
		description = strings.TrimSpace(firstParagraph)
	}
	return description, fullText
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
    _, exists := ws.visited.Load(URL)
    return exists
}

func (ws *webSpider) markAsVisited(URL string) {
    ws.visited.Store(URL, struct{}{})
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

func extractLinksStream(htmlContent, baseURL string, maxLinks int) []string {
	tokenizer := html.NewTokenizer(strings.NewReader(htmlContent))
	links := make([]string, 0, maxLinks)
	for {
		tokenType := tokenizer.Next()
		if tokenType == html.ErrorToken {
			if tokenizer.Err() == io.EOF {
				break
			}
			break
		}
		if tokenType == html.StartTagToken {
			t := tokenizer.Token()
			if strings.ToLower(t.Data) == "a" {
				for _, attr := range t.Attr {
					if strings.ToLower(attr.Key) == "href" {
						link := makeAbsoluteURL(baseURL, attr.Val)
						if link != "" {
							links = append(links, link)
							if len(links) >= maxLinks {
								return links
							}
						}
						break
					}
				}
			}
		}
	}
	return links
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
    uri := urlRegex.ReplaceAllString(rawUrl, "")
    
    // Parse remaining URL
    parsedUrl, err := url.Parse(uri)
    if err != nil {
        return "", err
    }
    
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

type WorkerPool struct {
	tasks     chan func()      // Канал, из которого воркеры берут задачи
	taskQueue chan func()      // Очередь для поступающих задач
	wg        *sync.WaitGroup
	quit      chan struct{}
	workers   int32
}

func NewWorkerPool(size int, queueCapacity int) *WorkerPool {
	wp := &WorkerPool{
		tasks:     make(chan func(), size*2),
		taskQueue: make(chan func(), queueCapacity), // Например, 60000
		wg:        &sync.WaitGroup{},
		quit:      make(chan struct{}),
	}
	// Диспетчер: непрерывно передаёт задачи из очереди в канал tasks
	go func() {
		for {
			select {
			case task := <-wp.taskQueue:
				wp.tasks <- task
			case <-wp.quit:
				return
			}
		}
	}()
	// Запускаем воркеров
	for i := 0; i < size; i++ {
		go wp.worker()
	}
	return wp
}

func (wp *WorkerPool) Submit(task func()) {
	// Оборачиваем задачу, чтобы гарантировать вызов wg.Done() после выполнения
	wp.wg.Add(1)
	wp.taskQueue <- func() {
		defer wp.wg.Done()
		task()
	}
}

func (wp *WorkerPool) worker() {
	atomic.AddInt32(&wp.workers, 1)
	defer atomic.AddInt32(&wp.workers, -1)
	for {
		select {
		case task, ok := <-wp.tasks:
			if !ok {
				return
			}
			task()
		case <-wp.quit:
			return
		}
	}
}

func (wp *WorkerPool) Wait() {
	wp.wg.Wait()
}

func (wp *WorkerPool) Stop() {
	close(wp.quit)
	wp.Wait()
}