package main

import (
	handle "github.com/box1bs/Saturday/pkg/handleTools"
	"github.com/box1bs/Saturday/pkg/logger"
	"github.com/box1bs/Saturday/pkg/searchIndex"
	"github.com/box1bs/Saturday/pkg/stemmer"
	"github.com/box1bs/Saturday/pkg/server"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
)

func main() {
	var (
		configFile = flag.String("config", "search_config.json", "Path to configuration file")
		logFile    = flag.String("log", "crawled.txt", "Path to log file")
		httpPort   = flag.Int("grpc-port", 50051, "gRPC server port")
		runCli     = flag.Bool("cli", false, "Run in CLI mode instead of gRPC server")
	)
	flag.Parse()

	logger, err := logger.NewAsyncLogger(*logFile)
	if err != nil {
		panic(err)
	}
	defer logger.File.Close()

	if *runCli {
		runCliMode(logger, *configFile)
		return
	}

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)

	errChan := make(chan error)
	go func() {
		errChan <- rest.StartServer(*httpPort, logger)
	}()

	select {
		case <-stop:
			log.Println("Shutting down...")
			return
		case err := <-errChan:
			log.Printf("Error: %v\n", err)
			return
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