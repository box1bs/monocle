package searcher

import (
	"context"
	"log"
	"math"
	"sort"
	"sync"
	"time"

	"github.com/box1bs/Saturday/internal/model"
	"github.com/google/uuid"
)

type index interface {
	GetDocumentsByWord(int) (map[uuid.UUID]int, map[uuid.UUID]struct{}, error)
	GetDocumentsCount() (int, error)
	GetDocumentByID(uuid.UUID) (*model.Document, error)
	GetAVGLen() (float64, error)
	HandleTextQuery(string) ([]int, error)
}

type Searcher struct {
	mu         	*sync.RWMutex
	vectorizer  *vectorizer
	idx 		index
}

func NewSearcher(idx index) *Searcher {
	return &Searcher{
		mu:        	&sync.RWMutex{},
		vectorizer: newVectorizer(),
		idx:       	idx,
	}
}

type requestRanking struct {
	tf_idf 			float64
	bm25 			float64
	wordsCos		float64
	headerCos		float64
	includesWords 	int
	//any ranking scores
}

func (s *Searcher) Search(query string, quorum float64, maxLen int) []*model.Document {
	s.mu.RLock()
	defer s.mu.RUnlock()
	
	rank := make(map[uuid.UUID]requestRanking)

	terms, err := s.idx.HandleTextQuery(query)
	if err != nil {
		log.Println(err)
		return nil
	}
	index := make(map[int]map[uuid.UUID]int)
	preCaclFuture := make(map[int]map[uuid.UUID]struct{})
	for i := range terms {
		mp, hasInHeader, err := s.idx.GetDocumentsByWord(terms[i])
		if err != nil {
			log.Println(err)
			return nil
		}
		index[terms[i]] = mp
		preCaclFuture[terms[i]] = hasInHeader
	}

	result := make([]*model.Document, 0)
	alreadyIncluded := make(map[string]struct{})
	var wg sync.WaitGroup
	var rankMu sync.Mutex
	var resultMu sync.Mutex
	errCh := make(chan error, len(terms))

	avgLen, err := s.idx.GetAVGLen()
	if err != nil {
		log.Println(err)
		return nil
	}
	
	for _, term := range terms {
		wg.Add(1)
		go func(term int) {
			defer wg.Done()
	
			length, err := s.idx.GetDocumentsCount()
			if err != nil {
				errCh <- err
				return
			}
			idf := math.Log(float64(length) / float64(len(index[term]) + 1)) + 1.0
	
			for docID, freq := range index[term] {
				doc, err := s.idx.GetDocumentByID(docID)
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
				r.bm25 += culcBM25(idf, float64(freq), doc, avgLen)
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
	vec, err := s.vectorizer.Vectorize(query, c)
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
			var sumCosW float64
			for _, v := range doc.WordVec{
				sumCosW += calcCosineSimilarity(v, vec[0])
			}
			var sumCosH float64
			for _, v := range doc.TitleVec{
				sumCosH += calcCosineSimilarity(v, vec[0])
			}
			r.wordsCos = sumCosW / float64(len(doc.WordVec))
			r.headerCos = sumCosH / float64(len(doc.TitleVec))
			rank[doc.Id] = r
			filteredResult = append(filteredResult, doc)
		}
	}

	length := len(filteredResult)
	if length == 0 {
		return nil
	}

	sort.Slice(filteredResult, func(i, j int) bool {
		if rank[filteredResult[i].Id].headerCos != rank[filteredResult[j].Id].headerCos {
			return rank[filteredResult[i].Id].headerCos > rank[filteredResult[j].Id].headerCos
		}
		if rank[filteredResult[i].Id].wordsCos != rank[filteredResult[j].Id].wordsCos {
			return rank[filteredResult[i].Id].wordsCos > rank[filteredResult[j].Id].wordsCos
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