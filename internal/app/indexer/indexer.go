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
	IndexDocumentWords([32]byte, []int, map[string][]model.Position) error
	GetDocumentsByWord(int) (map[[32]byte]*model.WordCountAndPositions, error)
	//IndexNGrams(string, ...string) error
	//GetWordsByNGrams(...string) ([]string, error)
	
	SavePageRank(map[string]float64) error
	LoadPageRank() (map[string]float64, error)

	GetProbByWord(...string) (int, error)
	SaveUnigramProb(map[string]int) error
	SaveBigramProb(map[string]map[string]int) error

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

func NewIndexer(repo repository, vec vectorizer, logger logger, maxTypo, nGramCount int) (*indexer, error) {
	return &indexer{
		stemmer:   	textHandling.NewEnglishStemmer(),
		sc:        	spellChecker.NewSpellChecker(maxTypo, nGramCount),
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
		words, stemmed, err := idx.stemmer.TokenizeAndStem(passage.Text)
		if err != nil {
			return err
		}
		if len(stemmed) == 0 || len(words) == 0 {
			continue
		}

		wl := len(words)
		bg := map[string]map[string]int{}
		ug := map[string]int{}
		for i := range wl {
			if i + 1 < wl {
				if _, ok := bg[words[i]]; !ok {
					bg[words[i]] = map[string]int{}
				}
				bg[words[i]][words[i + 1]]++
			}
			ug[words[i]]++
			//if err := idx.repository.IndexNGrams(words[i], idx.sc.BreakToNGrams(words[i])...); err != nil {
			//	return err
			//}
		}

		if err := idx.repository.SaveBigramProb(bg); err != nil {
			return err
		}
		if err := idx.repository.SaveUnigramProb(ug); err != nil {
			return err
		}

		s, err := idx.repository.SaveToSequence(stemmed...)
		if err != nil {
			log.Printf("error saving to sequence: %v", err)
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
		log.Printf("error indexing words: %v", err)
		return err
	}
	if err := idx.repository.SaveDocument(doc); err != nil {
		log.Printf("error saving words: %v", err)
		return err
	}

	return nil
}

func (idx *indexer) HandleTextQuery(text string) ([]int, error) {
	idx.mu.RLock()
	defer idx.mu.RUnlock()

	_, stemmed, err := idx.stemmer.TokenizeAndStem(text)
	if err != nil {
		return nil, err
	}

	log.Println(stemmed)

	sequence, err := idx.repository.TransferToSequence(stemmed...)
	if err != nil {
		return nil, err
	}
	/*
	for i, word := range sequence {
		if word == -1 {
			condidates, err := idx.repository.GetWordsByNGrams(idx.sc.BreakToNGrams(words[i])...)
			if err != nil || len(condidates) == 0 {
				continue
			}

			log.Printf("word: %s, has %d code", words[i], sequence[i])

			before := ""
			if i > 0 {
				before = words[i - 1]
			}
			_, stemmedReplacement, err := idx.stemmer.TokenizeAndStem(idx.sc.BestReplacement(words[i], before, condidates, idx.repository.GetProbByWord))
			if err != nil || len(stemmedReplacement) == 0 {
				continue
			}
			replacementSeq, err := idx.repository.TransferToSequence(stemmedReplacement[0])
			if err != nil || len(replacementSeq) == 0 || replacementSeq[0] == -1 {
				continue
			}
			sequence[i] = replacementSeq[0]
		}
	}
	*/
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