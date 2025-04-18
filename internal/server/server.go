package srv

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"log"
	"net/http"
	"sync"

	"github.com/box1bs/Saturday/configs"
	"github.com/box1bs/Saturday/internal/app/index"
	"github.com/box1bs/Saturday/internal/model"
	"github.com/box1bs/Saturday/internal/view"
	"github.com/box1bs/Saturday/pkg/stemmer"
	"github.com/google/uuid"
	"github.com/rs/cors"
)

type server struct {
	activeJobs     	map[string]*jobInfo
	jobsMutex      	sync.RWMutex
	logger         	model.Logger
	indexRepos		model.Repository
	encryptor 		model.Encryptor
	index         	*index.SearchIndex
}

type jobInfo struct {
	id            	string
	status        	string
	cancel 			context.CancelFunc
}

func NewSaturdayServer(logger model.Logger, ir model.Repository, enc model.Encryptor) *server {
	return &server{
		activeJobs:     make(map[string]*jobInfo),
		logger:         logger,
		indexRepos: 	ir,
		encryptor: enc,
		index: 			index.NewSearchIndex(stemmer.NewEnglishStemmer(), stemmer.NewStopWords(), logger, ir, index.NewVectorizer()),
	}
}

type CrawlRequest struct {
	BaseUrls       []string `json:"base_urls"`
	WorkerCount    int      `json:"worker_count"`
	TaskCount      int      `json:"task_count"`
	MaxLinksInPage int      `json:"max_links_in_page"`
	MaxDepthCrawl  int      `json:"max_depth_crawl"`
	OnlySameDomain bool     `json:"only_same_domain"`
	Rate           int      `json:"rate"`
}

type CrawlResponse struct {
	JobId  string `json:"job_id"`
	Status string `json:"status"`
}

type StopRequest struct {
	JobId string `json:"job_id"`
}

type StopResponse struct {
	Status string `json:"status"`
}

type StatusResponse struct {
	Status       string `json:"status"`
	PagesCrawled int    `json:"pages_crawled"`
}

type SearchRequest struct {
	JobId      string `json:"job_id"`
	Query      string `json:"query"`
	MaxResults int    `json:"max_results"`
}

type SearchResult struct {
	Url         string  `json:"url"`
	Description string  `json:"description"`
}

func (s *server) ecnryptResponse(w http.ResponseWriter, response any) {
	data, err := json.Marshal(response)
	if err != nil {
		http.Error(w, "Failed to marshal response", http.StatusInternalServerError)
		return
	}
	encrypted, err := s.encryptor.EncryptAES(data)
	if err != nil {
		http.Error(w, "Failed to encrypt response", http.StatusInternalServerError)
		return
	}
	b64 := base64.StdEncoding.EncodeToString(encrypted)
   	w.Header().Set("Content-Type", "application/json")
   	json.NewEncoder(w).Encode(map[string]string{"data": b64})
}

func (s *server) startCrawlHandler(w http.ResponseWriter, r *http.Request) {
	var req CrawlRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	jobID := uuid.New().String()
	cfg := &configs.ConfigData{
		BaseURLs:       req.BaseUrls,
		WorkersCount:   req.WorkerCount,
		TasksCount:     req.TaskCount,
		MaxLinksInPage: req.MaxLinksInPage,
		MaxDepth:       req.MaxDepthCrawl,
		OnlySameDomain: req.OnlySameDomain,
		Rate:           req.Rate,
	}
	ctx, cancel := context.WithCancel(context.Background())
	job := &jobInfo{
		id:            	jobID,
		status:        	"initializing",
		cancel: 		cancel,
	}
	s.jobsMutex.Lock()
	s.activeJobs[jobID] = job
	s.jobsMutex.Unlock()
	go func() {
		s.jobsMutex.Lock()
		job.status = "running"
		s.jobsMutex.Unlock()

		err := s.index.Index(cfg, ctx)

		s.jobsMutex.Lock()
		if err != nil {
			job.status = fmt.Sprintf("failed: %v", err)
		} else {
			job.status = "completed"
		}
		s.jobsMutex.Unlock()
	}()
	response := CrawlResponse{JobId: jobID, Status: "started"}
	s.ecnryptResponse(w, response)
}

func (s *server) stopCrawlHandler(w http.ResponseWriter, r *http.Request) {
	var req StopRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	s.jobsMutex.RLock()
	job, exists := s.activeJobs[req.JobId]
	s.jobsMutex.RUnlock()

	if !exists {
		s.ecnryptResponse(w, StopResponse{Status: "not_found"})
		return
	}

	job.cancel()
	s.jobsMutex.Lock()
	job.status = "stopping"
	s.jobsMutex.Unlock()
	s.ecnryptResponse(w, StopResponse{Status: "stopped"})
}

func (s *server) getCrawlStatusHandler(w http.ResponseWriter, r *http.Request) {
	jobId := r.URL.Query().Get("job_id")
	if jobId == "" {
		http.Error(w, "Empty param job_id", http.StatusBadRequest)
		return
	}
	s.jobsMutex.RLock()
	job, exists := s.activeJobs[jobId]
	s.jobsMutex.RUnlock()
	if !exists {
		s.ecnryptResponse(w, StatusResponse{Status: "not_found"})
		return
	}
	response := StatusResponse{
		Status:       job.status,
		PagesCrawled: int(s.index.UrlsCrawled),
	}
	s.ecnryptResponse(w, response)
}

func (s *server) searchHandler(w http.ResponseWriter, r *http.Request) {
	var req SearchRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	results := s.index.Search(req.Query, 0, max(0, req.MaxResults))
	responseResults := make([]*SearchResult, 0)
	for i := range results {
		responseResults = append(responseResults, &SearchResult{
			Url:         results[i].URL,
			Description: results[i].Description,
		})
	}
	s.ecnryptResponse(w, responseResults)
}

func StartServer(port int, logger model.Logger, ir model.Repository, enc model.Encryptor) error {
	s := NewSaturdayServer(logger, ir, enc)
	mux := http.NewServeMux()
	mux.Handle("GET /public", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		pemBlock, err := s.encryptor.GetPublicKey()
		if err != nil {
			log.Println(err)
			http.Error(w, "Failed to get public key", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/x-pem-file")
		if err := pem.Encode(w, pemBlock); err != nil {
			log.Println(err)
			http.Error(w, "Failed to encode public key", http.StatusInternalServerError)
			return
		}
	}))
	mux.HandleFunc("POST /aes", func(w http.ResponseWriter, r *http.Request) {
		var encryptedKey string
		if err := json.NewDecoder(r.Body).Decode(&encryptedKey); err != nil {
			http.Error(w, "Invalid request body", http.StatusBadRequest)
			return
		}
		if err := s.encryptor.DecryptAESKey(encryptedKey); err != nil {
			http.Error(w, "Failed to decrypt AES key", http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusOK)
	})
	mux.Handle("/crawl/start", s.encryptor.DecryptMiddleware(http.HandlerFunc(s.startCrawlHandler)))
	mux.Handle("/crawl/stop", s.encryptor.DecryptMiddleware(http.HandlerFunc(s.stopCrawlHandler)))
	mux.Handle("/crawl/status", s.encryptor.DecryptMiddleware(http.HandlerFunc(s.getCrawlStatusHandler)))
	mux.Handle("/search", s.encryptor.DecryptMiddleware(http.HandlerFunc(s.searchHandler)))
	c := cors.New(cors.Options{
        AllowedOrigins:   []string{"*"},
        AllowedMethods:   []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"},
        AllowCredentials: true,
    })
	addr := fmt.Sprintf(":%d", port)
	view.PrintLogo()
	log.Printf("REST API started at %d\n", port)
	return http.ListenAndServe(addr, c.Handler(mux))
}