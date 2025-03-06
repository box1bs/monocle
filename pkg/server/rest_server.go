package rest

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"sync"

	"github.com/box1bs/Saturday/pkg/handleTools"
	"github.com/box1bs/Saturday/pkg/logger"
	"github.com/box1bs/Saturday/pkg/searchIndex"
	"github.com/box1bs/Saturday/pkg/stemmer"
	"github.com/google/uuid"
)

type server struct {
	activeJobs     map[string]*jobInfo
	jobsMutex      sync.RWMutex
	logger         *logger.AsyncLogger
	indexInstances map[string]*searchIndex.SearchIndex
}

type jobInfo struct {
	id            string
	index         *searchIndex.SearchIndex
	status        string
	stopCrawlChan chan struct{}
}

func NewSaturdayServer(logger *logger.AsyncLogger) *server {
	return &server{
		activeJobs:     make(map[string]*jobInfo),
		logger:         logger,
		indexInstances: make(map[string]*searchIndex.SearchIndex),
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
	Score       float32 `json:"score"`
}

type SearchResponse struct {
	Results []SearchResult `json:"results"`
}

func (s *server) startCrawlHandler(w http.ResponseWriter, r *http.Request) {
	var req CrawlRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	jobID := uuid.New().String()
	cfg := &handleTools.ConfigData{
		BaseURLs:       req.BaseUrls,
		WorkersCount:   req.WorkerCount,
		TasksCount:     req.TaskCount,
		MaxLinksInPage: req.MaxLinksInPage,
		MaxDepth:       req.MaxDepthCrawl,
		OnlySameDomain: req.OnlySameDomain,
		Rate:           req.Rate,
	}
	stopCrawlChan := make(chan struct{})
	idx := searchIndex.NewSearchIndex(stemmer.NewEnglishStemmer(), s.logger, stopCrawlChan)
	job := &jobInfo{
		id:            jobID,
		index:         idx,
		status:        "initializing",
		stopCrawlChan: stopCrawlChan,
	}

	s.jobsMutex.Lock()
	s.activeJobs[jobID] = job
	s.indexInstances[jobID] = idx
	s.jobsMutex.Unlock()

	go func() {
		s.jobsMutex.Lock()
		job.status = "running"
		s.jobsMutex.Unlock()

		err := idx.Index(cfg)

		s.jobsMutex.Lock()
		if err != nil {
			job.status = fmt.Sprintf("failed: %v", err)
		} else {
			job.status = "completed"
		}
		s.jobsMutex.Unlock()
	}()

	response := CrawlResponse{JobId: jobID, Status: "started"}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
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
		json.NewEncoder(w).Encode(StopResponse{Status: "not_found"})
		return
	}

	job.stopCrawlChan <- struct{}{}
	s.jobsMutex.Lock()
	job.status = "stopping"
	s.jobsMutex.Unlock()
	json.NewEncoder(w).Encode(StopResponse{Status: "stopping"})
}

func (s *server) getCrawlStatusHandler(w http.ResponseWriter, r *http.Request) {
	jobId := r.URL.Query().Get("job_id")
	if jobId == "" {
		http.Error(w, "Отсутствует параметр job_id", http.StatusBadRequest)
		return
	}
	s.jobsMutex.RLock()
	job, exists := s.activeJobs[jobId]
	s.jobsMutex.RUnlock()
	if !exists {
		json.NewEncoder(w).Encode(StatusResponse{Status: "not_found"})
		return
	}
	response := StatusResponse{
		Status:       job.status,
		PagesCrawled: int(job.index.UrlsCrawled),
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

func (s *server) searchHandler(w http.ResponseWriter, r *http.Request) {
	var req SearchRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	s.jobsMutex.RLock()
	idx, exists := s.indexInstances[req.JobId]
	s.jobsMutex.RUnlock()
	if !exists || idx == nil {
		http.Error(w, "Search index isn't exist", http.StatusNotFound)
		return
	}
	results := idx.Search(req.Query)
	var responseResults []SearchResult
	maxResults := req.MaxResults
	if maxResults <= 0 || maxResults > len(results) {
		maxResults = len(results)
	}
	for i := range maxResults {
		responseResults = append(responseResults, SearchResult{
			Url:         results[i].URL,
			Description: results[i].Description,
			Score:       results[i].Score,
		})
	}
	response := SearchResponse{Results: responseResults}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

func StartServer(port int, logger *logger.AsyncLogger) error {
	s := NewSaturdayServer(logger)
	http.HandleFunc("POST /crawl/start", s.startCrawlHandler)
	http.HandleFunc("POST /crawl/stop", s.stopCrawlHandler)
	http.HandleFunc("GET /crawl/status", s.getCrawlStatusHandler)
	http.HandleFunc("POST /search", s.searchHandler)
	addr := fmt.Sprintf(":%d", port)
	log.Printf("REST API started at %d", port)
	return http.ListenAndServe(addr, nil)
}