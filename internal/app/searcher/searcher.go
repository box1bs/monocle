package searcher

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"math"
	"net/http"
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

type vectorizer interface {
	Vectorize(string, context.Context) ([][]float64, error)
}

type Searcher struct {
	mu         	*sync.RWMutex
	vectorizer  vectorizer
	idx 		index
}

func NewSearcher(idx index, vec vectorizer) *Searcher {
	return &Searcher{
		mu:        	&sync.RWMutex{},
		vectorizer: vec,
		idx:       	idx,
	}
}

type requestRanking struct {
	tf_idf 			float64
	bm25 			float64
	WordsCos		float64		`json:"cos"`
	Dpq				float64		`json:"euclid_dist"`
	QueryCoverage	float64		`json:"query_coverage"`
	QueryDencity 	float64		`json:"query_dencity"`
	TermProximity 	int			`json:"term_proximity"`
	IncludesWords 	int			`json:"sum_token_in_package"`
	HasWordInHeader bool		`json:"words_in_header"`
	//any ranking scores
}

func (s *Searcher) Search(query string, maxLen int) []*model.Document {
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
				r.IncludesWords += item.Count
				r.tf_idf += float64(doc.WordCount) * idf
				r.bm25 += culcBM25(idf, float64(item.Count), doc, avgLen)

				positions := []*[]model.Position{}
				coverage := 0.0
				for _, term := range terms {
					positions = append(positions, &item.Positions)
					if len(index[term][docID].Positions) > 0 {
						coverage++
					}
				}
				r.TermProximity += calcQueryDencity(positions, queryLen)
				r.QueryDencity += coverage / float64(len(item.Positions))
				r.QueryCoverage += coverage / float64(queryLen)

				if !r.HasWordInHeader {
					for i := 0; i < item.Count && !r.HasWordInHeader; i++ {
						r.HasWordInHeader = item.Positions[i].Type == 'h'
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
		sumCosW := 0.0
		for _, v := range doc.WordVec {
			sumCosW += calcCosineSimilarity(v, vec[0])
		}
		length := float64(len(doc.WordVec))
		r.WordsCos = sumCosW / length
		sumDistance := 0.0
		for _, v := range doc.WordVec {
			sumDistance += calcEuclidianDistance(v, vec[0])
		}
		r.Dpq = sumDistance / length
		rank[doc.Id] = r
		filteredResult = append(filteredResult, doc)
	}

	length = len(filteredResult)
	if length == 0 {
		return nil
	}

	sort.Slice(filteredResult, func(i, j int) bool {
		if TruncateToTwoDecimalPlaces(rank[filteredResult[i].Id].WordsCos) != TruncateToTwoDecimalPlaces(rank[filteredResult[j].Id].WordsCos) {
			return rank[filteredResult[i].Id].WordsCos > rank[filteredResult[j].Id].WordsCos
		}
		if TruncateToTwoDecimalPlaces(rank[filteredResult[i].Id].Dpq) != TruncateToTwoDecimalPlaces(rank[filteredResult[j].Id].Dpq) {
			return rank[filteredResult[i].Id].Dpq < rank[filteredResult[j].Id].Dpq
		}
		if rank[filteredResult[i].Id].bm25 != rank[filteredResult[j].Id].bm25 {
			return rank[filteredResult[i].Id].bm25 > rank[filteredResult[j].Id].bm25
		}
		if rank[filteredResult[i].Id].IncludesWords != rank[filteredResult[j].Id].IncludesWords {
			return rank[filteredResult[i].Id].IncludesWords > rank[filteredResult[j].Id].IncludesWords
		}
		if rank[filteredResult[i].Id].TermProximity != rank[filteredResult[j].Id].TermProximity {
			return rank[filteredResult[i].Id].TermProximity > rank[filteredResult[j].Id].TermProximity
		}
		return rank[filteredResult[i].Id].tf_idf > rank[filteredResult[j].Id].tf_idf
	})

	fl := filteredResult[:min(length, maxLen)]
	
	n := len(fl)
	for i := range n / 10 {
		condidates := map[[32]byte]requestRanking{}
		list := [][32]byte{}
		for j := range 10 {
			conId := fl[10 * j + i].Id
			condidates[conId] = rank[conId]
			list = append(list, conId)
		}
		bestPos, err := callRankAPI(list, condidates)
		if err != nil {
			panic(err)
			return fl
		}
		fl[i * 10 + bestPos], fl[i] = fl[i], fl[i * 10 + bestPos]
	}

	if endings := n % 10; endings != 0 {
		condidates := map[[32]byte]requestRanking{}
		list := [][32]byte{}
		for j := range endings {
			conId := fl[10 * n / 10 + j].Id
			condidates[conId] = rank[conId]
		}
		bestPos, err := callRankAPI(list, condidates)
		if err != nil {
			panic(err)
			return fl
		}
		fl[n / 10 * 10 + bestPos], fl[n / 10] = fl[n / 10], fl[n / 10 * 10 + bestPos]
	}

	return fl
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

func calcQueryDencity(positions []*[]model.Position, lenQuery int) int {
	minDencity := math.MaxInt

	bs := func(cur, target, l, r int) int {
		for l < r {
			m := (l + r) / 2
			if (*positions[cur])[m].I <= target {
				l = m + 1
			}
			if (*positions[cur])[m].I > target {
				r = m
			}
		}
		return l
	}

	for i := range *positions[0] {
		cur := (*positions[0])[i].I
		last := cur
		for j := range lenQuery {
			position := bs(i, last, 0, len(*positions[j]))
			if position == len(*positions[j]) {
				return minDencity
			}
			last = position
		}
		minDencity = min(minDencity, last - cur)
	}

	return minDencity
}

type rankingResponse struct {
	Relevances []float64 `json:"rel"`
}

func callRankAPI(conds [][32]byte, features map[[32]byte]requestRanking) (int, error) {
	var requestBody []requestRanking
	for _, c := range conds {
		requestBody = append(requestBody, features[c])
	}

	jsonData, err := json.Marshal(requestBody)
	if err != nil {
		return 0, fmt.Errorf("failed to marshal request body: %w", err)
	}

	resp, err := http.Post("http://localhost:50920/rank", "application/json", bytes.NewBuffer(jsonData))
	if err != nil {
		return 0, fmt.Errorf("http post request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return 0, fmt.Errorf("rank api returned non-200 status: %d, body: %s", resp.StatusCode, string(bodyBytes))
	}

	respData := rankingResponse{}
	if err := json.NewDecoder(resp.Body).Decode(&respData); err != nil {
		return 0, fmt.Errorf("failed to decode response body: %w", err)
	}

	for i := 0; i < len(respData.Relevances); i++ {
		if respData.Relevances[i] > 0.0 {
			return i, nil
		}
	}

	return 0, fmt.Errorf("somehow all candidates has been received with 0")
}