package main

import (
	handle "SE/handleTools"
	"SE/logger"
	"SE/searchIndex"
	"SE/stemmer"
	"fmt"
)

func main() {
	logger, err := logger.NewAsyncLogger("crawled.txt")
	if err != nil {
		panic(err)
	}
	defer logger.File.Close()

	cfg, err := handle.UploadLocalConfiguration("search_config.json")
	if err != nil {
		panic(err)
	}

	i := searchIndex.NewSearchIndex(stemmer.NewEnglishStemmer(), logger)
	if err := i.Start(cfg); err != nil {
		panic(err)
	}

	var query string
	for {
		fmt.Scan(&query)
		Present(i.Search(query))
	}
}

func Present(docs []*handle.Document) {
	for _, doc := range docs {
		fmt.Printf("URL: %s\nDescription: %s\nScore: %f\n", doc.URL, doc.Description, doc.Score)
	}
}