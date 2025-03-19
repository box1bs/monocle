package repository

import (
	"encoding/json"

	"github.com/box1bs/Saturday/internal/model"
	"github.com/dgraph-io/badger/v3"
	"github.com/google/uuid"
)

func (ir *IndexRepository) documentToBytes(doc *model.Document) ([]byte, error) {
	return json.Marshal(doc)
}

func (ir *IndexRepository) bytesToDocument(data []byte) (*model.Document, error) {
	doc := new(model.Document)
	err := json.Unmarshal(data, doc)
	return doc, err
}

func createDocumentKey(docID uuid.UUID) []byte {
	return []byte("doc:" + docID.String())
}

func (ir *IndexRepository) SaveDocument(doc *model.Document) error {
	docBytes, err := ir.documentToBytes(doc)
	if err != nil {
		return err
	}

	return ir.db.Update(func(txn *badger.Txn) error {
		docKey := createDocumentKey(doc.Id)
		if err := txn.Set(docKey, docBytes); err != nil {
			return err
		}
		return nil
	})
}

func (ir *IndexRepository) GetDocumentByID(docID uuid.UUID) (*model.Document, error) {
	var docBytes []byte
	err := ir.db.View(func(txn *badger.Txn) error {
		docKey := createDocumentKey(docID)
		item, err := txn.Get(docKey)
		if err != nil {
			return err
		}

		return item.Value(func(val []byte) error {
			docBytes = append([]byte{}, val...)
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
	
	err := ir.db.View(func(txn *badger.Txn) error {
		docPrefix := []byte("doc:")
		
		opts := badger.DefaultIteratorOptions
		opts.PrefetchSize = 10
		it := txn.NewIterator(opts)
		defer it.Close()
		
		for it.Seek(docPrefix); it.ValidForPrefix(docPrefix); it.Next() {
			item := it.Item()
			var docBytes []byte
			
			err := item.Value(func(val []byte) error {
				docBytes = append([]byte{}, val...)
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
	
	err := ir.db.View(func(txn *badger.Txn) error {
		docPrefix := []byte("doc:")
		
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