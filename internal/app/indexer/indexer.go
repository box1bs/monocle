package indexer

import (
	"context"
	"math/rand"
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
	IndexUrlsByHash([32]byte, []byte) error
	GetPageUrlsByHash([32]byte) ([]byte, error)

	LoadVisitedUrls(*sync.Map) error
	SaveVisitedUrls(*sync.Map) error

	IndexNGrams([]string, int) error
	GetWordsByNGram(string, int) ([]string, error)
	IndexDocShingles([128]uint64) error
	GetSimilarSignatures([128]uint64) ([][128]uint64, error)
	FlushAll()

	SaveSaltArrays([128]uint64, [128]uint64) error
	UploadSaltArrays() (*[128]uint64, *[128]uint64, error)

	IndexDocumentWords([32]byte, map[string]int, map[string][]model.Position) error
	GetDocumentsByWord(string) (map[[32]byte]model.WordCountAndPositions, error)

	SaveDocument(*model.Document) error
	GetDocumentByID([32]byte) (*model.Document, error)
	GetAllDocuments() ([]*model.Document, error)
	GetDocumentsCount() (int, error)
}

type indexer struct {
	spider 		*scraper.WebScraper
	stemmer 	*textHandling.EnglishStemmer
	sc 			*spellChecker.SpellChecker
	logger 		*logger.Logger
	vectorizer 	*textHandling.Vectorizer
	minHash 	*minHash
	mu 			*sync.RWMutex
	repository 	repository
}

func NewIndexer(repo repository, vec *textHandling.Vectorizer, log *logger.Logger, config *configs.ConfigData) *indexer {
	return &indexer{
		vectorizer: vec,
		stemmer:   	textHandling.NewEnglishStemmer(),
		mu: 		new(sync.RWMutex),
		repository: repo,
		sc: 		spellChecker.NewSpellChecker(config.MaxTypo, config.NGramCount),
		logger:    	log,
	}
}

func (idx *indexer) Index(config *configs.ConfigData, global context.Context) error {
	vis := &sync.Map{}
	if err := idx.repository.LoadVisitedUrls(vis); err != nil {
		return err
	}
	defer idx.repository.SaveVisitedUrls(vis)
	defer idx.repository.FlushAll()
	if a, b, err := idx.repository.UploadSaltArrays(); err != nil {
		return err
	} else if a == nil || b == nil {
		idx.minHash = NewHasher(rand.New(rand.NewSource(1)), nil, nil)
	} else {
		idx.minHash = NewHasher(nil, a, b)
	}
	defer idx.repository.SaveSaltArrays(idx.minHash.a, idx.minHash.b)

	idx.spider = scraper.NewScraper(vis, &scraper.ConfigData{
		StartURLs:     	config.BaseURLs,
		CacheCap: 		config.CacheCap,	
		Depth:       	config.MaxDepth,
		OnlySameDomain: config.OnlySameDomain,
	}, idx.logger,
		workerPool.NewWorkerPool(config.WorkersCount, config.TasksCount, global, idx.logger),
		idx, global, idx.vectorizer.PutDocQuery)
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

func (idx *indexer) SaveUrlsToBank(key [32]byte, data []byte) error {
	return idx.repository.IndexUrlsByHash(key, data)
}

func (idx *indexer) GetUrlsByHash(key [32]byte) ([]byte, error) {
	return idx.repository.GetPageUrlsByHash(key)
}