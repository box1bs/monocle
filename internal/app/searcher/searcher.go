package searcher

import (
	"context"
	"math"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/box1bs/monocle/internal/model"
	"github.com/box1bs/monocle/pkg/logger"
)

type index interface {
	HandleTextQuery(string) ([]string, []map[[32]byte]model.WordCountAndPositions, error)
	GetAVGLen() (float64, error)
}

type resitory interface {
	GetDocumentsCount() (int, error)
	GetDocumentByID([32]byte) (*model.Document, error)
}

type vectorizer interface {
	PutDocQuery(string, context.Context) <-chan [][]float64
}

type Searcher struct {
	log 		*logger.Logger
	mu         	*sync.RWMutex
	vectorizer  vectorizer
	idx 		index
	repo 	 	resitory
}

func NewSearcher(l *logger.Logger, idx index, repo resitory, vec vectorizer) *Searcher {
	return &Searcher{
		log: 		l,
		mu:        	&sync.RWMutex{},
		vectorizer: vec,
		idx:       	idx,
		repo: 	 	repo,
	}
}

type requestRanking struct {
	tf_idf 				float64		`json:"-"`
	bm25 				float64		`json:"-"`
	WordsCos			float64		`json:"cos"`
	Dpq					float64		`json:"euclid_dist"`
	QueryCoverage		float64		`json:"query_coverage"`
	QueryDencity 		float64		`json:"query_dencity"`
	LogLenWordInURL 	float64		`json:"log_len_words_in_url"`
	LenURL 				int 		`json:"len_url"`
	TermProximity 		int			`json:"term_proximity"`
	SumTokenInPackage 	int			`json:"sum_token_in_package"`
	HasWordInHeader 	bool		`json:"words_in_header"`
	WordInUrl			bool 		`json:"word_in_url"`
	//any ranking scores
}

func (s *Searcher) Search(query string, maxLen int) []*model.Document {
	s.mu.RLock()
	defer s.mu.RUnlock()
	
	rank := make(map[[32]byte]requestRanking)

	words, index, err := s.idx.HandleTextQuery(query)
	if err != nil {
		s.log.Write(logger.NewMessage(logger.SEARCHER_LAYER, logger.CRITICAL_ERROR, "handling words error: %v", err))
		return nil
	}

	queryLen := len(words)

	avgLen, err := s.idx.GetAVGLen()
	if err != nil {
		s.log.Write(logger.NewMessage(logger.SEARCHER_LAYER, logger.ERROR, "%v", err))
		return nil
	}
	
	length, err := s.repo.GetDocumentsCount()
	if err != nil {
		s.log.Write(logger.NewMessage(logger.SEARCHER_LAYER, logger.ERROR, "%v", err))
		return nil
	}

	result := make([]*model.Document, 0)
	alreadyIncluded := make(map[[32]byte]struct{})
	var wg sync.WaitGroup
	var rankMu sync.RWMutex
	var resultMu sync.Mutex
	done := make(chan struct{})

	for i := range words {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
	
			idf := math.Log(float64(length) / float64(len(index[i]) + 1)) + 1.0
			s.log.Write(logger.NewMessage(logger.SEARCHER_LAYER, logger.DEBUG, "len documents with word: %s, %d", words[i], len(index[i])))
	
			for docID, item := range index[i] {
				rankMu.RLock()
				doc, err := s.repo.GetDocumentByID(docID)
				if err != nil || doc == nil {
					rankMu.RUnlock()
					s.log.Write(logger.NewMessage(logger.SEARCHER_LAYER, logger.ERROR, "error: %v, doc: %v", err, doc))
					continue
				}
				rankMu.RUnlock()
				
				rankMu.Lock()
				r, ex := rank[docID]
				if !ex {
					rank[docID] = requestRanking{}
				}
				r.SumTokenInPackage += item.Count
				r.tf_idf += float64(doc.WordCount) * idf
				r.bm25 += culcBM25(idf, float64(item.Count), doc, avgLen)
				if r.TermProximity == 0 {
					positions := []*[]model.Position{}
					coverage := 0.0
					docLen := 0.0
					for i := range words {
						positions = append(positions, &item.Positions)
						if l := len(index[i][docID].Positions); l > 0 {
							coverage++
							docLen += float64(l)
						}
					}
					r.TermProximity = getMinQueryDistInDoc(positions, queryLen)
					r.QueryDencity = coverage / docLen
					r.QueryCoverage = coverage / float64(queryLen)

					r.WordInUrl, r.LogLenWordInURL = boyerMoorAlgorithm(strings.ToLower(doc.URL), words)
					r.LenURL = len(doc.URL)
					
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
		}(i)
	}
	
	go func() {
		wg.Wait()
		close(done)
	}()
	
	c, cancel := context.WithTimeout(context.Background(), 5 * time.Second)
	defer cancel()
	vec, ok := <-s.vectorizer.PutDocQuery(query, c)
	if !ok {
		s.log.Write(logger.NewMessage(logger.SEARCHER_LAYER, logger.CRITICAL_ERROR, "error vectorozing query"))
		return nil
	}
	
	s.log.Write(logger.NewMessage(logger.SEARCHER_LAYER, logger.DEBUG, "result len: %d", len(result)))
	
	<-done
	
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
		s.log.Write(logger.NewMessage(logger.SEARCHER_LAYER, logger.DEBUG, "empty result"))
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
			s.log.Write(logger.NewMessage(logger.SEARCHER_LAYER, logger.CRITICAL_ERROR, "python server error: %v", err))
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
			s.log.Write(logger.NewMessage(logger.SEARCHER_LAYER, logger.CRITICAL_ERROR, "python server error: %v", err))
			return fl
		}
		fl[n / 10 * 10 + bestPos], fl[n / 10] = fl[n / 10], fl[n / 10 * 10 + bestPos]
	}

	return fl
}

func TruncateToTwoDecimalPlaces(f float64) float64 {
	return math.Trunc(f*100) / 100
}