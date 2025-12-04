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
	"github.com/box1bs/monocle/internal/tui"
	"github.com/box1bs/monocle/pkg/logger"
	tea "github.com/charmbracelet/bubbletea"
)

func main() {
	var (
		configFile = flag.String("config", "configs/app_config.json", "Path to configuration file")
		indexFlag = flag.Bool("i", false, "disable indexing")
		interfaceFlag = flag.Bool("gui", false, "use terminal UI")
	)
	flag.Parse()
	
	cfg, err := configs.UploadLocalConfiguration(*configFile)
	if err != nil {
		panic(err)
	}

	if *interfaceFlag {
		initGUI(cfg, *indexFlag)
		return
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

	vec := textHandling.NewVectorizer(cfg.WorkersCount, cfg.TickerTimeMilliseconds, cfg.PythonSrvPath)
	if err := vec.WaitForPythonServer(ctx); err != nil && err.Error() != textHandling.BaseCanceledError {
		panic(err)
	} else if err != nil {
		log.Write(logger.NewMessage(logger.MAIN_LAYER, logger.ERROR, textHandling.BaseCanceledError))
		return
	}

	defer vec.Close()
	i, err := indexer.NewIndexer(ir, vec, log, cfg)
	if err != nil {
		panic(err)
	}
	if !*indexFlag {
		i.Index(cfg, ctx)
	}

	count, err := ir.GetDocumentsCount()
	if err != nil {
		panic(err)
	}

	log.Write(logger.NewMessage(logger.MAIN_LAYER, logger.INFO, "index built with %d documents", count))
	fmt.Printf("Index built with %d documents. Enter search queries (q to exit):\n", count)

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

func initGUI(cfg *configs.ConfigData, indexF bool) {
	lc := tui.NewLogChannel(cfg.LogChannelSize)
	log := logger.NewLogger(lc, lc, cfg.LogChannelSize)
	defer log.Close()

	ir, err := repository.NewIndexRepository(cfg.IndexPath, log, cfg.MaxTransactionBytes)
	if err != nil {
		panic(err)
	}
	defer ir.DB.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	c := make(chan struct{}, 1)
	go func() {
		<-c
		fmt.Println("\nShutting down...")
		cancel()
		//os.Exit(1)
	}()

	vec := textHandling.NewVectorizer(cfg.WorkersCount, cfg.TickerTimeMilliseconds, cfg.PythonSrvPath)
	if err := vec.WaitForPythonServer(ctx); err != nil && err.Error() != textHandling.BaseCanceledError {
		panic(err)
	} else if err != nil {
		log.Write(logger.NewMessage(logger.MAIN_LAYER, logger.ERROR, textHandling.BaseCanceledError))
		return
	}
	defer vec.Close()
	i, err := indexer.NewIndexer(ir, vec, log, cfg)
	if err != nil {
		panic(err)
	}
	if !indexF {
		go i.Index(cfg, ctx)
	}

	model := tui.InitModel(lc, cfg.TUIBorderColor, ir.GetDocumentsCount, searcher.NewSearcher(log, i, ir, vec).Search, c)
	if _, err := tea.NewProgram(model).Run(); err != nil {
		panic(err)
	}
}