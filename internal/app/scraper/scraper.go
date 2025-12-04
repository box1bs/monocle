package scraper

import (
	"bytes"
	"crypto/sha256"
	"encoding/gob"
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
	SaveUrlsToBank([32]byte, []byte) error
	GetUrlsByHash([32]byte) ([]byte, error)
}

type workerPool interface {
	Submit(func())
	Wait()
	Stop()
}

type WebScraper struct {
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
	OnlySameDomain  bool
}

const (
	userAgent = "Mozilla/5.0 (Windows NT 10.0; Win64; x64; rv:140.0) Gecko/20100101 Firefox/140.0"
 	crawlTime = 600 * time.Second
 	deadlineTime = 30 * time.Second
	numOfTries = 2 // если кто то решил поменять это на 0, чтож, удачи
)

func NewScraper(mp *sync.Map, cfg *ConfigData, l *logger.Logger, wp workerPool, idx indexer, c context.Context, putDocReq func(string, context.Context) <-chan [][]float64) *WebScraper {
	return &WebScraper{
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

func (ws *WebScraper) Run() {
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

func (ws *WebScraper) ScrapeWithContext(ctx context.Context, currentURL *url.URL, rules *parser.RobotsTxt, depth int) {
    if ws.checkContext(ctx, currentURL.String()) {return}

    if depth >= ws.cfg.Depth {
        return
    }
	
    normalized, err := normalizeUrl(currentURL.String())
    if err != nil {
		return
    }
	
	links, err := ws.fetchPageRulesAndOffers(ctx, currentURL, rules)
	if err.Error() == BaseXMLPageError || ws.checkContext(ctx, currentURL.String()) {
		return
	}
	hashed := sha256.Sum256([]byte(normalized))
	load := false
    
	if len(links) == 0 && (err == nil || err.Error() != BaseXMLPageError) {
		if prevDepth, loaded := ws.visited.LoadOrStore(normalized, depth); loaded && prevDepth.(int) <= depth {
			return
		} else if loaded {
			load = true
			if v := ws.lru.Get(hashed); v != nil {
				links = v.([]*linkToken)
			} else {
				encoded, err := ws.idx.GetUrlsByHash(hashed)
				if err != nil {
					if err.Error() != "Key not found" {
						ws.log.Write(logger.NewMessage(logger.SCRAPER_LAYER, logger.CRITICAL_ERROR, "error getting urls, from db: %v", err))
					}
					return
				}
				if err := gob.NewDecoder(bytes.NewBuffer(encoded)).Decode(&links); err != nil {
					ws.log.Write(logger.NewMessage(logger.SCRAPER_LAYER, logger.ERROR, "error unmarshalling urls from db: %v", err))
					return
				}
				if len(links) != 0 {
					ws.lru.Put(hashed, links)
				}
			}
		} else {
			links, err = ws.fetchHTMLcontent(currentURL, ctx, normalized, rules, depth)
			if err != nil {
				return
			}
		}
		
		if len(links) == 0 {
			ws.log.Write(logger.NewMessage(logger.SCRAPER_LAYER, logger.ERROR, "empty links in page %s\n", currentURL))
			return
		}

		if !load {
			var buf bytes.Buffer
			if err := gob.NewEncoder(&buf).Encode(links); err != nil {
				ws.log.Write(logger.NewMessage(logger.SCRAPER_LAYER, logger.CRITICAL_ERROR, "error marshalling urls: %v", err))
				return
			}

			if err := ws.idx.SaveUrlsToBank(hashed, buf.Bytes()); err != nil {
				ws.log.Write(logger.NewMessage(logger.SCRAPER_LAYER, logger.ERROR, "error saving urls: %v", err))
				return
			}
		}
	}
	
	for _, link := range links {	
		if ws.cfg.OnlySameDomain && !link.SameDomain {
			continue
		}
		
		rls := rules
        if !link.SameDomain {
			rls = nil
        }

        ws.pool.Submit(func() {
			c, cancel := context.WithTimeout(ws.globalCtx, crawlTime)
			defer cancel()
			ws.rlMu.Lock()
			if ws.rlMap[link.Link.Host] == nil {
				ws.rlMap[link.Link.Host] = NewRateLimiter(DefaultDelay)
			}
			ws.rlMu.Unlock()
			ws.ScrapeWithContext(c, link.Link, rls, depth+1)
		})
    }
}

func (ws *WebScraper) putDownLimiters() {
	ws.rlMu.Lock()
	defer ws.rlMu.Unlock()
	for _, limiter := range ws.rlMap {
		limiter.Shutdown()
	}
}

func (ws *WebScraper) checkContext(ctx context.Context, currentURL string) bool {
	select {
		case <-ctx.Done():
			ws.log.Write(logger.NewMessage(logger.SCRAPER_LAYER, logger.ERROR, "context canceled while parsing page: %s\n", currentURL))
			return true
		default:
	}
	return false
}