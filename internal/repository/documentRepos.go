package repository

import (
	"encoding/json"
	"errors"
	"fmt"

	"slices"

	"github.com/box1bs/Saturday/internal/model"
	"github.com/dgraph-io/badger/v3"
	"github.com/google/uuid"
)

const (
    DocumentKeyPrefix = "doc:"
    WordDocumentKeyFormat = "%d_%s"
)

func (ir *IndexRepository) documentToBytes(doc *model.Document) ([]byte, error) {
	return json.Marshal(doc)
}

func (ir *IndexRepository) bytesToDocument(body []byte) (*model.Document, error) {
	var payload struct {
		Id 				string 		`json:"id"`
		URL 			string 		`json:"url"`
		WordCount 		int 		`json:"words_count"`
		PartOfFullSize 	float64 	`json:"part_of_full_size"`
		Vec1 			[][]float64 `json:"word_vec"`
		Vec2 			[][]float64 `json:"title_vec"`
	}
	err := json.Unmarshal(body, &payload)
	if err != nil {
		return nil, err
	}
	id, err := uuid.Parse(payload.Id)
	if err != nil {
		return nil, err
	}
	doc := &model.Document{
		Id: id,
		URL: payload.URL,
		WordCount: payload.WordCount,
		PartOfFullSize: payload.PartOfFullSize,
		WordVec: payload.Vec1,
		TitleVec: payload.Vec2,
	}
	return doc, err
}

func createDocumentKey(docID uuid.UUID) []byte {
	return []byte(DocumentKeyPrefix + docID.String())
}

func (ir *IndexRepository) SaveDocument(doc *model.Document) error {
	docBytes, err := ir.documentToBytes(doc)
	if err != nil {
		return err
	}

	return ir.DB.Update(func(txn *badger.Txn) error {
		docKey := createDocumentKey(doc.Id)
		if err := txn.Set(docKey, docBytes); err != nil {
			return err
		}
		return nil
	})
}

func (ir *IndexRepository) GetDocumentByID(docID uuid.UUID) (*model.Document, error) {
	var docBytes []byte
	err := ir.DB.View(func(txn *badger.Txn) error {
		docKey := createDocumentKey(docID)
		item, err := txn.Get(docKey)
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

func (ir *IndexRepository) CheckContent(id uuid.UUID, hash [32]byte) (bool, *model.Document, error) {
	key := []byte(fmt.Sprintf("%s_%s", hash, id.String()))
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
			doc, err = ir.GetDocumentByID(uuid.MustParse(string(k[33:])))
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