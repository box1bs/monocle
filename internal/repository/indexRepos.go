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
	db *badger.DB
}

func NewIndexRepository(db *badger.DB) *IndexRepository {
	return &IndexRepository{db: db}
}

func (ir *IndexRepository) LoadVisitedUrls(visitedURLs *sync.Map) error {
    opts := badger.DefaultIteratorOptions
    opts.Prefix = []byte("visited:")

    return ir.db.View(func(txn *badger.Txn) error {
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
			ir.db.Update(func(txn *badger.Txn) error {
				return txn.Set([]byte("visited:"+url), []byte(""))
			})
		}
		return true
	})
	return nil
}

func (ir *IndexRepository) IndexDocument(docID uuid.UUID, words []int) error {
	wordFreq := make(map[int]int)
	for _, word := range words {
		wordFreq[word]++
	}
	for word, freq := range wordFreq {
        key := fmt.Appendf(nil, WordDocumentKeyFormat, word, docID.String())
        if err := ir.db.Update(func(txn *badger.Txn) error {
            return txn.Set(key, []byte(strconv.Itoa(freq)))
        }); err != nil {
			return err
		}
    }
    return nil
}

func (ir *IndexRepository) GetDocumentsByWord(word int) (map[uuid.UUID]int, error) {
	result := make(map[uuid.UUID]int)
	prefix := fmt.Appendf(nil, "%d_", word)
	return result, ir.db.View(func(txn *badger.Txn) error {
		it := txn.NewIterator(badger.DefaultIteratorOptions)
		defer it.Close()
		for it.Seek(prefix); it.ValidForPrefix(prefix); it.Next() {
            item := it.Item()
            key := string(item.Key())
            docID := strings.TrimPrefix(key, fmt.Sprintf("%d_", word))
			id, err := uuid.Parse(docID)
			if err != nil {
				return err
			}
            val, err := item.ValueCopy(nil)
            if err != nil {
                return err
            }
            freq, _ := strconv.Atoi(string(val))
            result[id] = freq
        }
		return nil
	})
}

func (ir *IndexRepository) IndexNGrams(nGrams ...string) error {
	for _, nGram := range nGrams {
		key := fmt.Sprintf("ngram:%s", string(nGram))
		err := ir.db.Update(func(txn *badger.Txn) error {
			item, err := txn.Get([]byte(key))
			if err == badger.ErrKeyNotFound {
				return txn.Set([]byte(key), []byte(nGram))
			} else if err != nil {
				return err
			}
			val, err := item.ValueCopy(nil)
			if err != nil {
				return err
			}
			words := strings.Split(string(val), ",")
			if slices.Contains(words, nGram) {
				return nil
			}
			words = append(words, nGram)
			return txn.Set([]byte(key), []byte(strings.Join(words, ",")))
		})
		if err != nil {
			return err
		}
	}

	return nil
}

func (ir *IndexRepository) GetWordsByNGrams(nGrams ...string) ([]string, error) {
	wordSet := make(map[string]struct{})
	err := ir.db.View(func(txn *badger.Txn) error {
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