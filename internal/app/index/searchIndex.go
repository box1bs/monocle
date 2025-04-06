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
	UrlsCrawled int32
	AvgLen	 	float64
	quitCTX		context.Context
	vectorizer  *Vectorizer
}

func NewSearchIndex(stemmer model.Stemmer, stopWords model.StopWords, l model.Logger, ir model.Repository, context context.Context, v *Vectorizer) *SearchIndex {
	return &SearchIndex{
		mu: new(sync.RWMutex),
		stopWords: stopWords,
		stemmer: stemmer,
		root: tree.NewNode("/"),
		logger: l,
		quitCTX: context,
		indexRepos: ir,
		vectorizer: v,
	}
}

func (idx *SearchIndex) Index(config *configs.ConfigData) error {
	wp := workerPool.NewWorkerPool(config.WorkersCount, config.TasksCount)
    mp := new(sync.Map)
	idx.indexRepos.LoadVisitedUrls(mp)
	defer idx.indexRepos.SaveVisitedUrls(mp)
	var rl *web.RateLimiter
	ctx, cancel := context.WithTimeout(context.Background(), 120 * time.Second)
	defer cancel()
    for _, url := range config.BaseURLs {
		if config.Rate > 0 {
			rl = web.NewRateLimiter(config.Rate)
			defer rl.Shutdown()
		}
		node := tree.NewNode(url)
		idx.root.AddChild(node)
        spider := web.NewSpider(url, config.MaxDepth, config.MaxLinksInPage, mp, wp, config.OnlySameDomain, rl)
        spider.Pool.Submit(func() {
            spider.CrawlWithContext(ctx, cancel, url, idx, idx.vectorizer, node, 0)
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
	alreadyIncluded := make(map[uuid.UUID]struct{})
	for _, term := range terms {
		lenght, err := idx.indexRepos.GetDocumentsCount()
		if err != nil {
			log.Println(err)
			return nil
		}
		idf := math.Log(float64(lenght) / float64(len(index[term]) + 1)) + 1.0
		
		for docID, freq := range index[term] {
			doc, err := idx.indexRepos.GetDocumentByID(docID)
			if err != nil {
				log.Println(err)
				continue
			}
			if doc == nil {
				continue
			}

			r, ex := rank[docID]
			if !ex {
				rank[docID] = requestRanking{}
			}
			r.includesWords++
			r.tf_idf += float64(freq) / doc.GetFullSize() * idf
			r.bm25 += culcBM25(idf, float64(freq), doc, idx.AvgLen)
			rank[docID] = r

			if _, ex := alreadyIncluded[docID]; ex {
				continue
			}
			alreadyIncluded[docID] = struct{}{}
			result = append(result, doc)
		}
	}

	if len(result) == 0 {
		return nil
	}

	c, cancel := context.WithTimeout(idx.quitCTX, 5 * time.Second)
	defer cancel()
	vec, err := idx.vectorizer.Vectorize(query, c)
	if err != nil {
		log.Println(err)
		return nil
	}

	filteredResult := make([]*model.Document, 0)
	for _, doc := range result {
		r := rank[doc.Id]

		if r.tf_idf >= quorum {
			r.cos = calcCosineSimilarity(doc.Vec, vec)
			rank[doc.Id] = r
			filteredResult = append(filteredResult, doc)
		}
	}

	length := len(filteredResult)
	if length == 0 {
		return nil
	}

	sort.Slice(result, func(i, j int) bool {
		return rank[result[i].Id].cos > rank[result[j].Id].cos ||
		 rank[result[i].Id].includesWords > rank[result[j].Id].includesWords ||
		  rank[result[i].Id].includesWords == rank[result[j].Id].includesWords && 
		(rank[result[i].Id].bm25 > rank[result[j].Id].bm25 || rank[result[i].Id].tf_idf > rank[result[j].Id].tf_idf)
	})

	return result[:min(length, maxLen)]
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
	doc.PartOfFullSize = 256.0 / float64(doc.WordCount)

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

func (idx *SearchIndex) HandleDocumentWords(text string) ([]int, error) {
	idx.mu.Lock()
	defer idx.mu.Unlock()
	
	return idx.indexRepos.TransferOrSaveToSequence(idx.TokenizeAndStem(text), true)
}

func (idx *SearchIndex) GetContext() context.Context {
	return idx.quitCTX
}