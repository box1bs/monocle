package main

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"time"

	"github.com/box1bs/monocle/configs"
	"github.com/box1bs/monocle/internal/app/indexer"
	"github.com/box1bs/monocle/internal/app/indexer/textHandling"
	"github.com/box1bs/monocle/internal/app/searcher"
	"github.com/box1bs/monocle/internal/model"
	"github.com/box1bs/monocle/internal/repository"
	"github.com/box1bs/monocle/pkg/logger"
	"github.com/joho/godotenv"
)

func main() {
	var (
		configFile = flag.String("config", "configs/search_config.json", "Path to configuration file")
	)
	flag.Parse()

	if err := godotenv.Load(); err != nil {
		panic(err)
	}

	infoPath := os.Getenv("INFO_LOG_PATH")
	iFile, err := os.OpenFile(infoPath, os.O_WRONLY, 0644)
	if err != nil {
		panic(err)
	}
	defer iFile.Close()

	errorPath := os.Getenv("ERROR_LOG_PATH")
	erFile, err := os.OpenFile(errorPath, os.O_WRONLY, 0644)
	if err != nil {
		panic(err)
	}
	defer erFile.Close()

	logger, err := logger.NewAsyncLogger(iFile, erFile)
	if err != nil {
		panic(err)
	}
	defer logger.Close()

	ir, err := repository.NewIndexRepository("index/badger", logger, 10 * 1024 * 1024)
	if err != nil {
		panic(err)
	}
	defer ir.DB.Close()

	cfg, err := configs.UploadLocalConfiguration(*configFile)
	if err != nil {
		panic(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() {
		c := make(chan os.Signal, 1)
		signal.Notify(c, os.Interrupt)
		<-c
		fmt.Println("\nShutting down...")
		cancel()
		//os.Exit(1)
	}()

	vec := textHandling.NewVectorizer()
	i, err := indexer.NewIndexer(ir, vec, logger, 2, 3)
	if err != nil {
		panic(err)
	}
	i.Index(cfg, ctx)

	count, err := i.GetDocumentsCount()
	if err != nil {
		panic(err)
	}

	fmt.Printf("Index built with %d documents. Enter search queries (Ctrl+C to exit):\n", count)

	s := searcher.NewSearcher(i, vec)

	reader := bufio.NewReader(os.Stdin)
	for {
		fmt.Print("> ")
		query, _ := reader.ReadString('\n')
		query = strings.TrimSpace(query)
		if query == "q" {
			return
		}
		t := time.Now()
		Present(s.Search(query, 100))
		fmt.Printf("--Search time: %v--\n", time.Since(t))
	}
}

func Present(docs []*model.Document) {
	if len(docs) == 0 {
		fmt.Println("No results found.")
		return
	}
	
	fmt.Printf("Found %d results:\n", len(docs))
	for i, doc := range docs {
		fmt.Printf("%d. URL: %s\n\n", 
			i+1, doc.URL)
	}
}