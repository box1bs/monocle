package repository

import (
	"strconv"
	"strings"
	"sync"

	"github.com/dgraph-io/badger/v3"
	"github.com/google/uuid"
	"fmt"
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

func (ir *IndexRepository) IndexDocument(docID string, words []int) error {
	wordFreq := make(map[int]int)
	for _, word := range words {
		wordFreq[word]++
	}
	for word, freq := range wordFreq {
        key := []byte(fmt.Sprint(word) + "_" + docID)
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
	prefix := []byte(fmt.Sprint(word) + "_")
	return result, ir.db.View(func(txn *badger.Txn) error {
		it := txn.NewIterator(badger.DefaultIteratorOptions)
		defer it.Close()
		for it.Seek(prefix); it.ValidForPrefix(prefix); it.Next() {
            item := it.Item()
            key := string(item.Key())
            docID := strings.TrimPrefix(key, fmt.Sprint(word) + "_")
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