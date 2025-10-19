package repository

import (
	"encoding/hex"
	"encoding/json"
	"strings"
	"sync"

	"fmt"

	"github.com/box1bs/monocle/internal/model"
	"github.com/dgraph-io/badger/v3"
)

const batchSize = 200

type IndexRepository struct {
	DB 		*badger.DB
	wg 		*sync.WaitGroup
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
	current := 0
	txn := ir.DB.NewTransaction(true)

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
		if err := txn.Set(key, val); err != nil {
			return err
		}

		current++
		if current >= batchSize {
			if err := txn.Commit(); err != nil {
				return err
			}
			current = 0
			txn = ir.DB.NewTransaction(true)
		}
	}
	return txn.Commit()
}

func (ir *IndexRepository) GetDocumentsByWord(word string) (map[[32]byte]model.WordCountAndPositions, error) {
	revertWordIndex := make(map[[32]byte]model.WordCountAndPositions)
	wprefix := fmt.Appendf(nil, "ri:%s_", word)
	return revertWordIndex, ir.DB.View(func(txn *badger.Txn) error {
		it := txn.NewIterator(badger.DefaultIteratorOptions)
		defer it.Close()
		errCh := make(chan error)
		ir.wg.Add(1)
		go func() {
			defer ir.wg.Done()
			for it.Seek(wprefix); it.ValidForPrefix(wprefix); it.Next() {
				item := it.Item()

				keyPart := item.Key()[len(wprefix):]
				decoded, err := hex.DecodeString(string(keyPart))
				if err != nil {
					errCh <- err
					return
				}

				id := [32]byte{}
				copy(id[:], decoded)

				val, err := item.ValueCopy(nil)
				if err != nil {
					errCh <- err
					return
				}
				positions := model.WordCountAndPositions{}
				if err := json.Unmarshal(val, &positions); err != nil {
					errCh <- err
					return
				}
				revertWordIndex[id] = positions
			}
		}()

		go func() {
			ir.wg.Wait()
			close(errCh)
		}()
		
		return <-errCh
	})
}

func (ir *IndexRepository) GetAllWords() []string {
	dictionary := []string{}
	wpref := "ri:"
	alreadyIncluded := map[string]struct{}{}
	it := ir.DB.NewTransaction(false).NewIterator(badger.DefaultIteratorOptions)
	for it.Seek([]byte(wpref)); it.ValidForPrefix([]byte(wpref)); it.Next() {
		word := strings.SplitN(strings.TrimPrefix(string(it.Item().Key()), wpref), "_", 1)[0]
		if _, ex := alreadyIncluded[word]; ex {
			continue
		}
		alreadyIncluded[word] = struct{}{}
		dictionary = append(dictionary, word)
	}
	return dictionary
}