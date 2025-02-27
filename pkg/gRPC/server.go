package gRPC

import (
	handle "Spider/pkg/handleTools"
	"Spider/pkg/logger"
	"Spider/pkg/searchIndex"
	"Spider/pkg/stemmer"
	"context"
	"fmt"
	"log"
	"net"
	"sync"

	"github.com/google/uuid"
	"google.golang.org/grpc"
)

type server struct {
	UnimplementedSpiderServiceServer
	activeJobs     map[string]*jobInfo
	jobsMutex      sync.RWMutex
	logger         *logger.AsyncLogger
	indexInstances map[string]*searchIndex.SearchIndex
}

type jobInfo struct {
	id         		string
	index      		*searchIndex.SearchIndex
	status     		string
	stopCrawlChan  	chan struct{}
}

func NewSpiderServer(logger *logger.AsyncLogger) *server {
	return &server{
		activeJobs:     make(map[string]*jobInfo),
		logger:         logger,
		indexInstances: make(map[string]*searchIndex.SearchIndex),
	}
}

func (s *server) StartCrawl(ctx context.Context, req *CrawlRequest) (*CrawlResponse, error) {
	jobID := uuid.New().String()
	
	// Convert proto request to config
	cfg := &handle.ConfigData{
		BaseURLs:       req.BaseUrls,
		WorkersCount:   int(req.WorkerCount),
		TasksCount:     int(req.TaskCount),
		MaxLinksInPage: int(req.MaxLinksInPage),
		MaxDepth:       int(req.MaxDepthCrawl),
		OnlySameDomain: req.OnlySameDomain,
		Rate:           int(req.Rate),
	}

	stopCrawlChan := make(chan struct{})
	
	// Create search index
	idx := searchIndex.NewSearchIndex(stemmer.NewEnglishStemmer(), s.logger, stopCrawlChan)
	
	// Store job info
	job := &jobInfo{
		id:     jobID,
		index:  idx,
		status: "initializing",
		stopCrawlChan: stopCrawlChan,
	}
	
	s.jobsMutex.Lock()
	s.activeJobs[jobID] = job
	s.indexInstances[jobID] = idx
	s.jobsMutex.Unlock()
	
	// Start crawling in a separate goroutine
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
	
	return &CrawlResponse{
		JobId:  jobID,
		Status: "started",
	}, nil
}

func (s *server) StopCrawl(ctx context.Context, req *StopRequest) (*StopResponse, error) {
	s.jobsMutex.RLock()
	job, exists := s.activeJobs[req.JobId]
	s.jobsMutex.RUnlock()
	
	if !exists {
		return &StopResponse{Status: "not_found"}, nil
	}
	
	job.stopCrawlChan <- struct{}{}
	
	s.jobsMutex.Lock()
	job.status = "stopping"
	s.jobsMutex.Unlock()
	
	return &StopResponse{Status: "stopping"}, nil
}

func (s *server) GetCrawlStatus(ctx context.Context, req *StatusRequest) (*StatusResponse, error) {
	s.jobsMutex.RLock()
	job, exists := s.activeJobs[req.JobId]
	s.jobsMutex.RUnlock()
	
	if !exists {
		return &StatusResponse{Status: "not_found"}, nil
	}
	
	return &StatusResponse{
		Status:      job.status,
		PagesCrawled: job.index.UrlsCrawled,
	}, nil
}

func (s *server) Search(ctx context.Context, req *SearchRequest) (*SearchResponse, error) {
	var idx *searchIndex.SearchIndex
	
	s.jobsMutex.RLock()
	idx = s.indexInstances[req.JobId]
	s.jobsMutex.RUnlock()
	
	if idx == nil {
		return &SearchResponse{}, fmt.Errorf("no search index available")
	}
	
	// Perform search
	results := idx.Search(req.Query)
	
	// Convert to response format
	var responseResults []*SearchResult
	maxResults := int(req.MaxResults)
	if maxResults <= 0 || maxResults > len(results) {
		maxResults = len(results)
	}
	
	for i := range maxResults {
		responseResults = append(responseResults, &SearchResult{
			Url:         results[i].URL,
			Description: results[i].Description,
			Score:       results[i].Score,
		})
	}
	
	return &SearchResponse{
		Results: responseResults,
	}, nil
}

// StartServer starts the gRPC server on the specified port
func StartServer(port int, logger *logger.AsyncLogger) error {
	lis, err := net.Listen("tcp", fmt.Sprintf(":%d", port))
	if err != nil {
		return err
	}
	
	s := grpc.NewServer()
	RegisterSpiderServiceServer(s, NewSpiderServer(logger))
	
	log.Printf("gRPC server listening on port %d", port)
	return s.Serve(lis)
}