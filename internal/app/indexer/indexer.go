package indexer

import (
	"context"
	"crypto/sha256"
	"fmt"
	"sync"

	"github.com/box1bs/Saturday/configs"
	"github.com/box1bs/Saturday/internal/app/indexer/spellChecker"
	"github.com/box1bs/Saturday/internal/app/indexer/textHandling"
	"github.com/box1bs/Saturday/internal/app/scraper"
	"github.com/box1bs/Saturday/internal/model"
	"github.com/box1bs/Saturday/pkg/workerPool"
	"github.com/google/uuid"
)

type repository interface {
	LoadVisitedUrls(*sync.Map) error
	SaveVisitedUrls(*sync.Map) error
	IndexDocumentWords(uuid.UUID, []int, []int) error
	GetDocumentsByWord(int) (map[uuid.UUID]int, map[uuid.UUID]struct{}, error)
	IndexNGrams(...string) error
	GetWordsByNGrams(...string) ([]string, error)
	
	SaveDocument(doc *model.Document) error
	GetDocumentByID(uuid.UUID) (*model.Document, error)
	GetAllDocuments() ([]*model.Document, error)
	GetDocumentsCount() (int, error)

	CheckContent(uuid.UUID, [32]byte) (bool, *model.Document, error)
	
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
	repository 	repository
	vectorizer 	vectorizer
	logger 		logger
}

func NewIndexer(repo repository, vec vectorizer, logger logger, maxTypo, nGramCount int) *indexer {
	return &indexer{
		stemmer:   	textHandling.NewEnglishStemmer(),
		sc:        	spellChecker.NewSpellChecker(maxTypo, nGramCount),
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

	scraper.NewScraper(vis, &scraper.ConfigData{
		StartURLs:     	config.BaseURLs,
		Depth:       	config.MaxDepth,
		MaxLinksInPage: config.MaxLinksInPage,
		OnlySameDomain: config.OnlySameDomain,
	}, wp, idx, global, idx.logger.Write, idx.vectorizer.Vectorize).Run()
}

func (idx *indexer) HandleDocumentWords(doc *model.Document, words, header string) error {
	stemmed, err := idx.stemmer.TokenizeAndStem(words, func(s string) error {
		return idx.repository.IndexNGrams(idx.sc.BreakToNGrams(s)...)
	})
	if err != nil {
		return err
	}
	if len(stemmed) == 0 {
		return fmt.Errorf("no valid words found")
	}
	
	sequence, err := idx.repository.SaveToSequence(stemmed...)
	if err != nil {
		return err
	}

	doc.WordCount = len(sequence)
	headerSeq := []int{}
	
	if header != "" {
		headerStemmed, err := idx.stemmer.TokenizeAndStem(header, func(s string) error {
			return idx.repository.IndexNGrams(idx.sc.BreakToNGrams(s)...)
		})
		if err != nil {
			return err
		}
		headerSeq, err = idx.repository.SaveToSequence(headerStemmed...)
		if err != nil {
			return err
		}
		doc.WordCount += len(headerSeq)
	}
	idx.repository.IndexDocumentWords(doc.Id, headerSeq, sequence)
	doc.PartOfFullSize = 256.0 / float64(doc.WordCount)
	idx.repository.SaveDocument(doc)

	return nil
}

func (idx *indexer) HandleTextQuery(text string) ([]int, error) {
	stemmed, err := idx.stemmer.TokenizeAndStem(text, nil)
	if err != nil {
		return nil, err
	}

	sequence, err := idx.repository.TransferToSequence(stemmed...)
	if err != nil {
		return nil, err
	}

	for i, word := range sequence {
		if word == 0 {
			condidates, err := idx.repository.GetWordsByNGrams(idx.sc.BreakToNGrams(stemmed[i])...)
			if err != nil || len(condidates) == 0 {
				continue
			}
			stemmedReplacement, err := idx.stemmer.TokenizeAndStem(idx.sc.BestReplacement(stemmed[i], condidates), nil)
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
	var wordCount int
	docs, err := idx.repository.GetAllDocuments()
	if err != nil {
		return 0, err
	}
	for _, doc := range docs {
		wordCount += int(doc.GetFullSize())
	}

	return float64(wordCount) / float64(len(docs)), nil
}

func (idx *indexer) IsCrawledContent(id uuid.UUID, content string) (bool, error) {
	hash := sha256.Sum256([]byte(content))

	crawled, doc, err := idx.repository.CheckContent(id, hash)
	if err != nil || doc == nil {
		return false, err
	}

	if crawled {
		doc.Id = id
		if err = idx.repository.SaveDocument(doc); err != nil {
			return true, err
		}
	}

	return crawled, err
}

func (idx *indexer) GetDocumentByID(id uuid.UUID) (*model.Document, error) {
	return idx.repository.GetDocumentByID(id)
}

func (idx *indexer) GetDocumentsCount() (int, error) {
	return idx.repository.GetDocumentsCount()
}

func (idx *indexer) GetDocumentsByWord(word int) (map[uuid.UUID]int, map[uuid.UUID]struct{}, error) {
	return idx.repository.GetDocumentsByWord(word)
}