package model

import (
	"context"
	"sync"
	"time"

	"github.com/google/uuid"
)

// Indexer defines the minimal interface required by the spider.
type Indexer interface {
    Write(string)
    HandleDocumentWords(string) ([]int, error)
    AddDocument(*Document, []int)
    IncUrlsCounter()
	GetContext() context.Context
}

type Logger interface {
	Write(string)
	Close()
}

type Vectorizer interface {
	Vectorize(string) ([]float64, error)
	SetContext(context.Context, time.Duration)
}

type Repository interface {
	LoadVisitedUrls(*sync.Map) error
	SaveVisitedUrls(*sync.Map) error
	IndexDocument(uuid.UUID, []int) error
	GetDocumentsByWord(int) (map[uuid.UUID]int, error)
	//DebugPrintKeys(string) error	//only for debug

	SaveDocument(doc *Document) error
	GetDocumentByID(uuid.UUID) (*Document, error)
	GetAllDocuments() ([]*Document, error)
	GetDocumentsCount() (int, error)

	TransferOrSaveToSequence([]string, bool) ([]int, error)
	SequenceToWords([]int) ([]string, error)
	GetLastId() (int, error)
	GetCurrentVocab() (map[int]string, error)
}

type Stemmer interface {
	Stem(string) string
}

type StopWords interface {
	IsStopWord(string) bool
}