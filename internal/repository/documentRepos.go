package repository

import (
	"encoding/json"
	"errors"
	"fmt"

	"slices"

	"github.com/box1bs/monocle/internal/model"
	"github.com/dgraph-io/badger/v3"
)

const (
	DocumentKeyPrefix     = "doc:%s"
	WordDocumentKeyFormat = "ri:%s_%x"
)

type payload struct {
	Id        []byte      `json:"id"`
	URL       string      `json:"url"`
	WordCount int         `json:"words_count"`
	Vec       [][]float64 `json:"word_vec"`
}

func (ir *IndexRepository) documentToBytes(doc *model.Document) ([]byte, error) {
	p := payload{
		Id:        doc.Id[:],
		URL:       doc.URL,
		WordCount: doc.WordCount,
		Vec:       doc.WordVec,
	}
	return json.Marshal(p)
}

func (ir *IndexRepository) bytesToDocument(body []byte) (*model.Document, error) {
	p := payload{}
	
	if err := json.Unmarshal(body, &p); err != nil {
		return nil, err
	}
	
	if len(p.Id) != 32 {
		return nil, fmt.Errorf("invalid id length: %d", len(p.Id))
	}
	
	var idArr [32]byte
	copy(idArr[:], p.Id)
	
	return &model.Document{
		Id:        idArr,
		URL:       p.URL,
		WordCount: p.WordCount,
		WordVec:   p.Vec,
	}, nil
}

func (ir *IndexRepository) SaveDocument(doc *model.Document) error {
	docBytes, err := ir.documentToBytes(doc)
	if err != nil {
		return err
	}
	return ir.DB.Update(func(txn *badger.Txn) error {
		if err := txn.Set(fmt.Appendf(nil, DocumentKeyPrefix, doc.Id[:]), docBytes); err != nil {
			return err
		}
		return nil
	})
}

func (ir *IndexRepository) GetDocumentByID(docID [32]byte) (*model.Document, error) {
	var docBytes []byte
	err := ir.DB.View(func(txn *badger.Txn) error {
		item, err := txn.Get(fmt.Appendf(nil, DocumentKeyPrefix, docID[:]))
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
		docPrefix := fmt.Appendf(nil, DocumentKeyPrefix, "")

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
	key := fmt.Appendf(nil, "%s/%s", hash, id)
	var (
		err error
		doc *model.Document
	)
	existError := "content already exists"

	err = ir.DB.Update(func(txn *badger.Txn) error {
		pref := fmt.Appendf(nil, "%s_", hash)

		opts := badger.DefaultIteratorOptions
		opts.PrefetchValues = false
		it := txn.NewIterator(opts)
		defer it.Close()

		if it.Seek(pref); it.ValidForPrefix(pref) {
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