package searcher

import (
	"context"
	"log"
	"math"
	"sort"
	"sync"
	"time"

	"github.com/box1bs/monocle/internal/model"
)

type index interface {
	GetDocumentsByWord(int) (map[[32]byte]*model.WordCountAndPositions, error)
	GetDocumentsCount() (int, error)
	GetDocumentByID([32]byte) (*model.Document, error)
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
	dpq				float64
	queryCoverage	float64
	queryDencity 	int
	includesWords 	int
	hasWordInHeader bool
	//any ranking scores
}

func (s *Searcher) Search(query string, quorum float64, maxLen int) []*model.Document {
	s.mu.RLock()
	defer s.mu.RUnlock()
	
	rank := make(map[[32]byte]requestRanking)

	terms, err := s.idx.HandleTextQuery(query)
	if err != nil {
		log.Println(err)
		return nil
	}
	index := make(map[int]map[[32]byte]*model.WordCountAndPositions)
	for i := range terms {
		mp, err := s.idx.GetDocumentsByWord(terms[i])
		if err != nil {
			log.Println(err)
			return nil
		}
		index[terms[i]] = mp
	}

	result := make([]*model.Document, 0)
	alreadyIncluded := make(map[[32]byte]struct{})
	var wg sync.WaitGroup
	var rankMu sync.Mutex
	var resultMu sync.Mutex
	errCh := make(chan error, len(terms))

	avgLen, err := s.idx.GetAVGLen()
	if err != nil {
		log.Println(err)
		return nil
	}

	length, err := s.idx.GetDocumentsCount()
	if err != nil {
		log.Println(err)
		return nil
	}

	queryLen := len(terms)

	for _, term := range terms {
		wg.Add(1)
		go func(term int) {
			defer wg.Done()
	
			idf := math.Log(float64(length) / float64(len(index[term]) + 1)) + 1.0
	
			for docID, item := range index[term] {
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
				r.tf_idf += float64(doc.WordCount) * idf
				r.bm25 += culcBM25(idf, float64(item.Count), doc, avgLen)
				positions := [][]model.Position{}
				if r.queryCoverage == 0.0 {
					coverage := 0.0
					for _, term := range terms {
						positions = append(positions, item.Positions)
						if len(index[term][docID].Positions) > 0 {
							coverage++
						}
					}
					r.queryDencity = calcQueryDencity(positions, queryLen)
					r.queryCoverage = coverage / float64(queryLen)
				}
				if !r.hasWordInHeader {
					for i := 0; i < item.Count && !r.hasWordInHeader; i++ {
						r.hasWordInHeader = item.Positions[i].Type == 'h'
					}
				}
				rank[docID] = r
				rankMu.Unlock()
	
				resultMu.Lock()
				if _, exists := alreadyIncluded[doc.Id]; exists {
					resultMu.Unlock()
					continue
				}
				alreadyIncluded[doc.Id] = struct{}{}
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
			sumCosW := 0.0
			for _, v := range doc.WordVec {
				sumCosW += calcCosineSimilarity(v, vec[0])
			}
			length := float64(len(doc.WordVec))
			r.wordsCos = sumCosW / length
			sumDistance := 0.0
			for _, v := range doc.WordVec {
				sumDistance += calcEuclidianDistance(v, vec[0])
			}
			r.dpq = sumDistance / length
			rank[doc.Id] = r
			filteredResult = append(filteredResult, doc)
		}
	}

	length = len(filteredResult)
	if length == 0 {
		return nil
	}

	sort.Slice(filteredResult, func(i, j int) bool {
		if TruncateToTwoDecimalPlaces(rank[filteredResult[i].Id].wordsCos) != TruncateToTwoDecimalPlaces(rank[filteredResult[j].Id].wordsCos) {
			return rank[filteredResult[i].Id].wordsCos > rank[filteredResult[j].Id].wordsCos
		}
		if TruncateToTwoDecimalPlaces(rank[filteredResult[i].Id].dpq) != TruncateToTwoDecimalPlaces(rank[filteredResult[j].Id].dpq) {
			return rank[filteredResult[i].Id].dpq < rank[filteredResult[j].Id].dpq
		}
		if rank[filteredResult[i].Id].bm25 != rank[filteredResult[j].Id].bm25 {
			return rank[filteredResult[i].Id].bm25 > rank[filteredResult[j].Id].bm25
		}
		if rank[filteredResult[i].Id].includesWords != rank[filteredResult[j].Id].includesWords {
			return rank[filteredResult[i].Id].includesWords > rank[filteredResult[j].Id].includesWords
		}
		if rank[filteredResult[i].Id].queryDencity != rank[filteredResult[j].Id].queryDencity {
			return rank[filteredResult[i].Id].queryDencity > rank[filteredResult[j].Id].queryDencity
		}
		return rank[filteredResult[i].Id].tf_idf > rank[filteredResult[j].Id].tf_idf
	})

	return filteredResult[:min(length, maxLen)]

	/*
	// линейная модель ранжирования
	
	for i := range 10 {
		condidates := map[uuid.UUID]requestRanking
		for j := range 10 {
			conId := filteredResult[10 * j + i].Id
			condidates[conId] = rank[conId]
		}
		best, err := GetPredict(condidates)
		if err != nil {
			return filteredResult
		}
		filteredResult[best * 10 + i], filteredResult[best] = filteredResult[best], filteredResult[best * 10 + i]
	}
	*/
}

func TruncateToTwoDecimalPlaces(f float64) float64 {
	return math.Trunc(f*100) / 100
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

func calcEuclidianDistance(v1, v2 []float64) float64 {
	tmp := 0.0
	for i := range v1 {
		tmp += math.Pow(v2[i] - v1[i], 2.0)
	}
	return math.Sqrt(tmp)
}

func culcBM25(idf float64, tf float64, doc *model.Document, avgLen float64) float64 {
	k1 := 1.2
	b := 0.75
	return idf * (tf * (k1 + 1)) / (tf + k1 * (1 - b + b * float64(doc.WordCount) / avgLen))
}

func calcQueryDencity(positions [][]model.Position, lenQuery int) int {
	minDencity := math.MaxInt

	bs := func(cur, target, l, r int) int {
		for l < r {
			m := (l + r) / 2
			if positions[cur][m].I <= target {
				l = m + 1
			}
			if positions[cur][m].I > target {
				r = m
			}
		}
		return l
	}

	for i := range positions[0] {
		cur := positions[0][i].I
		last := cur
		for j := range lenQuery {
			position := bs(i, last, 0, len(positions[j]))
			if position == len(positions[j]) {
				return minDencity
			}
			last = position
		}
		minDencity = min(minDencity, last - cur)
	}

	return minDencity
}