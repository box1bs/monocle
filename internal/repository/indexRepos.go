package repository

import (
	"context"
	"encoding/json"
	"strconv"
	"strings"
	"sync"

	"fmt"

	"slices"

	"github.com/box1bs/Saturday/internal/model"
	"github.com/dgraph-io/badger/v3"
)

type IndexRepository struct {
	DB *badger.DB
	wg *sync.WaitGroup
}

func NewIndexRepository(path string) (*IndexRepository, error) {
	db, err := badger.Open(badger.DefaultOptions(path))
	if err != nil {
		return nil, err
	}
	return &IndexRepository{
		DB: db,
		wg: new(sync.WaitGroup),
	}, nil
}

func (ir *IndexRepository) LoadVisitedUrls(visitedURLs *sync.Map) error {
    opts := badger.DefaultIteratorOptions
    opts.Prefix = []byte("visited:")

    return ir.DB.View(func(txn *badger.Txn) error {
        it := txn.NewIterator(opts)
        defer it.Close()
        for it.Rewind(); it.Valid(); it.Next() {
            item := it.Item()
            key := string(item.Key())
            url := strings.TrimPrefix(key, "visited:")
            visitedURLs.Store(url, struct{}{})
        }
        return nil
    })
}

func (ir *IndexRepository) SaveVisitedUrls(visitedURLs *sync.Map) error {
	visitedURLs.Range(func(key, value any) bool {
		if url, ok := key.(string); ok {
			ir.DB.Update(func(txn *badger.Txn) error {
				return txn.Set([]byte("visited:"+url), []byte(""))
			})
		}
		return true
	})
	return nil
}

func (ir *IndexRepository) IndexDocumentWords(c context.Context, docID [32]byte, sequence []int, positions map[string][]model.Position) <- chan error {
	errCh := make(chan error)
	ir.wg.Add(1)
	go func() {
		defer ir.wg.Done()
		wordFreq := make(map[int]int)
		for _, word := range sequence {
			wordFreq[word]++
		}
		encoded, err := json.Marshal(positions)
		if err != nil {
			errCh <- err
			return
		}
		if err := ir.DB.Update(func(txn *badger.Txn) error {
			for word, freq := range wordFreq {
				select {
				case <- c.Done():
					return nil
				default:
				}
				key := fmt.Appendf(nil, WordDocumentKeyFormat, word, docID, freq)
				if err := txn.Set(key, encoded); err != nil {
					return err
				}
			}
			return nil
		}); err != nil {
			errCh <- err
		}
	}()

	go func() {
		ir.wg.Wait()
		close(errCh)
	}()

    return errCh
}

func (ir *IndexRepository) GetDocumentsByWord(word int) (map[[32]byte]*model.WordCountAndPositions, error) {
	revertWordIndex := make(map[[32]byte]*model.WordCountAndPositions)
	wprefix := fmt.Appendf(nil, "%d_", word)
	return revertWordIndex, ir.DB.View(func(txn *badger.Txn) error {
		it1 := txn.NewIterator(badger.DefaultIteratorOptions)
		defer it1.Close()
		errCh := make(chan error)
		ir.wg.Add(1)
		go func() {
			defer ir.wg.Done()
			for it1.Seek(wprefix); it1.ValidForPrefix(wprefix); it1.Next() {
				item := it1.Item()
				key := string(item.Key())
				keyPart := strings.TrimPrefix(key, string(wprefix))
				splited := strings.SplitN(keyPart, "_", 2)
				id := [32]byte([]byte(splited[0]))
				val, err := item.ValueCopy(nil)
				if err != nil {
					errCh <- err
					return
				}
				positions := []model.Position{}
				if err := json.Unmarshal(val, &positions); err != nil {
					errCh <- err
					return
				}
				freq, _ := strconv.Atoi(string(splited[1]))
				revertWordIndex[id] = &model.WordCountAndPositions{Count: freq, Positions: positions}
			}
		}()

		go func() {
			ir.wg.Wait()
			close(errCh)
		}()
		
		return <-errCh
	})
}

func (ir *IndexRepository) IndexNGrams(word string, nGrams ...string) error {
	err := ir.DB.Update(func(txn *badger.Txn) error {
		for _, nGram := range nGrams {
			key := fmt.Sprintf("ngram:%s", string(nGram))
			item, err := txn.Get([]byte(key))
			if err == badger.ErrKeyNotFound {
				if err = txn.Set([]byte(key), []byte(word)); err != nil {
					return err
				}
			} else if err != nil {
				return err
			}
			val, err := item.ValueCopy(nil)
			if err != nil {
				return err
			}
			words := strings.Split(string(val), ",")
			if slices.Contains(words, nGram) {
				continue
			}
			words = append(words, word)
			if err = txn.Set([]byte(key), []byte(strings.Join(words, ","))); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return err
	}

	return nil
}

func (ir *IndexRepository) GetWordsByNGrams(nGrams ...string) ([]string, error) {
	wordSet := make(map[string]struct{})
	err := ir.DB.View(func(txn *badger.Txn) error {
		for _, nGram := range nGrams {
			key := fmt.Sprintf("ngram:%s", nGram)
			item, err := txn.Get([]byte(key))
			if err == badger.ErrKeyNotFound {
				continue
			} else if err != nil {
				return err
			}
			val, err := item.ValueCopy(nil)
			if err != nil {
				return err
			}
			words := strings.SplitSeq(string(val), ",")
			for word := range words {
				if _, exists := wordSet[word]; exists {
					continue
				}
				wordSet[word] = struct{}{}
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	words := make([]string, 0, len(wordSet))
	for word := range wordSet {
		words = append(words, word)
	}
	return words, nil
}