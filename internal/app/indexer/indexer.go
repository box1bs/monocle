package indexer

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"log"
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
	
	SavePageRank(map[string]float64) error
	LoadPageRank() (map[string]float64, error)

	IndexDocumentWords([32]byte, map[string]int, map[string][]model.Position) error
	GetDocumentsByWord(string) (map[[32]byte]model.WordCountAndPositions, error)
	GetAllWords() []string

	SaveDocument(*model.Document) error
	GetDocumentByID([32]byte) (*model.Document, error)
	GetAllDocuments() ([]*model.Document, error)
	GetDocumentsCount() (int, error)

	CheckContent([32]byte, [32]byte) (bool, *model.Document, error)
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

func NewIndexer(repo repository, vec vectorizer, logger logger, maxTypo int) (*indexer, error) {
	return &indexer{
		stemmer:   	textHandling.NewEnglishStemmer(),
		sc:        	spellChecker.NewSpellChecker(maxTypo),
		mu: new(sync.RWMutex),
		repository: repo,
		vectorizer: vec,
		logger:    	logger,
	}, nil
}

func (idx *indexer) Index(config *configs.ConfigData, global context.Context) {
	vis := &sync.Map{}
	idx.repository.LoadVisitedUrls(vis)
	defer idx.repository.SaveVisitedUrls(vis)
	
	wp := workerPool.NewWorkerPool(config.WorkersCount, config.TasksCount, global)
	
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
	}, wp, idx, global, pr, idx.logger.Write, idx.vectorizer.Vectorize).Run()
}

func (idx *indexer) HandleDocumentWords(doc *model.Document, passages []model.Passage) error {
	stem := map[string]int{}
	i := 0
	pos := map[string][]model.Position{}

	idx.mu.Lock()
	defer idx.mu.Unlock()

	for _, passage := range passages {
		_, stemmed, err := idx.stemmer.TokenizeAndStem(passage.Text)
		if err != nil {
			return err
		}
		if len(stemmed) == 0 {
			continue
		}
		doc.WordCount += len(stemmed)

		for _, w := range stemmed {
			stem[w]++
			pos[w] = append(pos[w], model.NewTypeTextObj[model.Position](passage.Type, "", i))
			i++
		}
	}
	
	if err := idx.repository.IndexDocumentWords(doc.Id, stem, pos); err != nil {
		log.Printf("error indexing words: %v", err)
		return err
	}
	if err := idx.repository.SaveDocument(doc); err != nil {
		log.Printf("error saving words: %v", err)
		return err
	}

	return nil
}

func (idx *indexer) HandleTextQuery(text string) ([]string, []map[[32]byte]model.WordCountAndPositions, error) {
	idx.mu.RLock()
	defer idx.mu.RUnlock()
	reverthIndex := []map[[32]byte]model.WordCountAndPositions{}
	_, stemmed, err := idx.stemmer.TokenizeAndStem(text)

	for i, lemma := range stemmed {
		documents, err := idx.repository.GetDocumentsByWord(lemma)
		if err != nil {
			return nil, nil, err
		}
		if len(documents) == 0 {
			replacement := idx.sc.BestReplacement(stemmed[i], idx.repository.GetAllWords())
			log.Printf("%s has no documents, was replaced by: %s", stemmed[i], replacement)
			//_, stem, err := idx.stemmer.TokenizeAndStem(replacement)
			//if err != nil {
			//	return nil, nil, err
			//}
			stemmed[i] = replacement
			documents, err = idx.repository.GetDocumentsByWord(replacement)
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