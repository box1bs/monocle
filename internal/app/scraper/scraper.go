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
	MaxVisitedDeep 	int
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
			ws.ScrapeWithContext(ctx, parsed, nil, 0, 0)
		})
	}
	ws.pool.Wait()
	ws.log.Write(logger.NewMessage(logger.SCRAPER_LAYER, logger.DEBUG, "waiting for stoppnig worker pool"))
	ws.pool.Stop()
}

type cacheData struct {
	html 		string
	scrapedD 	int
}

func (ws *webScraper) ScrapeWithContext(ctx context.Context, currentURL *url.URL, rules *parser.RobotsTxt, depth, visDeep int) {
    if ws.checkContext(ctx, currentURL.String()) {return}

    if depth >= ws.cfg.Depth {
        return
    }

	ws.fetchPageRulesAndOffers(ctx, currentURL, rules, depth, visDeep)

    normalized, err := normalizeUrl(currentURL.String())
    if err != nil {
        return
    }
    
	links := []*linkToken{}
    if _, loaded := ws.visited.LoadOrStore(normalized, struct{}{}); loaded && visDeep >= ws.cfg.MaxVisitedDeep {
		return
    } else if !loaded {
		links, err = ws.fetchHTMLcontent(currentURL, ctx, normalized, rules, depth, visDeep)
		if err != nil {
			return
		}
	} else if loaded {
		if v := ws.lru.Get(sha256.Sum256([]byte(normalized))); v != nil {
			cached := v.(cacheData)
			c, cancel := context.WithTimeout(ctx, deadlineTime)
			defer cancel()
			links, _ = ws.parseHTMLStream(c, cached.html, currentURL, rules, depth, visDeep)
		} else {
			return
		}
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
			visD := 0
			if link.visited {
				visD = visDeep + 1
			}
			ws.ScrapeWithContext(c, link.link, rls, depth+1, visD)
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