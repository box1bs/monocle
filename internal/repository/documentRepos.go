package repository

import (
	"encoding/json"

	"github.com/box1bs/Saturday/internal/model"
	"github.com/dgraph-io/badger/v3"
	"github.com/google/uuid"
	"slices"
)

const (
    DocumentKeyPrefix = "doc:"
    WordKeyPrefix = "word:"
    IdKeyPrefix = "id:"
    WordDocumentKeyFormat = "%d_%s"
)

func (ir *IndexRepository) documentToBytes(doc *model.Document) ([]byte, error) {
	return json.Marshal(map[string]any{
		"id": doc.Id.String(),
		"url": doc.URL,
		"description": doc.Description,
		"words_count": doc.WordCount,
		"part_of_full_size": doc.PartOfFullSize,
	})
}

func (ir *IndexRepository) bytesToDocument(body []byte) (*model.Document, error) {
	data := make(map[string]any) 
	err := json.Unmarshal(body, &data)
	if err != nil {
		return nil, err
	}
	id, err := uuid.Parse(data["id"].(string))
	if err != nil {
		return nil, err
	}
	doc := &model.Document{
		Id: id,
		URL: data["url"].(string),
		Description: data["description"].(string),
		WordCount: data["words_count"].(int),
		PartOfFullSize: data["part_of_full_size"].(float64),
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
	
	err := ir.db.View(func(txn *badger.Txn) error {
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
	
	err := ir.db.View(func(txn *badger.Txn) error {
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