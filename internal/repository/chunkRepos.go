package repository

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/box1bs/monocle/pkg/logger"
	"github.com/dgraph-io/badger/v3"
)

const (
	chunkSize = 100
	ngKey = "ng:%s:%04d"
	shingleKey = "shingle:%s:%04d"
)

type wordChunkData struct {
	buffer	map[string][]string
	counts	map[string]int
}

type shingleChunkData struct {
	buffer	map[[4]uint64][][128]uint64
	counts 	map[[4]uint64]int
}

func (ir *IndexRepository) IndexNGrams(words []string, n int) error {
	for _, word := range words {
		for _, ng := range ir.extractNGrams(word, n) {
			ir.mu.Lock()
			buf := ir.nGramIndexer.buffer[ng]
			buf = append(buf, word)
			if len(buf) >= chunkSize {
				ir.nGramIndexer.counts[ng]++
				chId := ir.nGramIndexer.counts[ng]
				toFlush := make([]string, len(buf)) // чтоб не обнулялось
				copy(toFlush, buf)
				ir.nGramIndexer.buffer[ng] = buf[:0]
				ir.mu.Unlock()
				if err := ir.flushChunk(chId, ngKey, ng, toFlush); err != nil {
					return err
				}
				continue
			}
			ir.nGramIndexer.buffer[ng] = buf
			ir.mu.Unlock()
		}
	}
	return nil
}

func (ir *IndexRepository) IndexDocShingles(signature [128]uint64) error {
	for i := 0; i <= 128 - 4; i += 4 {
		var lshKey [4]uint64
        copy(lshKey[:], signature[i: i + 4])
		ir.mu.Lock()
		buf := append(ir.shingleIndexer.buffer[lshKey], signature)
		if len(buf) >= chunkSize {
			ir.shingleIndexer.counts[lshKey]++
			chId := ir.shingleIndexer.counts[lshKey]
			toFlush := make([][128]uint64, len(buf)) // чтоб не обнулялся
			copy(toFlush, buf)
			ir.shingleIndexer.buffer[lshKey] = buf[:0]
			ir.mu.Unlock()
			strs := [4]string{}
			for i := range 4 {
				strs[i] = strconv.FormatUint(lshKey[i], 10)
			}
			if err := ir.flushChunk(chId, shingleKey, strings.Join(strs[:], "."), toFlush); err != nil {
				return err
			}
			continue
		}
		ir.shingleIndexer.buffer[lshKey] = buf
		ir.mu.Unlock()
	}
	return nil
}

func (ir *IndexRepository) GetWordsByNGram(word string, n int) ([]string, error) {
	ir.mu.Lock()
	defer ir.mu.Unlock()

	result := []string{}
	alreadyInc := map[string]struct{}{}

	for _, ngram := range ir.extractNGrams(word, n) {
		for _, word := range ir.nGramIndexer.buffer[ngram] { // берем из буффера
			if _, ex := alreadyInc[word]; ex {
				continue
			}
			alreadyInc[word] = struct{}{}
			result = append(result, word)
		}
		prefix := []byte("ng:" + ngram + ":")
		if err := ir.DB.View(func(txn *badger.Txn) error { // берем из памяти, технически можно это делать не через итератор, а напрямую меняя ключ в цикле от 0 до count для этой нграммы, это позволило бы лучше обрабатывать ошибочные ситуации
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

func (ir *IndexRepository) GetSimilarSignatures(signature [128]uint64) ([][128]uint64, error) {
	result := [][128]uint64{}
	alreadyInc := map[[128]uint64]struct{}{}

	for i := 0; i <= 128 - 4; i += 4 {
		var lshKey [4]uint64
        copy(lshKey[:], signature[i: i + 4])
		ir.mu.Lock()
		buf := make([][128]uint64, len(ir.shingleIndexer.buffer[lshKey]))
        copy(buf, ir.shingleIndexer.buffer[lshKey])
		ir.mu.Unlock()
		for _, sign := range buf {
			if _, ex := alreadyInc[sign]; ex {
				continue
			}
			alreadyInc[sign] = struct{}{}
			result = append(result, sign)
		}
		strs := [4]string{}
		for i := range 4 {
			strs[i] = strconv.FormatUint(lshKey[i], 10)
		}
		prefix := []byte("ng:" + strings.Join(strs[:], ".") + ":")
		if err := ir.DB.View(func(txn *badger.Txn) error {
			it := txn.NewIterator(badger.DefaultIteratorOptions)
			defer it.Close()
			for it.Seek(prefix); it.ValidForPrefix(prefix); it.Next() {
				item := it.Item()
				val, err := item.ValueCopy(nil)
				if err != nil {
					return err
				}
				var signatures [][128]uint64
				if err := json.Unmarshal(val, &signatures); err != nil {
					return err
				}
				for _, sign := range signatures {
					if _, ex := alreadyInc[sign]; ex {
						continue
					}
					alreadyInc[sign] = struct{}{}
					result = append(result, sign)
				}
			}
			return nil
		}); err != nil {
			return nil, err
		}
	}

	return result, nil
}

func (ir *IndexRepository) flushChunk(id int, k, data string, buffer any) error {
	key := fmt.Appendf(nil, k, data, id)
	val, err := json.Marshal(buffer)
	if err != nil {
		return err
	}

	if err := ir.DB.Update(func(txn *badger.Txn) error {
		return txn.Set(key, val)
	}); err != nil {
		ir.log.Write(logger.NewMessage(logger.REPOSITORY_LAYER, logger.CRITICAL_ERROR, "error flushing chunk %s, with error %v", data, err))
		return err
	}
	return nil
}

func (ir *IndexRepository) FlushAll() {
	for ng, buf := range ir.nGramIndexer.buffer {
		if len(buf) == 0 {
			continue
		}
		ir.nGramIndexer.counts[ng]++
		ir.flushChunk(ir.nGramIndexer.counts[ng], ngKey, ng, buf)
	}
	for sh, buf := range ir.shingleIndexer.buffer {
		if len(buf) == 0 {
			continue
		}
		ir.shingleIndexer.counts[sh]++
		strs := [4]string{}
		for i := range 4 {
			strs[i] = strconv.Itoa(int(sh[i]))
		}
		ir.flushChunk(ir.shingleIndexer.counts[sh], shingleKey, strings.Join(strs[:], "."), buf)
	}
	ir.nGramIndexer.buffer = make(map[string][]string)
}

func (ir * IndexRepository) UpdateChunkingCounts() error {
	prefixN := []byte("ng:")
	if err := ir.DB.View(func(txn *badger.Txn) error {
		it := txn.NewIterator(badger.DefaultIteratorOptions)
		defer it.Close()
		for it.Seek(prefixN); it.ValidForPrefix(prefixN); it.Next() {
			item := it.Item()
			ngramButch := strings.Split(strings.TrimPrefix(string(item.Key()), string(prefixN)), ":")
			if len(ngramButch) < 2 {
				return fmt.Errorf("invalid data chunk")
			}
			lnum, err := strconv.Atoi(ngramButch[1])
			if err != nil {
				return err
			}
			ir.nGramIndexer.counts[ngramButch[0]] = max(ir.nGramIndexer.counts[ngramButch[0]], lnum)
		}
		return nil
	}); err != nil {
		return err
	}
	prefixS := []byte("shingle:")
	return ir.DB.View(func(txn *badger.Txn) error {
		it := txn.NewIterator(badger.DefaultIteratorOptions)
		defer it.Close()
		for it.Seek(prefixS); it.ValidForPrefix(prefixS); it.Next() {
			item := it.Item()
			shingleButch := strings.Split(strings.TrimPrefix(string(item.Key()), string(prefixS)), ":")
			if len(shingleButch) < 2 {
				return fmt.Errorf("invalid data chunk")
			}
			num, err := strconv.Atoi(shingleButch[1])
			if err != nil {
				return err
			}
			lshKey := [4]uint64{}
			rawKeys := strings.Split(shingleButch[0], ".")
			if len(rawKeys) != 4 {
				return fmt.Errorf("invalid key size")
			}
			for i := range 4 {
				mHash, err := strconv.Atoi(rawKeys[i])
				if err != nil {
					return err
				}
				lshKey[i] = uint64(mHash)
			}
			ir.shingleIndexer.counts[lshKey] = max(ir.shingleIndexer.counts[lshKey], num)
		}
		return nil
	})
}

func (ir *IndexRepository) SaveSaltArrays(a, b [128]uint64) error {
	abuf := bytes.NewBuffer(nil)
	if err := binary.Write(abuf, binary.LittleEndian, a); err != nil {
		return err
	}
	bbuf := bytes.NewBuffer(nil)
	if err := binary.Write(bbuf, binary.LittleEndian, b); err != nil {
		return err
	}
	return ir.DB.Update(func(txn *badger.Txn) error {
		return txn.Set(fmt.Appendf(nil, "salt:%s%s", abuf.Bytes(), bbuf.Bytes()), nil)
	})
}

func (ir *IndexRepository) UploadSaltArrays() (*[128]uint64, *[128]uint64, error) {
	a, b := &[128]uint64{}, &[128]uint64{}
	return a, b, ir.DB.View(func(txn *badger.Txn) error {
		iter := txn.NewIterator(badger.DefaultIteratorOptions)
		defer iter.Close()
		if iter.Seek([]byte("salt:")); iter.ValidForPrefix([]byte("salt:")) {
			key := iter.Item().Key()
			i := len("salt:")
			if l := 128 * 2 * 8 + i; len(key) != l {
				return fmt.Errorf("invalid key len: %d, needed L: %d", len(key), l)
			}
			partA := key[i:i + 128 * 8]
			partB := key[i + 128 * 8:]
			for i := range 128 {
				start := i * 8
				a[i] = binary.LittleEndian.Uint64(partA[start:start + 8])
				b[i] = binary.LittleEndian.Uint64(partB[start:start + 8])
			}
			return nil
		}
		a, b = nil, nil
		return nil
	})
}