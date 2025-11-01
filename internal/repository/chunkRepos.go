package repository

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/box1bs/monocle/pkg/logger"
	"github.com/dgraph-io/badger/v3"
)

const chunkSize = 100

func (ir *IndexRepository) IndexNGrams(words []string, n int) error {
	for _, word := range words {
		for _, ng := range ir.extractNGrams(word, n) {
			buf := ir.wordBuffer[ng]
			buf = append(buf, word)
			if len(buf) >= chunkSize {
				if err := ir.flushChunk(ng, buf); err != nil {
					return err
				}
				buf = buf[:0]
			}
			ir.wordBuffer[ng] = buf
		}
	}
	return nil
}

func (ir *IndexRepository) GetWordsByNGram(word string, n int) ([]string, error) {
	result := []string{}
	alreadyInc := map[string]struct{}{}

	for _, ngram := range ir.extractNGrams(word, n) {
		prefix := []byte("ng:" + ngram + ":")
		if err := ir.DB.View(func(txn *badger.Txn) error {
			it := txn.NewIterator(badger.DefaultIteratorOptions)
			defer it.Close()
			for it.Seek(prefix); it.ValidForPrefix(prefix); it.Next() {
				item := it.Item()
				val, err := item.ValueCopy(nil)
				if err != nil {
					return err
				}
				var words []string
				if err := json.Unmarshal(val, &words); err != nil {
					return err
				}
				for _, w := range words {
					if _, ex := alreadyInc[w]; ex {
						continue
					}
					alreadyInc[w] = struct{}{}
					result = append(result, w)
				}
			}
			return nil
		}); err != nil {
			return nil, err
		}
	}

	return result, nil
}

func (ir *IndexRepository) extractNGrams(word string, n int) []string {
	runes := []rune(strings.ToLower(word))
	out := []string{}
	alIn := map[string]struct{}{}
	if len(runes) < n {
		return nil
	}
	for i := range len(runes) - n + 1 {
		ng := string(runes[i:i + n])
		if _, ex := alIn[ng]; ex {
			continue
		}
		alIn[ng] = struct{}{}
		out = append(out, ng)
	}
	return out
}

func (ir *IndexRepository) flushChunk(ng string, buffer []string) error {
	ir.mu.Lock()
	defer ir.mu.Unlock()

	ir.counts[ng]++
	chunkID := ir.counts[ng]

	key := fmt.Appendf(nil, "ng:%s:%04d", ng, chunkID)
	val, _ := json.Marshal(buffer)

	if err := ir.DB.Update(func(txn *badger.Txn) error {
		return txn.Set(key, val)
	}); err != nil {
		ir.log.Write(logger.NewMessage(logger.REPOSITORY_LAYER, logger.CRITICAL_ERROR, "error flushing chunk %s, with error %v", ng, err))
		return err
	}
	return nil
}

func (ir *IndexRepository) FlushAll() {
	for ng, buf := range ir.wordBuffer {
		if len(buf) == 0 {
			continue
		}
		ir.flushChunk(ng, buf)
	}
	ir.wordBuffer = make(map[string][]string)
}