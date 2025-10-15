package repository

import (
	"encoding/json"
	"strconv"
	"strings"
	"sync"

	"fmt"

	"slices"

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

const (
	bgMapKey = "%s:%s:"
	ugMapKey = "%s:"
)

func (ir *IndexRepository) GetProbByWord(word ...string) (int, error) {
	txn := ir.DB.NewTransaction(false)
	key := []byte(nil)
	switch len(word) {
	case 1:
		key = fmt.Appendf(nil, ugMapKey, word[0])

	case 2:
		key = fmt.Appendf(nil, bgMapKey, word[0], word[1])

	default:
		return -1, fmt.Errorf("we dont do this here")
	}
	it, err := txn.Get(key)
	if err != nil {
		return -1, err
	}

	count, err := it.ValueCopy(nil)
	if err != nil {
		return -1, err
	}

	return strconv.Atoi(string(count))
}

func (ir *IndexRepository) SaveUnigramProb(in map[string]int) error {
	txn := ir.DB.NewTransaction(true)
	count := 0

	for k, c := range in {
			it, err := txn.Get(fmt.Appendf(nil, ugMapKey, k))
			if err != nil {
				if err == badger.ErrKeyNotFound {
					if err := txn.Set(fmt.Appendf(nil, ugMapKey, k), fmt.Append(nil, c)); err != nil {
						return err
					}
					count++
					if count >= batchSize {
						if err := txn.Commit(); err != nil {
							return err
						}
						txn = ir.DB.NewTransaction(true)
						count = 0
					}
				} else {
					return err
				}
			} else {
				cnt, err := it.ValueCopy(nil)
				if err != nil {
					return err
				}
				counter, err := strconv.Atoi(string(cnt))
				if err != nil {
					return err
				}
				if err := txn.Set(fmt.Appendf(nil, ugMapKey, k), fmt.Append(nil, c + counter)); err != nil {
					return err
				}
				count++
				if count >= batchSize {
					if err := txn.Commit(); err != nil {
						return err
					}
					txn = ir.DB.NewTransaction(true)
					count = 0
				}
			}
	}
	return txn.Commit()
}

func (ir *IndexRepository) SaveBigramProb(in map[string]map[string]int) error {
	txn := ir.DB.NewTransaction(true)
	count := 0

	for k1, v := range in {
		for k2, c := range v {
			it, err := txn.Get(fmt.Appendf(nil, bgMapKey, k1, k2))
			if err != nil {
				if err == badger.ErrKeyNotFound {
					if err := txn.Set(fmt.Appendf(nil, bgMapKey, k1, k2), fmt.Append(nil, c)); err != nil {
						return err
					}
					count++
					if count >= batchSize {
						if err := txn.Commit(); err != nil {
							return err
						}
						txn = ir.DB.NewTransaction(true)
						count = 0
					}
				} else {
					return err
				}
			} else {
				cnt, err := it.ValueCopy(nil)
				if err != nil {
					return err
				}
				counter, err := strconv.Atoi(string(cnt))
				if err != nil {
					return err
				}
				if err := txn.Set(fmt.Appendf(nil, bgMapKey, k1, k2), fmt.Append(nil, c + counter)); err != nil {
					return err
				}
				count++
				if count >= batchSize {
					if err := txn.Commit(); err != nil {
						return err
					}
					txn = ir.DB.NewTransaction(true)
					count = 0
				}
			}
		}
	}
	return txn.Commit()
}

func (ir *IndexRepository) IndexDocumentWords(docID [32]byte, sequence []int, positions map[string][]model.Position) error {
	wordFreq := make(map[int]int)
	for _, word := range sequence {
		wordFreq[word]++
	}
	encoded, err := json.Marshal(positions)
	if err != nil {
		return err
	}

	const batch = 50

	current := 0
	txn := ir.DB.NewTransaction(true)

	for word, freq := range wordFreq {
		key := fmt.Appendf(nil, WordDocumentKeyFormat, word, docID[:], freq)
		if err := txn.Set(key, encoded); err != nil {
			return err
		}

		current++
		if current >= batch {
			if err := txn.Commit(); err != nil {
				return err
			}
			current = 0
			txn = ir.DB.NewTransaction(true)
		}
	}
	return txn.Commit()
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
				if len(splited[0]) != 32 {
					errCh <- fmt.Errorf("invalid id size: %s", splited[0])
					return
				}
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
	current := 0

	txn := ir.DB.NewTransaction(true)
	defer txn.Discard()

	for _, nGram := range nGrams {
		key := fmt.Appendf(nil, "ngram:%s", string(nGram))
		item, err := txn.Get(key)

		if err == badger.ErrKeyNotFound {
			if err = txn.Set(key, []byte(word)); err != nil {
				return err
			}
			continue
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
		byteVal := []byte(strings.Join(words, ","))
		if err = txn.Set(key, byteVal); err != nil {
			return err
		}

		current++
		if current >= batchSize {
			if err := txn.Commit(); err != nil {
				return err
			}
			txn = ir.DB.NewTransaction(true)
			current = 0
		}
	}

	return txn.Commit()
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