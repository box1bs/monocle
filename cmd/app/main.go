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
	"github.com/box1bs/monocle/logs/logger"
)

func main() {
	var (
		configFile = flag.String("config", "configs/search_config.json", "Path to configuration file")
		logFile    = flag.String("log", "logs/indexedURLs.txt", "Path to log file")
	)
	flag.Parse()

	ir, err := repository.NewIndexRepository("index/badger")
	if err != nil {
		panic(err)
	}
	defer ir.DB.Close()

	cfg, err := configs.UploadLocalConfiguration(*configFile)
	if err != nil {
		panic(err)
	}

	file, err := os.Create(*logFile)
	if err != nil {
		panic(err)
	}
	defer file.Close()

	logger, err := logger.NewAsyncLogger(os.Stdout)
	if err != nil {
		panic(err)
	}
	defer logger.Close()

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
	i, err := indexer.NewIndexer(ir, vec, logger, 2)
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