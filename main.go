package main

import (
	"SE/handleTools"
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

	urls, err := handleTools.GetLocalConfigUrls("sitemap.xml")
	if err != nil {
		panic(err)
	}
	i := searchIndex.NewSearchIndex(stemmer.NewEnglishStemmer(), logger)
	i.Start(urls, 5)

	var query string
	for {
		fmt.Scan(&query)
		Present(i.Search(query))
	}
}

func Present(docs []*handleTools.Document) {
	for _, doc := range docs {
		fmt.Printf("URL: %s\nDescription: %s\nScore: %f\n", doc.URL, doc.Description, doc.Score)
	}
}