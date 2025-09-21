package repository

import (
	"strconv"
	"strings"
	"sync"

	"fmt"

	"github.com/dgraph-io/badger/v3"
	"github.com/google/uuid"
	"slices"
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

func (ir *IndexRepository) IndexDocumentWords(docID uuid.UUID, words, titleWords []int) error {
	errCh := make(chan error)
	ir.wg.Add(1)
	go func() {
		defer ir.wg.Done()
		wordFreq := make(map[int]int)
		mp := map[int][]int{}
		for i, word := range words {
			wordFreq[word]++
			if mp[word] == nil {
				mp[word] = make([]int, 0)
			}
			mp[word] = append(mp[word], i) // сделать функцию сохранения
		}
		if err := ir.DB.Update(func(txn *badger.Txn) error {
			for word, freq := range wordFreq {
				key := fmt.Appendf(nil, WordDocumentKeyFormat, word, docID.String())
				if err := txn.Set(key, []byte(strconv.Itoa(freq))); err != nil {
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

	headerFreq := make(map[int]int)
	for _, word := range titleWords {
		headerFreq[word]++
	}
	if err := ir.DB.Update(func(txn *badger.Txn) error {
		for word, freq := range headerFreq {
			key := fmt.Appendf(nil, "%d:%s", word, docID.String())
			if err := txn.Set(key, []byte(strconv.Itoa(freq))); err != nil {
				return err
			}
		}
		return nil
    }); err != nil {
		return err
	}

    return <-errCh
}

func (ir *IndexRepository) GetDocumentsByWord(word int) (map[uuid.UUID]int, map[uuid.UUID]struct{}, error) {
	revertWordIndex := make(map[uuid.UUID]int)
	hasWordInHeader := make(map[uuid.UUID]struct{})
	wprefix := fmt.Appendf(nil, "%d_", word)
	hprefix := fmt.Appendf(nil, "%d:", word)
	return revertWordIndex, hasWordInHeader, ir.DB.View(func(txn *badger.Txn) error {
		it1 := txn.NewIterator(badger.DefaultIteratorOptions)
		defer it1.Close()
		errCh := make(chan error)
		ir.wg.Add(1)
		go func() {
			defer ir.wg.Done()
			for it1.Seek(wprefix); it1.ValidForPrefix(wprefix); it1.Next() {
				item := it1.Item()
				key := string(item.Key())
				docID := strings.TrimPrefix(key, string(wprefix))
				id, err := uuid.Parse(docID)
				if err != nil {
					errCh <- err
					return
				}
				val, err := item.ValueCopy(nil)
				if err != nil {
					errCh <- err
					return
				}
				freq, _ := strconv.Atoi(string(val))
				revertWordIndex[id] += freq
			}
		}()

		go func() {
			ir.wg.Wait()
			close(errCh)
		}()
		
		it2 := txn.NewIterator(badger.DefaultIteratorOptions)
		defer it2.Close()
		for it2.Seek(hprefix); it2.ValidForPrefix(hprefix); it2.Next() {
			item := it2.Item()
			key := string(item.Key())
			docID := strings.TrimPrefix(key, string(hprefix))
			id, err := uuid.Parse(docID)
			if err != nil {
				return err
			}
			val, err := item.ValueCopy(nil)
			if err != nil {
				return err
			}
			freq, _ := strconv.Atoi(string(val))
			revertWordIndex[id] += freq
			hasWordInHeader[id] = struct{}{}
		}
		return <-errCh
	})
}

func (ir *IndexRepository) IndexNGrams(nGrams ...string) error {
	err := ir.DB.Update(func(txn *badger.Txn) error {
		for _, nGram := range nGrams {
			key := fmt.Sprintf("ngram:%s", string(nGram))
			item, err := txn.Get([]byte(key))
			if err == badger.ErrKeyNotFound {
				if err = txn.Set([]byte(key), []byte(nGram)); err != nil {
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
			words = append(words, nGram)
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