package main

import (
	"bufio"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/box1bs/Saturday/configs"
	searchIndex "github.com/box1bs/Saturday/internal/app/index"
	"github.com/box1bs/Saturday/internal/model"
	indexRepository "github.com/box1bs/Saturday/internal/repository"
	srv "github.com/box1bs/Saturday/internal/server"
	"github.com/box1bs/Saturday/logs/logger"
	"github.com/box1bs/Saturday/pkg/stemmer"
	"github.com/dgraph-io/badger/v3"
)

func main() {
	var (
		configFile = flag.String("config", "configs/search_config.json", "Path to configuration file")
		logFile    = flag.String("log", "logs/indexedURLs.txt", "Path to log file")
		httpPort   = flag.Int("srv-port", 50051, "REST server port")
		runCli     = flag.Bool("cli", false, "Run in CLI mode instead of REST server")
	)
	flag.Parse()

	logger, err := logger.NewAsyncLogger(*logFile)
	if err != nil {
		panic(err)
	}
	defer logger.File.Close()

	opts := badger.DefaultOptions("/index/badger")
	db, err := badger.Open(opts)
	if err != nil {
		panic(err)
	}
	defer db.Close()

	ir := indexRepository.NewIndexRepository(db)

	if *runCli {
		runCliMode(logger, *configFile, ir)
		return
	}

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)

	errChan := make(chan error)
	go func() {
		errChan <- srv.StartServer(*httpPort, logger, ir)
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
func runCliMode(logger *logger.AsyncLogger, configPath string, ir model.Repository) {
	cfg, err := configs.UploadLocalConfiguration(configPath)
	if err != nil {
		panic(err)
	}

	i := searchIndex.NewSearchIndex(stemmer.NewEnglishStemmer(), stemmer.NewStopWords(), logger, ir, nil)
	if err := i.Index(cfg); err != nil {
		panic(err)
	}

	fmt.Println("Index built. Enter search queries (Ctrl+C to exit):")

	reader := bufio.NewReader(os.Stdin)
	for {
		fmt.Print("> ")
		query, _ := reader.ReadString('\n')
		Present(i.Search(strings.TrimSpace(query)))
	}
}

func Present(docs []*model.Document) {
	if len(docs) == 0 {
		fmt.Println("No results found.")
		return
	}
	
	fmt.Printf("Found %d results:\n", len(docs))
	for i, doc := range docs {
		fmt.Printf("%d. URL: %s\nDescription: %s\n\n", 
			i+1, doc.URL, doc.Description)
	}
}