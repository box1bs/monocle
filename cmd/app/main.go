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
)

func main() {
	var (
		configFile = flag.String("config", "configs/app_config.json", "Path to configuration file")
	)
	flag.Parse()

	cfg, err := configs.UploadLocalConfiguration(*configFile)
	if err != nil {
		panic(err)
	}

	in := os.Stdout
	er := os.Stderr
	if cfg.InfoLogPath != "-" {
		in, err = os.Create(cfg.InfoLogPath)
		if err != nil {
			panic(err)
		}
	}
	if cfg.ErrorLogPath != "-" {
		er, err = os.Create(cfg.ErrorLogPath)
		if err != nil {
			panic(err)
		}
	}
	defer in.Close()
	defer er.Close()

	log := logger.NewLogger(in, er, cfg.LogChannelSize)
	defer log.Close()

	ir, err := repository.NewIndexRepository(cfg.IndexPath, log, cfg.MaxTransactionBytes)
	if err != nil {
		panic(err)
	}
	defer ir.DB.Close()

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

	vec := textHandling.NewVectorizer(ctx, cfg.WorkersCount, cfg.TickerTimeMilliseconds)
	defer vec.Close()
	i, err := indexer.NewIndexer(ir, vec, log)
	if err != nil {
		panic(err)
	}
	i.Index(cfg, ctx)

	count, err := ir.GetDocumentsCount()
	if err != nil {
		panic(err)
	}

	log.Write(logger.NewMessage(logger.MAIN_LAYER, logger.INFO, "index built with %d documents", count))
	fmt.Printf("Index built with %d documents. Enter search queries (Ctrl+C to exit):\n", count)

	s := searcher.NewSearcher(log, i, ir, vec)

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