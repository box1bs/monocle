package searchIndex

import (
	handle "Spider/pkg/handleTools"
	"Spider/pkg/logger"
	"Spider/pkg/stemmer"
	"Spider/pkg/webSpider"
	"Spider/pkg/workerPool"
	"context"
	"math"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode"

	"github.com/google/uuid"
)

type SearchIndex struct {
	index     	map[string]map[uuid.UUID]int
	docs      	map[uuid.UUID]*handle.Document
	mu        	*sync.RWMutex
	stemmer   	stemmer.Stemmer
	stopWords 	*stemmer.StopWords
	logger    	*logger.AsyncLogger
	UrlsCrawled int32
	quitChan  	chan struct{}
}

func NewSearchIndex(Stemmer stemmer.Stemmer, l *logger.AsyncLogger, quitChan chan struct{}) *SearchIndex {
	return &SearchIndex{
		index: make(map[string]map[uuid.UUID]int),
		docs: make(map[uuid.UUID]*handle.Document),
		mu: new(sync.RWMutex),
		stopWords: stemmer.NewStopWords(),
		stemmer: Stemmer,
		logger: l,
		quitChan: quitChan,
	}
}

func (idx *SearchIndex) Index(config *handle.ConfigData) error {
	wp := workerPool.NewWorkerPool(config.WorkersCount, config.TasksCount)
    mp := new(sync.Map)
	var rl *webSpider.RateLimiter
	if config.Rate > 0 {
		rl = webSpider.NewRateLimiter(config.Rate)
		defer rl.Shutdown()
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
    for _, url := range config.BaseURLs {
        spider := webSpider.NewSpider(url, config.MaxDepth, config.MaxLinksInPage, mp, wp, config.OnlySameDomain, rl)
        spider.Pool.Submit(func() {
            spider.CrawlWithContext(ctx, url, idx, 0)
        })
    }
	go func() {
		for {
			select {
			case <-done:
				return
			case <-idx.quitChan:
				cancel()
				return
			default:
				time.Sleep(time.Millisecond * 500)
			}
		}
	}()
	wp.Wait()
	done <- struct{}{}
	wp.Stop()
    return nil
}

func (idx *SearchIndex) Search(query string) []*handle.Document {
	idx.mu.RLock()
	defer idx.mu.RUnlock()

	result := make([]*handle.Document, 0)
	tf := make(map[uuid.UUID]float32)

	words := idx.TokenizeAndStem(query)
	for _, word := range words {
		idf := float32(math.Log(float64(len(idx.docs)) / float64(len(idx.index[word]))))

		for docID, freq := range idx.index[word] {
			tf[docID] += float32(freq) * idf
		}
	}

	for id, tf_idf := range tf {
		doc := idx.docs[id]
		doc.Score = tf_idf / float32(doc.LineCount)
		result = append(result, doc)
	}

	sort.Slice(result, func(i, j int) bool {
		return result[i].Score > result[j].Score
	})

	return result
}

func (idx *SearchIndex) Write(data string) {
	idx.logger.Write(data)
}

func (idx *SearchIndex) IncUrlsCounter() {
	atomic.AddInt32(&idx.UrlsCrawled, 1)
}

func (idx *SearchIndex) AddDocument(doc *handle.Document, words []string) {
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

func (idx *SearchIndex) TokenizeAndStem(text string) []string {
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