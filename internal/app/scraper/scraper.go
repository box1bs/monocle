package scraper

import (
	"crypto/sha256"
	"regexp"

	"github.com/box1bs/monocle/internal/app/scraper/lruCache"
	"github.com/box1bs/monocle/internal/model"
	"github.com/box1bs/monocle/pkg/logger"
	"github.com/box1bs/monocle/pkg/parser"

	"context"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

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
	log 			*logger.Logger
	rlMu         	*sync.RWMutex
	lru 			*lrucache.LRUCache
	pool           	workerPool
	idx 			indexer
	globalCtx		context.Context
	rlMap			map[string]*rateLimiter
	putDocReq		func(string, context.Context) <-chan [][]float64
}

type ConfigData struct {
	StartURLs     	[]string
	CacheCap 		int
	Depth       	int
	MaxLinksInPage 	int
	OnlySameDomain  bool
}

const (
	userAgent = "Mozilla/5.0 (Windows NT 10.0; Win64; x64)"
	sitemap = "sitemap.xml"
 	crawlTime = 600 * time.Second
 	deadlineTime = 30 * time.Second
	numOfTries = 4 // если кто то решил поменять это на 0, чтож, удачи
)

func NewScraper(mp *sync.Map, cfg *ConfigData, l *logger.Logger, wp workerPool, idx indexer, c context.Context, putDocReq func(string, context.Context) <-chan [][]float64) *webScraper {
	return &webScraper{
		client: &http.Client{
			Timeout: deadlineTime,
			Transport: &http.Transport{
				IdleConnTimeout:   15 * time.Second,
				DisableKeepAlives: false,
				ForceAttemptHTTP2: true,
			},
		},
		visited:        mp,
		mu: 			new(sync.Mutex),
		cfg: 			cfg,
		log:			l,
		rlMu:           new(sync.RWMutex),
		lru: 			lrucache.NewLRUCache(cfg.CacheCap),
		pool:           wp,
		idx: 			idx,
		globalCtx:		c,
		rlMap: 			make(map[string]*rateLimiter),
		putDocReq:		putDocReq,
	}
}

func (ws *webScraper) Run() {
	defer ws.putDownLimiters()
	for _, uri := range ws.cfg.StartURLs {
		parsed, err := url.Parse(uri)
		if err != nil {
			ws.log.Write(logger.NewMessage(logger.SCRAPER_LAYER, logger.ERROR, "error parsing link: %v", err))
			continue
		}
		ws.pool.Submit(func() {
			ctx, cancel := context.WithTimeout(ws.globalCtx, crawlTime)
			defer cancel()
			ws.rlMu.Lock()
			rl := NewRateLimiter(DefaultDelay)
			ws.rlMap[parsed.Host] = rl
			ws.rlMu.Unlock()
			ws.ScrapeWithContext(ctx, parsed, nil, 0)
		})
	}
	ws.pool.Wait()
	ws.log.Write(logger.NewMessage(logger.SCRAPER_LAYER, logger.DEBUG, "waiting for stoppnig worker pool"))
	ws.pool.Stop()
}

func (ws *webScraper) ScrapeWithContext(ctx context.Context, currentURL *url.URL, rules *parser.RobotsTxt, depth int) {
    if ws.checkContext(ctx, currentURL.String()) {return}

    if depth >= ws.cfg.Depth {
        return
    }

	if strings.HasSuffix(currentURL.String(), ".xml") && strings.Contains(currentURL.String(), "sitemap") {
		ws.scrapeThroughtSitemap(ctx, currentURL, rules, depth)
		return
	}

    normalized, err := normalizeUrl(currentURL.String())
    if err != nil {
        return
    }
    
    if _, loaded := ws.visited.LoadOrStore(normalized, struct{}{}); loaded {
        return
    }
	
	if r, err := parser.FetchRobotsTxt(ctx, currentURL.String(), ws.client); r != "" && err == nil {
		robotsTXT := parser.ParseRobotsTxt(r)
		rules = robotsTXT
		ws.rlMu.Lock()
		if ex := ws.rlMap[currentURL.Host]; (ex == nil || ex.R == DefaultDelay) && rules.Rules["*"].Delay > 0 {
			ws.rlMap[currentURL.Host] = NewRateLimiter(rules.Rules["*"].Delay)
		} else if ex == nil {
			ws.rlMap[currentURL.Host] = NewRateLimiter(DefaultDelay)
		}
		ws.rlMu.Unlock()
	}
		
	if urls, err := ws.haveSitemap(currentURL); err == nil && len(urls) > 0 {
		ws.scrapeThroughtSitemap(ctx, currentURL, rules, depth)
	}

	ws.rlMu.RLock()
	rl := ws.rlMap[currentURL.Host]
	ws.rlMu.RUnlock()
    doc, err := ws.getHTML(currentURL.String(), rl, numOfTries)
    if err != nil || doc == "" {
		ws.log.Write(logger.NewMessage(logger.SCRAPER_LAYER, logger.ERROR, "error parsing page: %s, with error: %v\n", currentURL, err))
        return
    }

	id := sha256.Sum256([]byte(normalized))
    document := &model.Document{
        Id: id,
        URL: currentURL.String(),
    }

	c, cancel := context.WithTimeout(ctx, deadlineTime)
	defer cancel()
    links, passages := ws.parseHTMLStream(c, doc, currentURL, rules)
	
	if crawled, err := ws.idx.IsCrawledContent(document.Id, passages); err != nil || crawled {
		return
	}

	fullText := strings.Builder{}
	for _, passage := range passages {
		fullText.WriteString(passage.Text)
	}
	
	var ok bool
	select {
	case document.WordVec, ok = <-ws.putDocReq(fullText.String(), ws.globalCtx):
		if !ok {
			ws.log.Write(logger.NewMessage(logger.SCRAPER_LAYER, logger.CRITICAL_ERROR, "error vectorizing document for page: %s\n", currentURL))
			return
		}
	case <-ws.globalCtx.Done():
		ws.log.Write(logger.NewMessage(logger.SCRAPER_LAYER, logger.CRITICAL_ERROR, "timeout vectorizing document for page: %s\n", currentURL))
		return
	}
	
    if err := ws.idx.HandleDocumentWords(document, passages); err != nil {
		return
	}
	
	if len(links) == 0 {
		ws.log.Write(logger.NewMessage(logger.SCRAPER_LAYER, logger.ERROR, "empty links in page %s\n", currentURL))
		return
	}
	
	for _, link := range links {	
		if ws.cfg.OnlySameDomain && !link.sameDomain {
			continue
		}
		
		rls := rules
        if !link.sameDomain {
			rls = nil
        }

        ws.pool.Submit(func() {
			if ws.checkContext(ctx, currentURL.String()) {return}
			c, cancel := context.WithTimeout(ws.globalCtx, crawlTime)
			defer cancel()
			ws.rlMu.Lock()
			if ws.rlMap[link.link.Host] == nil {
				ws.rlMap[link.link.Host] = NewRateLimiter(DefaultDelay)
			}
			ws.rlMu.Unlock()
			ws.ScrapeWithContext(c, link.link, rls, depth+1)
		})
    }
}

func (ws *webScraper) putDownLimiters() {
	ws.rlMu.Lock()
	defer ws.rlMu.Unlock()
	for _, limiter := range ws.rlMap {
		limiter.Shutdown()
	}
}

func (ws *webScraper) checkContext(ctx context.Context, currentURL string) bool {
	select {
		case <-ctx.Done():
			ws.log.Write(logger.NewMessage(logger.SCRAPER_LAYER, logger.ERROR, "context canceled while parsing page: %s\n", currentURL))
			return true
		default:
	}
	return false
}