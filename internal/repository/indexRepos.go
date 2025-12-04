package repository

import (
	"encoding/hex"
	"encoding/json"
	"strconv"
	"strings"
	"sync"

	"fmt"

	"github.com/box1bs/monocle/internal/model"
	"github.com/box1bs/monocle/pkg/logger"
	"github.com/dgraph-io/badger/v3"
)

type IndexRepository struct {
	DB 			*badger.DB
	log 		*logger.Logger
	wg 			*sync.WaitGroup
	mu 			*sync.Mutex
	wordBuffer	map[string][]string
	counts		map[string]int
}

func NewIndexRepository(path string, logger *logger.Logger) (*IndexRepository, error) {
	db, err := badger.Open(badger.DefaultOptions(path).WithLoggingLevel(badger.WARNING))
	if err != nil {
		return nil, err
	}
	return &IndexRepository{
		DB: db,
		log: logger,
		wg: new(sync.WaitGroup),
		mu: new(sync.Mutex),
		wordBuffer: make(map[string][]string),
		counts: make(map[string]int),
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
			depth, err := item.ValueCopy(nil)
			if err != nil {
				return err
			}
			d, err := strconv.Atoi(string(depth))
			if err != nil {
				return err
			}
            visitedURLs.Store(url, d)
        }
        return nil
    })
}

func (ir *IndexRepository) SaveVisitedUrls(visitedURLs *sync.Map) error {
	visitedURLs.Range(func(key, value any) bool {
		if url, ok := key.(string); ok {
			ir.DB.Update(func(txn *badger.Txn) error {
				return txn.Set([]byte("visited:" + url), fmt.Append(nil, value.(int)))
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

	ir.mu.Lock()
	defer ir.mu.Unlock()

	const maxWordsInTXN = 1000
	itNum := 0

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
		itNum++

		if itNum >= maxWordsInTXN {
			if err := wb.Flush(); err != nil {
				return err
			}
			itNum = 0
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