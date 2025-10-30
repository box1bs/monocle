package indexer

import (
	"context"
	"sync"

	"github.com/box1bs/monocle/configs"
	"github.com/box1bs/monocle/internal/app/indexer/spellChecker"
	"github.com/box1bs/monocle/internal/app/indexer/textHandling"
	"github.com/box1bs/monocle/internal/app/scraper"
	"github.com/box1bs/monocle/internal/model"
	"github.com/box1bs/monocle/pkg/logger"
	"github.com/box1bs/monocle/pkg/workerPool"
)

type repository interface {
	LoadVisitedUrls(*sync.Map) error
	SaveVisitedUrls(*sync.Map) error
	
	SavePageRank(map[string]float64) error
	LoadPageRank() (map[string]float64, error)

	IndexNGrams([]string, int) error
	GetWordsByNGram(string, int) ([]string, error)
	FlushAll()

	IndexDocumentWords([32]byte, map[string]int, map[string][]model.Position) error
	GetDocumentsByWord(string) (map[[32]byte]model.WordCountAndPositions, error)

	SaveDocument(*model.Document) error
	GetDocumentByID([32]byte) (*model.Document, error)
	GetAllDocuments() ([]*model.Document, error)
	GetDocumentsCount() (int, error)

	CheckContent([32]byte, [32]byte) (bool, *model.Document, error)
}

type indexer struct {
	spider 		*scraper.WebScraper
	stemmer 	*textHandling.EnglishStemmer
	sc 			*spellChecker.SpellChecker
	logger 		*logger.Logger
	vectorizer 	*textHandling.Vectorizer
	mu 			*sync.RWMutex
	pageRank 	map[string]float64
	repository 	repository
}

func NewIndexer(repo repository, vec *textHandling.Vectorizer, log *logger.Logger, config *configs.ConfigData) (*indexer, error) {
	idx := &indexer{
		vectorizer: vec,
		stemmer:   	textHandling.NewEnglishStemmer(),
		mu: 		new(sync.RWMutex),
		repository: repo,
		logger:    	log,
	}
	
	idx.sc = spellChecker.NewSpellChecker(config.MaxTypo, config.NGramCount)
	idx.logger.Write(logger.NewMessage(logger.INDEX_LAYER, logger.INFO, "spell checker initialized"))

	var err error
	idx.pageRank, err = idx.repository.LoadPageRank()
	if err != nil {
		idx.logger.Write(logger.NewMessage(logger.INDEX_LAYER, logger.CRITICAL_ERROR, "db error: %v", err))
		return nil, err
	}
	return idx, nil
}

func (idx *indexer) Index(config *configs.ConfigData, global context.Context) error {
	defer idx.repository.SavePageRank(idx.pageRank)
	vis := &sync.Map{}
	if err := idx.repository.LoadVisitedUrls(vis); err != nil {
		return err
	}
	defer idx.repository.SaveVisitedUrls(vis)
	defer idx.repository.FlushAll()

	idx.spider = scraper.NewScraper(vis, &scraper.ConfigData{
		StartURLs:     	config.BaseURLs,
		CacheCap: 		config.CacheCap,	
		Depth:       	config.MaxDepth,
		MaxVisitedDeep: config.MaxVisitedDepth,
		MaxLinksInPage: config.MaxLinksInPage,
		OnlySameDomain: config.OnlySameDomain,
	}, idx.logger, workerPool.NewWorkerPool(config.WorkersCount, config.TasksCount, global, idx.logger), idx, global, idx.vectorizer.PutDocQuery)
	idx.spider.Run()
	return nil
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