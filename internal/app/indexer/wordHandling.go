package indexer

import (
	"crypto/sha256"
	"encoding/json"

	"github.com/box1bs/monocle/internal/app/indexer/textHandling"
	"github.com/box1bs/monocle/internal/model"
	"github.com/box1bs/monocle/pkg/logger"
)

func (idx *indexer) HandleDocumentWords(doc *model.Document, passages []model.Passage) error {
	stem := map[string]int{}
	i := 0
	pos := map[string][]model.Position{}

	idx.mu.Lock()
	defer idx.mu.Unlock()

	allTokens := []string{}

	for _, passage := range passages {
		orig, stemmed, err := idx.stemmer.TokenizeAndStem(passage.Text)
		if err != nil {
			return err
		}
		if len(stemmed) == 0 {
			continue
		}
		doc.WordCount += len(stemmed)

		allTokens = append(allTokens, orig...)
		for _, w := range stemmed {
			if w.Type == textHandling.NUMBER {
				continue
			}
			stem[w.Value]++
			pos[w.Value] = append(pos[w.Value], model.NewTypeTextObj[model.Position](passage.Type, "", i))
			i++
		}
	}

	if err := idx.repository.SaveDocument(doc); err != nil {
		idx.logger.Write(logger.NewMessage(logger.INDEX_LAYER, logger.CRITICAL_ERROR, "error saving document: %v", err))
		return err
	}
	if err := idx.repository.IndexNGrams(allTokens, idx.sc.NGramCount); err != nil {
		idx.logger.Write(logger.NewMessage(logger.INDEX_LAYER, logger.CRITICAL_ERROR, "error indexing ngrams: %v", err))
		return err
	}
	if err := idx.repository.IndexDocumentWords(doc.Id, stem, pos); err != nil {
		idx.logger.Write(logger.NewMessage(logger.INDEX_LAYER, logger.CRITICAL_ERROR, "error indexing document words: %v", err))
		return err
	}

	return nil
}

func (idx *indexer) HandleTextQuery(text string) ([]string, []map[[32]byte]model.WordCountAndPositions, error) {
	idx.mu.RLock()
	defer idx.mu.RUnlock()
	reverthIndex := []map[[32]byte]model.WordCountAndPositions{}
	words, stemmed, err := idx.stemmer.TokenizeAndStem(text)
	stemmedTokens := []string{}

	for i, lemma := range stemmed {
		documents, err := idx.repository.GetDocumentsByWord(lemma.Value)
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
			if stem[0].Value == "" { // если заменяется на стоп слово
				continue
			}
			stemmed[i] = stem[0]
			documents, err = idx.repository.GetDocumentsByWord(stem[0].Value)
			if err != nil {
				return nil, nil, err
			}
		}
		stemmedTokens = append(stemmedTokens, stemmed[i].Value)
		reverthIndex = append(reverthIndex, documents)
	}

	return stemmedTokens, reverthIndex, err
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