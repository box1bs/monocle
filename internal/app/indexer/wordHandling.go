package indexer

import (
	"fmt"
	"sync"

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

		allTokens = append(allTokens, orig...)
		for _, w := range stemmed {
			if w.Type == textHandling.NUMBER || len(w.Value) > 64 {
				continue
			}
			stem[w.Value]++
			pos[w.Value] = append(pos[w.Value], model.NewTypeTextObj[model.Position](passage.Type, "", i))
			i++
		}
	}
	doc.WordCount = i

	sign := idx.minHash.CreateSignature(allTokens)
	conds, err := idx.repository.GetSimilarSignatures(sign)
	if err != nil {
		return err
	}
	if simRate := calcSim(sign, conds); simRate > 64 { // > 50% нграмм
		idx.logger.Write(logger.NewMessage(logger.INDEX_LAYER, logger.DEBUG, "finded %d/128 similar page", simRate))
		return fmt.Errorf("page already indexed")
	}
	if err := idx.repository.IndexDocShingles(sign); err != nil {
		return err
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
	if len(stemmed) == 0 {
		return nil, nil, fmt.Errorf("empty tokens")
	}
	stemmedTokens := []string{}
	wordp := 0

	for i, lemma := range stemmed {
		documents, err := idx.repository.GetDocumentsByWord(lemma.Value)
		if err != nil {
			return nil, nil, err
		}
		if len(documents) == 0 && lemma.Type == textHandling.WORD && len(words) > i {
			conds, err := idx.repository.GetWordsByNGram(words[wordp], idx.sc.NGramCount)
			if err != nil {
				return nil, nil, err
			}
			replacement := idx.sc.BestReplacement(words[wordp], conds)
			idx.logger.Write(logger.NewMessage(logger.INDEX_LAYER, logger.DEBUG, "word '%s' replaced with '%s' in query", words[wordp], replacement))
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
		if lemma.Type == textHandling.WORD {
			wordp++
		}
	}

	return stemmedTokens, reverthIndex, err
}

func calcSim(curSign [128]uint64, condidates [][128]uint64) int {
	wg := &sync.WaitGroup{}
	mu := &sync.Mutex{}
	result := 0
	l := len(condidates)
	for i := range l {
		wg.Add(1)
		sum := 0
		go func() {
			defer wg.Done()
			for j := range 128 {
				if curSign[j] == condidates[i][j] {
					sum++
				}
			}
			mu.Lock()
			result = max(result, sum)
			mu.Unlock()
		}()
	}
	wg.Wait()
	return result
}