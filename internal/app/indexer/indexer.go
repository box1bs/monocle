package indexer

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"sync"

	"github.com/box1bs/monocle/configs"
	"github.com/box1bs/monocle/internal/app/indexer/spellChecker"
	"github.com/box1bs/monocle/internal/app/indexer/textHandling"
	"github.com/box1bs/monocle/internal/app/scraper"
	"github.com/box1bs/monocle/internal/model"
	"github.com/box1bs/monocle/pkg/workerPool"
)

type repository interface {
	LoadVisitedUrls(*sync.Map) error
	SaveVisitedUrls(*sync.Map) error
	IndexDocumentWords([32]byte, []int, map[string][]model.Position) error
	GetDocumentsByWord(int) (map[[32]byte]*model.WordCountAndPositions, error)
	IndexNGrams(string, ...string) error
	GetWordsByNGrams(...string) ([]string, error)
	
	SavePageRank(map[string]int) error
	LoadPageRank() (map[string]int, error)

	SaveDocument(*model.Document) error
	GetDocumentByID([32]byte) (*model.Document, error)
	GetAllDocuments() ([]*model.Document, error)
	GetDocumentsCount() (int, error)

	CheckContent([32]byte, [32]byte) (bool, *model.Document, error)
	
	TransferToSequence(...string) ([]int, error)
	SaveToSequence(...string) ([]int, error)
}

type logger interface {
	Write(string)
}

type vectorizer interface {
	Vectorize(string, context.Context) ([][]float64, error)
}

type indexer struct {
	stemmer 	*textHandling.EnglishStemmer
	sc 			*spellChecker.SpellChecker
	mu 			*sync.RWMutex
	repository 	repository
	vectorizer 	vectorizer
	logger 		logger
}

func NewIndexer(repo repository, vec vectorizer, logger logger, maxTypo, nGramCount int) *indexer {
	return &indexer{
		stemmer:   	textHandling.NewEnglishStemmer(),
		sc:        	spellChecker.NewSpellChecker(maxTypo, nGramCount),
		mu: new(sync.RWMutex),
		repository: repo,
		vectorizer: vec,
		logger:    	logger,
	}
}

func (idx *indexer) Index(config *configs.ConfigData, global context.Context) {
	vis := &sync.Map{}
	idx.repository.LoadVisitedUrls(vis)
	defer idx.repository.SaveVisitedUrls(vis)
	
	wp := workerPool.NewWorkerPool(config.WorkersCount, config.TasksCount)
	
	pr, err := idx.repository.LoadPageRank()
	defer idx.repository.SavePageRank(pr)
	if err != nil {
		idx.logger.Write(err.Error())
		return
	}

	scraper.NewScraper(vis, &scraper.ConfigData{
		StartURLs:     	config.BaseURLs,
		Depth:       	config.MaxDepth,
		MaxLinksInPage: config.MaxLinksInPage,
		OnlySameDomain: config.OnlySameDomain,
		Rate:			config.Rate,
		DocNGramCount: 	64,
	}, wp, idx, global, pr, idx.logger.Write, idx.vectorizer.Vectorize).Run()
}

func (idx *indexer) HandleDocumentWords(doc *model.Document, passages []model.Passage) error {
	var i = 0
	var sequence []int
	positions := map[string][]model.Position{}

	idx.mu.Lock()
	defer idx.mu.Unlock()

	for _, passage := range passages {
		stemmed, err := idx.stemmer.TokenizeAndStem(passage.Text, idx.sc.BreakToNGrams, idx.repository.IndexNGrams)
		if err != nil {
			return err
		}
		if len(stemmed) == 0 {
			continue
		}

		s, err := idx.repository.SaveToSequence(stemmed...)
		if err != nil {
			return err
		}
		sequence = append(sequence, s...)

		doc.WordCount += len(s)
		for _, word := range stemmed {
			positions[word] = append(positions[word], model.NewTypeTextObj[model.Position](passage.Type, "", i))
			i++
		}
	}
	
	if err := idx.repository.IndexDocumentWords(doc.Id, sequence, positions); err != nil {
		return err
	}
	if err := idx.repository.SaveDocument(doc); err != nil {
		return err
	}

	return nil
}

func (idx *indexer) HandleTextQuery(text string) ([]int, error) {
	idx.mu.RLock()
	defer idx.mu.RUnlock()

	stemmed, err := idx.stemmer.TokenizeAndStem(text, nil, nil)
	if err != nil {
		return nil, err
	}

	sequence, err := idx.repository.TransferToSequence(stemmed...)
	if err != nil {
		return nil, err
	}

	for i, word := range sequence {
		if word == 0 { // переделать оно ищет по нормализованой форме, а нужно по изначальной
			condidates, err := idx.repository.GetWordsByNGrams(idx.sc.BreakToNGrams(stemmed[i])...)
			if err != nil || len(condidates) == 0 {
				continue
			}
			stemmedReplacement, err := idx.stemmer.TokenizeAndStem(idx.sc.BestReplacement(stemmed[i], min(i, 2), condidates), nil, nil)
			if err != nil || len(stemmedReplacement) == 0 {
				continue
			}
			replacementSeq, err := idx.repository.TransferToSequence(stemmedReplacement...)
			if err != nil || len(replacementSeq) == 0 || replacementSeq[0] == 0 {
				continue
			}
			sequence[i] = replacementSeq[0]
		}
	}
	return sequence, nil
}

func (idx *indexer) GetAVGLen() (float64, error) {
	idx.mu.RLock()
	defer idx.mu.RUnlock()

	var wordCount int
	docs, err := idx.repository.GetAllDocuments()
	if err != nil {
		return 0, err
	}

	for _, doc := range docs {
		wordCount += int(doc.WordCount)
	}

	return float64(wordCount) / float64(len(docs)), nil
}

func (idx *indexer) IsCrawledContent(id [32]byte, content []model.Passage) (bool, error) {
	c, err := json.Marshal(content)
	if err != nil {
		return false, err
	}
	hash := sha256.Sum256(c)

	idx.mu.RLock()
	crawled, doc, err := idx.repository.CheckContent(id, hash)
	if err != nil && err.Error() != "content already exists" {
		idx.mu.RUnlock()
		return false, err
	}
	idx.mu.RUnlock()

	if doc == nil {
		return false, nil
	}

	idx.mu.Lock()
	defer idx.mu.Unlock()
	if crawled {
		doc.Id = id
		if err := idx.repository.SaveDocument(doc); err != nil {
			return true, err
		}
	}

	return crawled, err
}

func (idx *indexer) GetDocumentByID(id [32]byte) (*model.Document, error) {
	idx.mu.RLock()
	defer idx.mu.RUnlock()

	return idx.repository.GetDocumentByID(id)
}

func (idx *indexer) GetDocumentsCount() (int, error) {
	idx.mu.RLock()
	defer idx.mu.RUnlock()

	return idx.repository.GetDocumentsCount()
}

func (idx *indexer) GetDocumentsByWord(word int) (map[[32]byte]*model.WordCountAndPositions, error) {
	idx.mu.RLock()
	defer idx.mu.RUnlock()

	return idx.repository.GetDocumentsByWord(word)
}