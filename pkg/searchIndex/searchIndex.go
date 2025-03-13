package searchIndex

import (
	"context"
	"log"
	"math"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode"

	handle "github.com/box1bs/Saturday/pkg/handleTools"
	"github.com/box1bs/Saturday/pkg/logger"
	"github.com/box1bs/Saturday/pkg/stemmer"
	tree "github.com/box1bs/Saturday/pkg/treeIndex"
	"github.com/box1bs/Saturday/pkg/webSpider"
	"github.com/box1bs/Saturday/pkg/workerPool"

	"github.com/google/uuid"
)

type SearchIndex struct {
	indexRepos  Repository
	mu        	*sync.RWMutex
	stemmer   	stemmer.Stemmer
	stopWords 	*stemmer.StopWords
	logger    	*logger.AsyncLogger
	root 		*tree.TreeNode
	UrlsCrawled int32
	AvgLen	 	float64
	quitChan  	chan struct{}
}

type Repository interface {
	LoadVisitedUrls(*sync.Map) error
	SaveVisitedURLs(*sync.Map) error
	IndexDocument(string, []string) error
	GetDocumentsByWord(string) (map[uuid.UUID]int, error)
	SaveDocument(*handle.Document) error
	GetDocumentByID(uuid.UUID) (*handle.Document, error)
	GetAllDocuments() ([]*handle.Document, error)
	GetDocumentsCount() (int, error)
}

func NewSearchIndex(Stemmer stemmer.Stemmer, l *logger.AsyncLogger, ir Repository, quitChan chan struct{}) *SearchIndex {
	return &SearchIndex{
		mu: new(sync.RWMutex),
		stopWords: stemmer.NewStopWords(),
		stemmer: Stemmer,
		root: tree.NewNode("/"),
		logger: l,
		quitChan: quitChan,
		indexRepos: ir,
	}
}

func (idx *SearchIndex) Index(config *handle.ConfigData) error {
	wp := workerPool.NewWorkerPool(config.WorkersCount, config.TasksCount)
    mp := new(sync.Map)
	idx.indexRepos.LoadVisitedUrls(mp)
	defer idx.indexRepos.SaveVisitedURLs(mp)
	var rl *webSpider.RateLimiter
	if config.Rate > 0 {
		rl = webSpider.NewRateLimiter(config.Rate)
		defer rl.Shutdown()
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
    for _, url := range config.BaseURLs {
		node := tree.NewNode(url)
		idx.root.AddChild(node)
        spider := webSpider.NewSpider(url, config.MaxDepth, config.MaxLinksInPage, mp, wp, config.OnlySameDomain, rl)
        spider.Pool.Submit(func() {
            spider.CrawlWithContext(ctx, url, idx, node, 0)
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

type requestRanking struct {
	includesWords 	int
	relation 		float64
	tf_idf 			float64
	bm25 			float64
	//any ranking scores
}

func (idx *SearchIndex) updateAVGLen() {
	idx.mu.RLock()
	defer idx.mu.RUnlock()

	var wordCount int
	docs, err := idx.indexRepos.GetAllDocuments()
	if err != nil {
		log.Println(err)
		return
	}
	for _, doc := range docs {
		wordCount += int(doc.GetFullSize())
	}

	idx.AvgLen = float64(wordCount) / float64(len(docs))
}

func (idx *SearchIndex) Search(query string) []*handle.Document {
	idx.mu.RLock()
	defer idx.mu.RUnlock()
	
	if idx.AvgLen == 0 {
		idx.updateAVGLen()
	}
	
	rank := make(map[uuid.UUID]*requestRanking)

	words := idx.TokenizeAndStem(query)
	index := make(map[string]map[uuid.UUID]int)
	for _, word := range words {
		mp, err := idx.indexRepos.GetDocumentsByWord(word)
		if err != nil || len(mp) == 0 {
			log.Println(err)
			return nil
		}
		index[word] = mp
		for docID := range mp {
			if _, ok := rank[docID]; !ok {
				rank[docID] = &requestRanking{}
			}
		}
	}

	idx.fetchDocuments(words, rank, index)
	if len(rank) == 0 {
		return nil
	}
	
	result := make([]*handle.Document, 0)
	
	alreadyIncluded := make(map[uuid.UUID]struct{})
	for _, word := range words {
		lenght, err := idx.indexRepos.GetDocumentsCount()
		if err != nil {
			log.Println(err)
			return nil
		}
		idf := math.Log(float64(lenght) / float64(len(index[word]))) + 1.0
		
		for docID, freq := range index[word] {
			doc, err := idx.indexRepos.GetDocumentByID(docID)
			if err != nil {
				log.Println(err)
				continue
			}
			if doc == nil {
				continue
			}
			
			rank[docID].tf_idf += float64(freq) / doc.GetFullSize() * (idf - 1) / float64(len(doc.Words))
			rank[docID].bm25 += culcBM25(idf, float64(freq), doc, idx.AvgLen)

			if _, ok := alreadyIncluded[docID]; ok {
				continue
			}
			alreadyIncluded[docID] = struct{}{}
			result = append(result, doc)
		}
	}

	if len(result) == 0 {
		return nil
	}

	predicted, err := handleBinaryScore(words, result)
	if err != nil {
		log.Println(err)
		return nil
	}

	for _, rel := range predicted {
		rank[rel.Doc.Id].relation = rel.Score
	}

	sort.Slice(result, func(i, j int) bool {
		return rank[result[i].Id].relation > rank[result[j].Id].relation || rank[result[i].Id].includesWords > rank[result[j].Id].includesWords || 
		(rank[result[i].Id].relation == rank[result[j].Id].relation && rank[result[i].Id].includesWords == rank[result[j].Id].includesWords) && 
		(rank[result[i].Id].bm25 > rank[result[j].Id].bm25 || rank[result[i].Id].tf_idf > rank[result[j].Id].tf_idf)
	})

	return result
}

func (idx *SearchIndex) fetchDocuments(words []string, rank map[uuid.UUID]*requestRanking, index map[string]map[uuid.UUID]int) {
	result := index[words[0]]
	if result == nil {
		return
	}

	for _, word := range words[1:] {
		docs, ex := index[word]
		if !ex {
			continue
		}
		result = intersect(result, docs, rank)
		if len(result) == 0 {
			return
		}
	}
}

func intersect(a, b map[uuid.UUID]int, rank map[uuid.UUID]*requestRanking) map[uuid.UUID]int {
    result := make(map[uuid.UUID]int)
    for key := range a {
        if v, exists := b[key]; exists {
			rank[key].includesWords++
            result[key] = a[key] + v
        }
    }
    return result
}

func culcBM25(idf float64, tf float64, doc *handle.Document, avgLen float64) float64 {
	k1 := 1.2
	b := 0.75
	return idf * (tf * (k1 + 1)) / (tf + k1 * (1 - b + b * doc.GetFullSize() / avgLen))
}

func (idx *SearchIndex) Write(data string) {
	idx.logger.Write(data)
}

func (idx *SearchIndex) IncUrlsCounter() {
	atomic.AddInt32(&idx.UrlsCrawled, 1)
}

func (idx *SearchIndex) AddDocument(doc *handle.Document) {
    idx.mu.Lock()
    defer idx.mu.Unlock()
	
	idx.indexRepos.SaveDocument(doc)
	idx.indexRepos.IndexDocument(doc.Id.String(), doc.Words)
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

func (idx *SearchIndex) HandleDocumentWords(doc *handle.Document, text string) {
	idx.mu.Lock()
	defer idx.mu.Unlock()
	
	doc.Words = append(doc.Words, idx.TokenizeAndStem(text)...)
	doc.ArchiveDocument()
}