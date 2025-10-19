package repository

import (
	"encoding/hex"
	"encoding/json"
	"strings"
	"sync"

	"fmt"

	"github.com/box1bs/monocle/internal/model"
	"github.com/box1bs/monocle/pkg/logger"
	"github.com/dgraph-io/badger/v3"
)

const perEntryOverhead = 64

type IndexRepository struct {
	DB 			*badger.DB
	log 		*logger.Logger
	wg 			*sync.WaitGroup
	wordBuffer	map[string][]string
	counts		map[string]int
	maxTxnBytes int
}

func NewIndexRepository(path string, logger *logger.Logger, maxTransactionBytes int) (*IndexRepository, error) {
	db, err := badger.Open(badger.DefaultOptions(path).WithLoggingLevel(badger.WARNING))
	if err != nil {
		return nil, err
	}
	return &IndexRepository{
		DB: db,
		log: logger,
		wg: new(sync.WaitGroup),
		wordBuffer: make(map[string][]string),
		counts: make(map[string]int),
		maxTxnBytes: maxTransactionBytes,
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
				return txn.Set([]byte("visited:" + url), []byte(""))
			})
		}
		return true
	})
	return nil
}

func (ir *IndexRepository) SavePageRank(numOfUrlEntries map[string]float64) error {
	return ir.DB.Update(func(txn *badger.Txn) error {
		data, err := json.Marshal(numOfUrlEntries)
		if err != nil {
			return err
		}
		return txn.Set([]byte("pagerank:"), data)
	})
}

func (ir *IndexRepository) LoadPageRank() (map[string]float64, error) {
	out := map[string]float64{}
	return out, ir.DB.View(func(txn *badger.Txn) error {
		it := txn.NewIterator(badger.DefaultIteratorOptions)
		defer it.Close()
		if it.ValidForPrefix([]byte("pagerank:")) {
			val, err := it.Item().ValueCopy(nil)
			if err != nil {
				return err
			}
			return json.Unmarshal(val, &out)
		}
		return nil
	})
}

func (ir *IndexRepository) IndexDocumentWords(docID [32]byte, sequence map[string]int, pos map[string][]model.Position) error {
	wb := ir.DB.NewWriteBatch()
	defer wb.Cancel()

	curBytes := 0
	var flushTreshold = ir.maxTxnBytes / 8

	for word, freq := range sequence {
		key := fmt.Appendf(nil, WordDocumentKeyFormat, word, docID)
        wcp := model.WordCountAndPositions{
            Count:     freq,
            Positions: pos[word],
        }

		val, err := json.Marshal(wcp)
		if err != nil {
			return err
		}
		
		if err := wb.Set(key, val); err != nil {
			return err
		}

		curBytes += len(key) + len(val) + perEntryOverhead
		if curBytes >= flushTreshold {
			if err := wb.Flush(); err != nil {
				return err
			}
			curBytes = 0
		}
	}
	return wb.Flush()
}

func (ir *IndexRepository) GetDocumentsByWord(word string) (map[[32]byte]model.WordCountAndPositions, error) {
	revertWordIndex := make(map[[32]byte]model.WordCountAndPositions)
	wprefix := fmt.Appendf(nil, "ri:%s_", word)
	return revertWordIndex, ir.DB.View(func(txn *badger.Txn) error {
		it := txn.NewIterator(badger.DefaultIteratorOptions)
		defer it.Close()
		for it.Seek(wprefix); it.ValidForPrefix(wprefix); it.Next() {
			item := it.Item()
			keyPart := item.Key()[len(wprefix):]

			decoded, err := hex.DecodeString(string(keyPart))
			if err != nil {
				return err
			}
			id := [32]byte{}
			copy(id[:], decoded)

			val, err := item.ValueCopy(nil)
			if err != nil {
				return err
			}
			positions := model.WordCountAndPositions{}
			if err := json.Unmarshal(val, &positions); err != nil {
				return err
			}

			revertWordIndex[id] = positions
		}
		
		return nil
	})
}