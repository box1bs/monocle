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
	stemmer 	*textHandling.EnglishStemmer
	sc 			*spellChecker.SpellChecker
	logger 		*logger.Logger
	vectorizer 	*textHandling.Vectorizer
	mu 			*sync.RWMutex
	pageRank 	map[string]float64
	repository 	repository
}

func NewIndexer(repo repository,vec *textHandling.Vectorizer, logger *logger.Logger) (*indexer, error) {
	return &indexer{
		vectorizer: vec,
		stemmer:   	textHandling.NewEnglishStemmer(),
		mu: 		new(sync.RWMutex),
		repository: repo,
		logger:    	logger,
	}, nil
}

func (idx *indexer) Index(config *configs.ConfigData, global context.Context) {
	vis := &sync.Map{}
	idx.repository.LoadVisitedUrls(vis)
	defer idx.repository.SaveVisitedUrls(vis)
	defer idx.repository.FlushAll()
	
	wp := workerPool.NewWorkerPool(config.WorkersCount, config.TasksCount, global, idx.logger)
	idx.logger.Write(logger.NewMessage(logger.INDEX_LAYER, logger.INFO, "worker pool initialized"))
	
	idx.sc = spellChecker.NewSpellChecker(config.MaxTypo, config.NGramCount)
	idx.logger.Write(logger.NewMessage(logger.INDEX_LAYER, logger.INFO, "spell checker initialized"))

	var err error
	idx.pageRank, err = idx.repository.LoadPageRank()
	defer idx.repository.SavePageRank(idx.pageRank)
	if err != nil {
		idx.logger.Write(logger.NewMessage(logger.INDEX_LAYER, logger.CRITICAL_ERROR, "db error: %v", err))
		return
	}

	scraper.NewScraper(vis, &scraper.ConfigData{
		StartURLs:     	config.BaseURLs,
		Depth:       	config.MaxDepth,
		MaxLinksInPage: config.MaxLinksInPage,
		OnlySameDomain: config.OnlySameDomain,
	}, idx.logger, wp, idx, global, idx.vectorizer.PutDocQuery).Run()
}

func (idx *indexer) HandleDocumentWords(doc *model.Document, passages []model.Passage) error {
	stem := map[string]int{}
	i := 0
	pos := map[string][]model.Position{}

	idx.mu.Lock()
	defer idx.mu.Unlock()

	allWords := []string{}

	for _, passage := range passages {
		words, stemmed, err := idx.stemmer.TokenizeAndStem(passage.Text)
		if err != nil {
			return err
		}
		if len(stemmed) == 0 {
			continue
		}
		doc.WordCount += len(stemmed)

		allWords = append(allWords, words...)
		for _, w := range stemmed {
			stem[w]++
			pos[w] = append(pos[w], model.NewTypeTextObj[model.Position](passage.Type, "", i))
			i++
		}
	}
	
	if err := idx.repository.IndexNGrams(allWords, idx.sc.NGramCount); err != nil {
		idx.logger.Write(logger.NewMessage(logger.INDEX_LAYER, logger.CRITICAL_ERROR, "error indexing ngrams: %v", err))
		return err
	}
	if err := idx.repository.IndexDocumentWords(doc.Id, stem, pos); err != nil {
		idx.logger.Write(logger.NewMessage(logger.INDEX_LAYER, logger.CRITICAL_ERROR, "error indexing document words: %v", err))
		return err
	}
	if err := idx.repository.SaveDocument(doc); err != nil {
		idx.logger.Write(logger.NewMessage(logger.INDEX_LAYER, logger.CRITICAL_ERROR, "error saving document: %v", err))
		return err
	}

	return nil
}

func (idx *indexer) HandleTextQuery(text string) ([]string, []map[[32]byte]model.WordCountAndPositions, error) {
	idx.mu.RLock()
	defer idx.mu.RUnlock()
	reverthIndex := []map[[32]byte]model.WordCountAndPositions{}
	words, stemmed, err := idx.stemmer.TokenizeAndStem(text)

	for i, lemma := range stemmed {
		documents, err := idx.repository.GetDocumentsByWord(lemma)
		if err != nil {
			return nil, nil, err
		}
		if len(documents) == 0 {
			conds, err := idx.repository.GetWordsByNGram(words[i], idx.sc.NGramCount)
			if err != nil {
				return nil, nil, err
			}
			replacement := idx.sc.BestReplacement(words[i], conds)
			idx.logger.Write(logger.NewMessage(logger.INDEX_LAYER, logger.DEBUG, "word '%s' replaced with '%s' in query", words[i], replacement))
			_, stem, err := idx.stemmer.TokenizeAndStem(replacement)
			if err != nil {
				return nil, nil, err
			}
			stemmed[i] = stem[0]
			documents, err = idx.repository.GetDocumentsByWord(stem[0])
			if err != nil {
				return nil, nil, err
			}
		}
		reverthIndex = append(reverthIndex, documents)
	}

	return stemmed, reverthIndex, err
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

func (idx *indexer) CalcPageRank(url string, linksCount int) {
	idx.mu.Lock()
	defer idx.mu.Unlock()
	idx.pageRank[url] = 1.0 / float64(linksCount)
}

func (idx *indexer) GetPageRank(url string) float64 {
	idx.mu.RLock()
	defer idx.mu.RUnlock()
	return idx.pageRank[url]
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

func (idx *indexer) GetDocumentsByWord(word string) (map[[32]byte]model.WordCountAndPositions, error) {
	idx.mu.RLock()
	defer idx.mu.RUnlock()
	return idx.repository.GetDocumentsByWord(word)
}