package model

import (
	"context"
	"sync"

	"github.com/google/uuid"
)

// Indexer defines the minimal interface required by the spider.
type Indexer interface {
    Write(string)
    HandleDocumentWords(string) ([]int, error)
    AddDocument(*Document)
    IncUrlsCounter()
	GetContext() context.Context
}

type Logger interface {
	Write(string)
	Close() error
}

type Repository interface {
	LoadVisitedUrls(*sync.Map) error
	SaveVisitedUrls(*sync.Map) error
	IndexDocument(string, []int) error
	GetDocumentsByWord(int) (map[uuid.UUID]int, error)

	SaveDocument(doc *Document) error
	GetDocumentByID(uuid.UUID) (*Document, error)
	GetAllDocuments() ([]*Document, error)
	GetDocumentsCount() (int, error)

	TransferOrSaveToSequence([]string) ([]int, error)
	SequenceToWords([]int) ([]string, error)
	GetLastId() (int, error)
}

type Stemmer interface {
	Stem(string) string
}

type StopWords interface {
	IsStopWord(string) bool
}