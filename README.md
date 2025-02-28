# Saturday - Web Crawler and Search Engine

## Project Overview

Saturday is a Go-based web crawler and search engine that efficiently indexes web content for searching. The system crawls specified websites, extracts and processes text content, and builds a search index that supports relevancy-based querying.

## Core Features

1. **Multi-site Crawling**: Supports crawling multiple base URLs with configurable depth limits
2. **Rate Limiting**: Controls request rates to avoid overloading target servers
3. **Worker Pool Architecture**: Utilizes concurrent workers for efficient crawling
4. **Text Processing**: Implements stemming and stop word filtering for improved search quality
5. **TF-IDF Search**: Uses term frequency-inverse document frequency algorithm for relevancy-based search results
6. **Sitemap Support**: Automatically detects and processes XML sitemaps
7. **Configurable Parameters**: Customizable crawl behavior through configuration files

## System Architecture

The project is structured into several packages with clear separation of concerns:

### Main Components

1. **Web Spider**: Handles crawling webpages, extracting links, and processing content
2. **Search Index**: Manages document indexing and search functionality using TF-IDF scoring
3. **Worker Pool**: Provides concurrent task execution with controlled parallelism
4. **Text Processing**: Implements English stemming and stop word filtering
5. **Rate Limiter**: Controls request rates to respect server limits
6. **Document Handler**: Processes HTML content and extracts relevant information
7. **Logger**: Provides asynchronous logging capabilities

## Technical Implementation

### Web Crawling Process

The crawler works as follows:

1. Starts with a set of base URLs defined in the configuration
2. For each base URL, it creates a Spider instance
3. The Spider checks for a sitemap.xml file to discover additional URLs
4. For each discovered page:
   - Extracts links and content
   - Processes text (removes stop words, applies stemming)
   - Adds the processed content to the search index
   - Enqueues new discovered links for crawling (within depth limit)

### Search Functionality

The search engine implements TF-IDF (Term Frequency-Inverse Document Frequency):

1. User enters a search query
2. Query text is tokenized and stemmed
3. The system calculates TF-IDF scores for matching documents
4. Results are sorted by relevance score
5. The system returns URL, description, and score for each result

### Concurrency Model

The project uses a worker pool pattern:

1. A configurable number of worker goroutines process crawling tasks
2. Tasks are submitted to a queue with configurable capacity
3. A wait group manages synchronization for graceful shutdown

## Configuration Options

The system is highly configurable through a JSON configuration file:

```json
{
    "base_urls": ["https://example.com/"],
    "worker_count": 1000,
    "task_count": 10000,
    "max_links_in_page": 100,
    "max_depth_crawl": 6,
    "only_same_domain": true,
    "rate": 500
}
```

Key parameters:
- `base_urls`: Starting points for crawling
- `worker_count`: Number of concurrent workers
- `task_count`: Capacity of the task queue
- `max_links_in_page`: Maximum number of links to extract per page
- `max_depth_crawl`: Maximum crawl depth from base URLs
- `only_same_domain`: Whether to stay within the same domain
- `rate`: Maximum requests per second

## Code Structure

### Main Components

- **`main.go`**: Program entry point, initializes logger, configuration, and search index
- **`handleTools.go`**: HTML parsing and URL normalization utilities
- **`searchIndex.go`**: Core search index implementation with TF-IDF scoring
- **`stemmer.go`**: English stemming algorithm and stop words management
- **`webSpider.go`**: Web crawling logic including sitemap processing
- **`workerPool.go`**: Concurrent task execution framework
- **`rateLimiter.go`**: Controls request rate to target servers
- **`asyncLogger.go`**: Non-blocking logging implementation

## Usage Examples

### Starting the Crawler

```go
// Initialize logger
logger, err := logger.NewAsyncLogger("crawled.txt")
if err != nil {
    panic(err)
}
defer logger.File.Close()

// Load configuration
cfg, err := handle.UploadLocalConfiguration("search_config.json")
if err != nil {
    panic(err)
}

// Create and start search index
i := searchIndex.NewSearchIndex(stemmer.NewEnglishStemmer(), logger)
if err := i.Start(cfg); err != nil {
    panic(err)
}
```

### Performing Searches

```go
// Interactive search loop
var query string
for {
    fmt.Scan(&query)
    results := i.Search(query)
    
    for _, doc := range results {
        fmt.Printf("URL: %s\nDescription: %s\nScore: %f\n", 
                  doc.URL, doc.Description, doc.Score)
    }
}
```

## Performance Considerations

- The worker pool architecture allows efficient utilization of system resources
- Rate limiting prevents overwhelming target servers
- Asynchronous logging minimizes I/O bottlenecks
- URL normalization and visit tracking prevent redundant crawling
- The search index uses memory-efficient data structures for term-document relationships

## Technical Requirements

- Go 1.13+
- External dependencies:
  - github.com/google/uuid
  - golang.org/x/net/html

## License

This project is available under the MIT License.