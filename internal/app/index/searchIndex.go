package index

import (
	"context"
	"log"
	"math"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	"github.com/box1bs/Saturday/configs"
	"github.com/box1bs/Saturday/internal/app/index/spell_checker"
	"github.com/box1bs/Saturday/internal/app/index/tree"
	"github.com/box1bs/Saturday/internal/app/scraper"
	"github.com/box1bs/Saturday/internal/model"
	"github.com/box1bs/Saturday/pkg/workerPool"
	"github.com/google/uuid"
)

type repository interface {
	LoadVisitedUrls(*sync.Map) error
	SaveVisitedUrls(*sync.Map) error
	IndexDocument(uuid.UUID, []int) error
	GetDocumentsByWord(int) (map[uuid.UUID]int, error)

	SaveDocument(doc *model.Document) error
	GetDocumentByID(uuid.UUID) (*model.Document, error)
	GetAllDocuments() ([]*model.Document, error)
	GetDocumentsCount() (int, error)

	GetDict() ([]string, error)
	TransferOrSaveToSequence([]string, bool) ([]int, error)
}

type stemmer interface {
	TokenizeAndStem(string) []string
}

type logger interface {
	Write(string)
	Close()
}

type SearchIndex struct {
	mu        	*sync.RWMutex
	root 		*tree.TreeNode
	visitedUrls *sync.Map
	vectorizer  *vectorizer
	indexRepos  repository
	stemmer   	stemmer
	logger    	logger
	AvgLen	 	float64
	UrlsCrawled int32
	isUpToDate 	bool
}

func NewSearchIndex(ir repository, stemmer stemmer, l logger) *SearchIndex {
	return &SearchIndex{
		mu: new(sync.RWMutex),
		root: tree.NewNode("/"),
		visitedUrls: new(sync.Map),
		vectorizer: newVectorizer(),
		indexRepos: ir,
		stemmer: stemmer,
		logger: l,
	}
}

func (idx *SearchIndex) Index(config *configs.ConfigData, global context.Context) error {
	wp := workerPool.NewWorkerPool(config.WorkersCount, config.TasksCount)
	idx.indexRepos.LoadVisitedUrls(idx.visitedUrls)
	defer idx.indexRepos.SaveVisitedUrls(idx.visitedUrls)

	var rl *web.RateLimiter
	if config.Rate > 0 {
		rl = web.NewRateLimiter(config.Rate)
		defer rl.Shutdown()
	}
	spider := web.NewScraper(idx.visitedUrls, rl, global, idx, wp, idx.logger.Write, idx.vectorizer.vectorize, web.ScrapeConfig{
		Depth:          config.MaxDepth,
		MaxLinksInPage: config.MaxLinksInPage,
		OnlySameDomain: config.OnlySameDomain,
	})

    for _, url := range config.BaseURLs {
		node := tree.NewNode(url)
		idx.root.AddChild(node)
        spider.Pool.Submit(func() {
			ctx, cancel := context.WithTimeout(global, 20 * time.Second)
			defer cancel()
            spider.ScrapeWithContext(ctx, url, node, 0)
        })
    }

	wp.Wait()
	wp.Stop()
	if idx.isUpToDate && idx.UrlsCrawled != 0 {
		idx.isUpToDate = false
	}
    return nil
}

type requestRanking struct {
	tf_idf 			float64
	bm25 			float64
	cos 			float64
	includesWords 	int
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

func (idx *SearchIndex) Search(query string, quorum float64, maxLen int) []*model.Document {
	idx.mu.RLock()
	defer idx.mu.RUnlock()
	
	if !idx.isUpToDate {
		idx.updateAVGLen()
		idx.isUpToDate = true
	}
	
	rank := make(map[uuid.UUID]requestRanking)

	queryTerms := idx.stemmer.TokenizeAndStem(query)
	terms, err := idx.indexRepos.TransferOrSaveToSequence(queryTerms, false)
	if err != nil || len(terms) == 0 {
		return nil
	}

	index := make(map[int]map[uuid.UUID]int)
	dict, err := idx.indexRepos.GetDict()
	if err != nil {
		log.Println(err)
		return nil
	}
	for i := range terms {
		if terms[i] == 0 {
			sc := spellChecker.NewSpellChecker(2)
			el := sc.BestReplacement(queryTerms[i], dict)
			num, err := idx.indexRepos.TransferOrSaveToSequence([]string{el}, false)
			if err != nil || len(num) == 0 {
				continue
			}
			terms[i] = num[0]
		}
		mp, err := idx.indexRepos.GetDocumentsByWord(terms[i])
		if err != nil {
			log.Println(err)
			return nil
		}
		index[terms[i]] = mp
	}

	result := make([]*model.Document, 0)
	alreadyIncluded := make(map[string]struct{})
	var wg sync.WaitGroup
	var rankMu sync.Mutex
	var resultMu sync.Mutex
	errCh := make(chan error, len(terms))
	
	for _, term := range terms {
		wg.Add(1)
		go func(term int) {
			defer wg.Done()
	
			length, err := idx.indexRepos.GetDocumentsCount()
			if err != nil {
				errCh <- err
				return
			}
			idf := math.Log(float64(length) / float64(len(index[term]) + 1)) + 1.0
	
			for docID, freq := range index[term] {
				doc, err := idx.indexRepos.GetDocumentByID(docID)
				if err != nil || doc == nil {
					continue
				}
				
				rankMu.Lock()
				r, ex := rank[docID]
				if !ex {
					rank[docID] = requestRanking{}
				}
				r.includesWords++
				r.tf_idf += float64(freq) / doc.GetFullSize() * idf
				r.bm25 += culcBM25(idf, float64(freq), doc, idx.AvgLen)
				rank[docID] = r
				rankMu.Unlock()
	
				resultMu.Lock()
				if _, exists := alreadyIncluded[doc.Id.String()]; exists {
					resultMu.Unlock()
					continue
				}
				alreadyIncluded[doc.Id.String()] = struct{}{}
				result = append(result, doc)
				resultMu.Unlock()
			}
		}(term)
	}
	
	go func() {
		wg.Wait()
		close(errCh)
	}()
	
	c, cancel := context.WithTimeout(context.Background(), 5 * time.Second)
	defer cancel()
	vec, err := idx.vectorizer.vectorize(query, c)
	if err != nil {
		log.Println(err)
		return nil
	}

	if err := <-errCh; err != nil {
		return nil
	}

	filteredResult := make([]*model.Document, 0)
	for _, doc := range result {
		r := rank[doc.Id]

		if r.tf_idf >= quorum {
			var sumCos float64
			for _, v := range doc.Vec{
				sumCos += calcCosineSimilarity(v, vec[0])
			}
			r.cos = sumCos / float64(len(doc.Vec))
			rank[doc.Id] = r
			filteredResult = append(filteredResult, doc)
		}
	}

	length := len(filteredResult)
	if length == 0 {
		return nil
	}

	sort.Slice(filteredResult, func(i, j int) bool {
		if rank[filteredResult[i].Id].cos != rank[filteredResult[j].Id].cos {
			return rank[filteredResult[i].Id].cos > rank[filteredResult[j].Id].cos
		}
		if rank[filteredResult[i].Id].includesWords != rank[filteredResult[j].Id].includesWords {
			return rank[filteredResult[i].Id].includesWords > rank[filteredResult[j].Id].includesWords
		}
		if rank[filteredResult[i].Id].bm25 != rank[filteredResult[j].Id].bm25 {
			return rank[filteredResult[i].Id].bm25 > rank[filteredResult[j].Id].bm25
		}
		return rank[filteredResult[i].Id].tf_idf > rank[filteredResult[j].Id].tf_idf
	})

	return filteredResult[:min(length, maxLen)]
}

func (idx *SearchIndex) IncUrlsCounter() {
	atomic.AddInt32(&idx.UrlsCrawled, 1)
}

func (idx *SearchIndex) AddDocument(doc *model.Document, words []int) {
	idx.mu.Lock()
	defer idx.mu.Unlock()
	
	idx.indexRepos.IndexDocument(doc.Id, words)

	doc.WordCount = len(words)
	doc.PartOfFullSize = 512.0 / float64(doc.WordCount)

	idx.indexRepos.SaveDocument(doc)
}

func (idx *SearchIndex) GetCurrentUrlsCrawled() int32 {
	return atomic.LoadInt32(&idx.UrlsCrawled)
}

func (idx *SearchIndex) HandleDocumentWords(text string) ([]int, error) {
	idx.mu.Lock()
	defer idx.mu.Unlock()
	
	return idx.indexRepos.TransferOrSaveToSequence(idx.stemmer.TokenizeAndStem(text), true)
}

func calcCosineSimilarity(vec1, vec2 []float64) float64 {
	if len(vec1) != len(vec2) {
		return 0.0
	}

	dotProduct := 0.0
	magnitude1 := 0.0
	magnitude2 := 0.0

	for i := range vec1 {
		dotProduct += vec1[i] * vec2[i]
		magnitude1 += vec1[i] * vec1[i]
		magnitude2 += vec2[i] * vec2[i]
	}

	if magnitude1 == 0 || magnitude2 == 0 {
		return 0.0
	}

	return dotProduct / (math.Sqrt(magnitude1) * math.Sqrt(magnitude2))
}

func culcBM25(idf float64, tf float64, doc *model.Document, avgLen float64) float64 {
	k1 := 1.2
	b := 0.75
	return idf * (tf * (k1 + 1)) / (tf + k1 * (1 - b + b * doc.GetFullSize() / avgLen))
}