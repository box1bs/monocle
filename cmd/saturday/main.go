package main

import (
	handle "github.com/box1bs/Saturday/pkg/handleTools"
	"github.com/box1bs/Saturday/pkg/logger"
	"github.com/box1bs/Saturday/pkg/searchIndex"
	"github.com/box1bs/Saturday/pkg/stemmer"
	grpcServer "github.com/box1bs/Saturday/pkg/gRPC"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
)

func main() {
	// Parse command line flags
	var (
		configFile = flag.String("config", "search_config.json", "Path to configuration file")
		logFile    = flag.String("log", "crawled.txt", "Path to log file")
		grpcPort   = flag.Int("grpc-port", 50051, "gRPC server port")
		runCli     = flag.Bool("cli", false, "Run in CLI mode instead of gRPC server")
	)
	flag.Parse()

	// Initialize logger
	logger, err := logger.NewAsyncLogger(*logFile)
	if err != nil {
		panic(err)
	}
	defer logger.File.Close()

	// If CLI mode is enabled, run traditional CLI workflow
	if *runCli {
		runCliMode(logger, *configFile)
		return
	}

	// Otherwise, start gRPC server
	log.Printf("Starting gRPC server on port %d", *grpcPort)
	
	// Set up graceful shutdown
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	
	errChan := make(chan error)
	go func() {
		errChan <- grpcServer.StartServer(*grpcPort, logger)
	}()
	
	// Wait for shutdown signal or error
	select {
	case <-stop:
		log.Println("Received shutdown signal")
	case err := <-errChan:
		log.Printf("Server error: %v", err)
	}
}

// Original CLI mode functionality
func runCliMode(logger *logger.AsyncLogger, configPath string) {
	cfg, err := handle.UploadLocalConfiguration(configPath)
	if err != nil {
		panic(err)
	}

	i := searchIndex.NewSearchIndex(stemmer.NewEnglishStemmer(), logger, nil)
	if err := i.Index(cfg); err != nil {
		panic(err)
	}

	fmt.Println("Index built. Enter search queries (Ctrl+C to exit):")
	
	var query string
	for {
		fmt.Print("> ")
		fmt.Scan(&query)
		Present(i.Search(query))
	}
}

func Present(docs []*handle.Document) {
	if len(docs) == 0 {
		fmt.Println("No results found.")
		return
	}
	
	fmt.Printf("Found %d results:\n", len(docs))
	for i, doc := range docs {
		fmt.Printf("%d. URL: %s\nDescription: %s\nScore: %.4f\n\n", 
			i+1, doc.URL, doc.Description, doc.Score)
	}
}