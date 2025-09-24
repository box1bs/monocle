package repository

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"slices"

	"github.com/box1bs/monocle/internal/model"
	"github.com/dgraph-io/badger/v3"
)

const (
    DocumentKeyPrefix = "doc:"
    WordDocumentKeyFormat = "%d_%s_%d"
)

func (ir *IndexRepository) documentToBytes(doc *model.Document) ([]byte, error) {
	return json.Marshal(doc)
}

func (ir *IndexRepository) bytesToDocument(body []byte) (*model.Document, error) {
	var payload struct {
		Id 				[]byte 		`json:"id"`
		URL 			string 		`json:"url"`
		WordCount 		int 		`json:"words_count"`
		Vec				[][]float64 `json:"word_vec"`
	}
	err := json.Unmarshal(body, &payload)
	if err != nil {
		return nil, err
	}
	b := [32]byte{}
	for i := range 32 {
		b[i] = payload.Id[i]
	}
	doc := &model.Document{
		Id: b,
		URL: payload.URL,
		WordCount: payload.WordCount,
		WordVec: payload.Vec,
	}
	return doc, err
}

func (ir *IndexRepository) SaveDocument(c context.Context, doc *model.Document) <- chan error {
	errCh := make(chan error)

	go func() {
		docBytes, err := ir.documentToBytes(doc)
		if err != nil {
			errCh <- err
		}

		errCh <- ir.DB.Update(func(txn *badger.Txn) error {
			select {
			case <- c.Done():
				return nil
			default:
			}
			if err := txn.Set([]byte("doc:" + string(doc.Id[:])), docBytes); err != nil {
				return err
			}
			return nil
		})
	}()

	return errCh
}

func (ir *IndexRepository) GetDocumentByID(docID [32]byte) (*model.Document, error) {
	var docBytes []byte
	err := ir.DB.View(func(txn *badger.Txn) error {
		item, err := txn.Get([]byte("doc:" + string(docID[:])))
		if err != nil {
			return err
		}

		return item.Value(func(val []byte) error {
			docBytes = slices.Clone(val)
			return nil
		})
	})

	if err != nil {
		return nil, err
	}

	return ir.bytesToDocument(docBytes)
}

func (ir *IndexRepository) GetAllDocuments() ([]*model.Document, error) {
	var documents []*model.Document
	
	err := ir.DB.View(func(txn *badger.Txn) error {
		opts := badger.DefaultIteratorOptions
		opts.PrefetchSize = 10
		it := txn.NewIterator(opts)
		defer it.Close()
		
		for it.Seek([]byte(DocumentKeyPrefix)); it.ValidForPrefix([]byte(DocumentKeyPrefix)); it.Next() {
			item := it.Item()
			var docBytes []byte
			
			err := item.Value(func(val []byte) error {
				docBytes = slices.Clone(val)
				return nil
			})
			
			if err != nil {
				return err
			}
			
			doc, err := ir.bytesToDocument(docBytes)
			if err != nil {
				return err
			}
			
			documents = append(documents, doc)
		}
		
		return nil
	})
	
	if err != nil {
		return nil, err
	}
	
	return documents, nil
}

func (ir *IndexRepository) GetDocumentsCount() (int, error) {
	var count int
	
	err := ir.DB.View(func(txn *badger.Txn) error {
		docPrefix := []byte(DocumentKeyPrefix)
		
		opts := badger.DefaultIteratorOptions
		opts.PrefetchValues = false
		it := txn.NewIterator(opts)
		defer it.Close()
		
		for it.Seek(docPrefix); it.ValidForPrefix(docPrefix); it.Next() {
			count++
		}
		
		return nil
	})
	
	if err != nil {
		return 0, err
	}
	
	return count, nil
}

func (ir *IndexRepository) CheckContent(id [32]byte, hash [32]byte) (bool, *model.Document, error) {
	key := []byte(fmt.Sprintf("%s_%s", hash, id))
	var (
		err error
		doc *model.Document
	)
	existError := "content already exists"

	err = ir.DB.Update(func(txn *badger.Txn) error {
		pref := []byte(fmt.Sprintf("%s_", hash))

		opts := badger.DefaultIteratorOptions
		opts.PrefetchValues = false
		it := txn.NewIterator(opts)
		defer it.Close()

		if it.Seek(pref); it.ValidForPrefix(pref){
			item := it.Item()
			k := item.Key()
			doc, err = ir.GetDocumentByID([32]byte(k[33:]))
			if err != nil {
				return err
			}

			return errors.New(existError)
		}

		if err := txn.Set(key, []byte{}); err != nil {
			return err
		}

		return nil
	})

	if err != nil {
		if err.Error() == existError {
			return true, doc, nil
		}
	}
	return false, nil, err
}

const (
	hashWordKey = "hash:%s"
	docHashesKey = "docHashes:%s"
)

func (ir *IndexRepository) IndexDocHashes(c context.Context, id [32]byte, hashes [][32]byte) <- chan error {
	tCh := make(chan error)
	go func() {
		if err := ir.DB.Update(func(txn *badger.Txn) error {
			for _, hash := range hashes {
				select {
				case <- c.Done():
					return nil
				default:
				}

				key := fmt.Sprintf(hashWordKey, hash)
				item, err := txn.Get([]byte(key))
				if err == badger.ErrKeyNotFound {
					if err = txn.Set([]byte(key), id[:]); err != nil {
						return err
					}
				} else if err != nil {
					return err
				}
				val, err := item.ValueCopy(nil)
				if err != nil {
					return err
				}
				docs := strings.Split(string(val), ",")
				slices.Contains(docs, string(hash[:]))
				docs = append(docs, string(hash[:]))
				if err = txn.Set([]byte(key), []byte(strings.Join(docs, ","))); err != nil {
					return err
				}
			}
			return nil
		}); err != nil {
			tCh <- err
		}
	}()

	errCh := make(chan error, 2)
	go func() {
		defer close(tCh)
		if err := <- tCh; err != nil {
			errCh <- err
		}
	}()

	if err := ir.DB.Update(func(txn *badger.Txn) error {
		key := fmt.Appendf(nil, docHashesKey, id)
		_, err := txn.Get(key)
		if err == badger.ErrKeyNotFound {
			save := []string{}
			for _, hashPart := range hashes {
				save = append(save, string(hashPart[:]))
			}
			if err = txn.Set(key, []byte(strings.Join(save, ","))); err != nil {
				return err
			}
		} else if err != nil {
			return err
		}
		return nil
	}); err != nil {
		errCh <- err
	}

	return errCh
}

func (ir *IndexRepository) GetDocumentsByHash(hashes [][32]byte) ([][][32]byte, error) {
	ids := [][32]byte{}
	if err := ir.DB.View(func(txn *badger.Txn) error {
		for _, hash := range hashes {
			key := fmt.Sprintf(hashWordKey, hash)
			item, err := txn.Get([]byte(key))
			if err == badger.ErrKeyNotFound {
				return nil
			} else if err != nil {
				return err
			}
			val, err := item.ValueCopy(nil)
			if err != nil {
				return err
			}
			for id := range strings.SplitSeq(string(val), ",") {
				ids = append(ids, [32]byte([]byte(id)))
			}
		}
		return nil
	}); err != nil {
		return nil, err
	}

	similarHashes := [][][32]byte{}
	if err := ir.DB.View(func(txn *badger.Txn) error {
		for _, id := range ids {
			key := fmt.Sprintf(docHashesKey, id)
			item, err := txn.Get([]byte(key))
			if err != nil {
				if err == badger.ErrKeyNotFound {
					continue
				}
				return err
			}
			val, err := item.ValueCopy(nil)
			if err != nil {
				return err
			}
			hash := [][32]byte{}
			for hashes := range strings.SplitSeq(string(val), ",") {
				hash = append(hash, [32]byte([]byte(hashes)))
			}
			if len(hash) > 0 {
				similarHashes = append(similarHashes, hash)
			}
		}
		return nil
	}); err != nil {
		return nil, err
	}
	return similarHashes, nil
}