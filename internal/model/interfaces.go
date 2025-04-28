package model

import (
	"context"
	"encoding/pem"
	"net/http"
	"sync"

	"github.com/box1bs/Saturday/configs"
	"github.com/google/uuid"
)

// Indexer defines the minimal interface required by the spider.
type Indexer interface {
    Write(string)
    HandleDocumentWords(string) ([]int, error)
    AddDocument(*Document, []int)
    IncUrlsCounter()
	Index(*configs.ConfigData, context.Context) error
	GetCurrentUrlsCrawled() int32
	Search(string, float64, int) []*Document
}

type Logger interface {
	Write(string)
	Close()
}

type Vectorizer interface {
	Vectorize(string, context.Context) ([][]float64, error)
}

type Repository interface {
	LoadVisitedUrls(*sync.Map) error
	SaveVisitedUrls(*sync.Map) error
	IndexDocument(uuid.UUID, []int) error
	GetDocumentsByWord(int) (map[uuid.UUID]int, error)

	SaveDocument(doc *Document) error
	GetDocumentByID(uuid.UUID) (*Document, error)
	GetAllDocuments() ([]*Document, error)
	GetDocumentsCount() (int, error)

	GetDict() ([]string, error)
	TransferOrSaveToSequence([]string, bool) ([]int, error)
}

type Stemmer interface {
	Stem(string) string
}

type Encryptor interface {
	DecryptAESKey(string) error
	GetPublicKey() (*pem.Block, error)
	EncryptAES([]byte) ([]byte, error)
	DecryptMiddleware(http.Handler) http.Handler
}

type StopWords interface {
	IsStopWord(string) bool
}