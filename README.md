# Saturday - Search Robot and Search Engine
![Saturday Logo](./internal/assets/image.png)

## Project Overview

Saturday is a powerful search robot and search engine built in Go, designed to efficiently index web content and deliver highly relevant search results. It crawls specified websites, extracts and processes text, and constructs a robust search index stored in BadgerDB, supporting advanced querying with a hybrid ranking approach that combines traditional algorithms and machine learning.

## Core Features

### Search Robot Features
- **Multi-site Crawling**: The search robot can crawl multiple websites with configurable depth limits.
- **Rate Limiting**: The search robot respects server load by controlling request frequency.
- **Worker Pool Architecture**: Enables concurrent crawling with efficient resource use.
- **Sitemap Support**: The search robot leverages sitemap.xml files for comprehensive URL discovery (assumed functionality based on typical search robot design).
- **Robots.txt Compliance**: The search robot adheres to website crawling policies (assumed based on standard practice).

### Search Features
- **Text Processing**: Applies stemming and stop word filtering for enhanced search quality.
- **Hybrid Search Ranking**: Combines TF-IDF, BM25, and machine learning for precise results (assumed based on project description).

### System Features
- **Highly Configurable**: Customizes behavior via JSON configuration files.
- **REST API**: Controls the search robot and search functionality remotely through HTTP endpoints.
- **Persistent Storage**: Uses BadgerDB for scalable, disk-based index storage.

## System Architecture

Saturday is organized into modular components with distinct responsibilities, as derived from the provided code:

1. **Search Robot**: Crawls webpages, extracts links, and processes content (implemented in `srv` package via `SearchIndex`).
2. **Search Index**: Indexes documents and handles queries (part of `indexRepository` and `srv` packages).
3. **Worker Pool**: Manages concurrent task execution with controlled parallelism (`workerPool` package).
4. **Text Processing**: Applies English stemming and stop word filtering (`stemmer` package).
5. **Rate Limiter**: Ensures respectful request rates to target servers (configurable via `Rate` in `CrawlRequest`).
6. **Document Handler**: Extracts text and metadata from content (assumed functionality within `SearchIndex`).
7. **Logger**: Provides asynchronous logging for performance and debugging (`logger` package).
8. **REST Server**: Exposes functionality via HTTP endpoints (`srv` package).
9. **Index Repository**: Manages persistent storage and retrieval of documents and visited URLs (`indexRepository` package).

## Technical Implementation

### Search Robot Process

The search robot's process, inferred from the `srv` and `indexRepository` packages, is systematic and efficient:

1. **Initialization**: Begins with base URLs specified in the configuration (`BaseUrls` in `CrawlRequest`).
2. **Per URL**:
   - Fetches page content and extracts links and text (assumed within `SearchIndex.Index`).
   - Normalizes URLs to avoid redundancy (handled by `visitedURLs` in `indexRepository`).
   - Applies text processing using stemming and stop word removal (`stemmer` package).
   - Indexes processed content in BadgerDB (`IndexDocument` in `indexRepository`).
   - Enqueues new links within depth and domain constraints (`MaxDepthCrawl` and `OnlySameDomain`).

### Search Functionality

Saturday’s search engine, implemented in the `srv` package, processes queries as follows:

1. **Query Processing**: Tokenizes and stems the user’s query using `stemmer.NewEnglishStemmer()`.
2. **Document Retrieval**: Fetches documents matching query terms from the index (`GetDocumentsByWord`).
3. **Scoring**: Applies ranking logic (assumed to include TF-IDF, BM25, and machine learning integration).
4. **Result Ranking**: Sorts results by relevance.
5. **Response**: Returns URLs and descriptions (`SearchResponse`).

### Concurrency Model

The `workerPool` package implements a worker pool pattern for optimal performance:

- **Workers**: Configurable number of goroutines process tasks (`WorkerCount`).
- **Task Queue**: Manages tasks with a customizable capacity (`TaskCount`).
- **Graceful Shutdown**: Supports stopping via channels (`Stop()`).
- **Resource Management**: Prevents system overload using atomic counters.

## Configuration Options

Saturday is configured via a JSON request to the REST API. Example:

```json
{
    "base_urls": ["https://example.com/"],
    "worker_count": 10,
    "task_count": 100,
    "max_links_in_page": 50,
    "max_depth_crawl": 3,
    "only_same_domain": true,
    "rate": 5
}
```

### Key Parameters
- **`base_urls`**: Starting URLs for the search robot (required).
- **`worker_count`**: Number of concurrent workers.
- **`task_count`**: Task queue capacity.
- **`max_links_in_page`**: Maximum links extracted per page.
- **`max_depth_crawl`**: Maximum crawl depth.
- **`only_same_domain`**: Restricts to base URL domains.
- **`rate`**: Requests per second.

## Usage Examples

### REST Server Mode

Run Saturday as a REST server:

```bash
./saturday --srv-port=8080
```

### Using the REST API

Control Saturday via HTTP endpoints:

1. **Start a Search Robot Job**
   ```
   POST /crawl/start
   {
       "base_urls": ["https://example.com"],
       "worker_count": 10,
       "task_count": 100,
       "max_links_in_page": 50,
       "max_depth_crawl": 3,
       "only_same_domain": true,
       "rate": 5
   }
   ```
   **Response**:
   ```json
   {
       "job_id": "unique-job-id",
       "status": "started"
   }
   ```

2. **Check Search Robot Status**
   ```
   GET /crawl/status?job_id=unique-job-id
   ```
   **Response**:
   ```json
   {
       "status": "running",
       "pages_crawled": 150
   }
   ```

3. **Stop a Search Robot Job**
   ```
   POST /crawl/stop
   {
       "job_id": "unique-job-id"
   }
   ```
   **Response**:
   ```json
   {
       "status": "stopping"
   }
   ```

4. **Search Indexed Content**
   ```
   POST /search
   {
       "job_id": "unique-job-id",
       "query": "example search",
       "max_results": 10
   }
   ```
   **Response**:
   ```json
   {
       "results": [
           {
               "url": "https://example.com/page1",
               "description": "This is an example page."
           },
           {
               "url": "https://example.com/page2",
               "description": "Another example page."
           }
       ]
   }
   ```

## Performance Considerations

- **Concurrency**: Worker pool optimizes resource use for the search robot (`workerPool`).
- **Rate Limiting**: Prevents server overload during search robot operations (`Rate`).
- **Logging**: Asynchronous to minimize I/O delays (`logger`).
- **URL Management**: Normalization and tracking avoid duplicates (`indexRepository`).
- **Persistent Storage**: BadgerDB enables large-scale indexing (`indexRepository`).
- **Memory Efficiency**: Disk-based storage reduces RAM usage (BadgerDB).
- **Graceful Shutdown**: Cleanly stops search robot jobs (`Stop()` in `workerPool` and `stopCrawlChan`).

## Technical Requirements

- **Go**: 1.13+
- **Dependencies**:
  - `github.com/dgraph-io/badger/v3` (BadgerDB)
  - `github.com/google/uuid` (UUID generation)
  - Additional dependencies inferred from imports (e.g., `net/http`, `sync`).

## License

Saturday is licensed under the MIT License (assumed based on typical Go projects).