package index

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

	"github.com/box1bs/Saturday/configs"
	"github.com/box1bs/Saturday/internal/app/index/tree"
	"github.com/box1bs/Saturday/internal/app/web"
	"github.com/box1bs/Saturday/internal/model"
	"github.com/box1bs/Saturday/pkg/workerPool"
	"github.com/google/uuid"
)

type SearchIndex struct {
	indexRepos  model.Repository
	mu        	*sync.RWMutex
	stemmer   	model.Stemmer
	stopWords 	model.StopWords
	logger    	model.Logger
	root 		*tree.TreeNode
	visitedUrls *sync.Map
	UrlsCrawled int32
	AvgLen	 	float64
	vectorizer  *Vectorizer
}

func NewSearchIndex(stemmer model.Stemmer, stopWords model.StopWords, l model.Logger, ir model.Repository, v *Vectorizer) *SearchIndex {
	return &SearchIndex{
		mu: new(sync.RWMutex),
		stopWords: stopWords,
		stemmer: stemmer,
		root: tree.NewNode("/"),
		visitedUrls: new(sync.Map),
		logger: l,
		indexRepos: ir,
		vectorizer: v,
	}
}

func (idx *SearchIndex) Index(config *configs.ConfigData, c context.Context) error {
	wp := workerPool.NewWorkerPool(config.WorkersCount, config.TasksCount)
	idx.indexRepos.LoadVisitedUrls(idx.visitedUrls)
	defer idx.indexRepos.SaveVisitedUrls(idx.visitedUrls)
	var rl *web.RateLimiter
	ctx, cancel := context.WithTimeout(c, 120 * time.Second)
	defer cancel()
    for _, url := range config.BaseURLs {
		if config.Rate > 0 {
			rl = web.NewRateLimiter(config.Rate)
			defer rl.Shutdown()
		}
		node := tree.NewNode(url)
		idx.root.AddChild(node)
        spider := web.NewSpider(url, config.MaxDepth, config.MaxLinksInPage, idx.visitedUrls, wp, config.OnlySameDomain, rl)
        spider.Pool.Submit(func() {
            spider.CrawlWithContext(ctx, c, cancel, url, idx, idx.vectorizer, node, 0)
        })
    }
	wp.Wait()
	wp.Stop()
    return nil
}

type requestRanking struct {
	includesWords 	int
	tf_idf 			float64
	bm25 			float64
	cos 			float64
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
	
	if idx.AvgLen == 0 {
		idx.updateAVGLen()
	}
	
	rank := make(map[uuid.UUID]requestRanking)

	queryTerms := idx.TokenizeAndStem(query)
	terms, err := idx.indexRepos.TransferOrSaveToSequence(queryTerms, false)
	if err != nil || len(terms) == 0 {
		return nil
	}

	index := make(map[int]map[uuid.UUID]int)
	for _, term := range terms {
		mp, err := idx.indexRepos.GetDocumentsByWord(term)
		if err != nil {
			log.Println(err)
			return nil
		}
		index[term] = mp
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
				if _, exists := alreadyIncluded[doc.URL]; exists {
					resultMu.Unlock()
					continue
				}
				alreadyIncluded[doc.URL] = struct{}{}
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
	vec, err := idx.vectorizer.Vectorize(query, c)
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

func (idx *SearchIndex) Write(data string) {
	idx.logger.Write(data)
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

func (idx *SearchIndex) GetCurrentUrlsCrawled() int32 {
	return atomic.LoadInt32(&idx.UrlsCrawled)
}

func (idx *SearchIndex) HandleDocumentWords(text string) ([]int, error) {
	idx.mu.Lock()
	defer idx.mu.Unlock()
	
	return idx.indexRepos.TransferOrSaveToSequence(idx.TokenizeAndStem(text), true)
}