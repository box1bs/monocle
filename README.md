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
- **Hybrid Search Ranking**: Combines TF-IDF, BM25, and BERT-tiny model for precise results.
- **Neural Embeddings**: Uses BERT-tiny for generating document and query embeddings, enabling semantic search capabilities.

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

Saturday's search engine, implemented in the `srv` package, processes queries as follows:

1. **Query Processing**: Tokenizes and stems the user's query using `stemmer.NewEnglishStemmer()`.
2. **Document Retrieval**: Fetches documents matching query terms from the index (`GetDocumentsByWord`).
3. **Semantic Scoring**: Generates embeddings using BERT-tiny model for both query and documents.
4. **Hybrid Ranking**: Combines traditional scoring (TF-IDF, BM25) with cosine similarity of BERT-tiny embeddings.
5. **Result Ranking**: Sorts results by relevance using the combined scoring approach.
6. **Response**: Returns URLs and descriptions (`SearchResponse`).

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

## API Endpoints Documentation

### Authentication and Encryption
All endpoints except `/public` and `/aes` require encrypted request bodies using AES encryption.

#### 1. Get Public Key
```http
GET /public
```
**Description**: Retrieves the RSA public key for encrypting the AES key.
**Response**: PEM-encoded RSA public key
**Content-Type**: application/x-pem-file
**Status Codes**:
- 200: Success
- 500: Server error getting/encoding public key

#### 2. Set AES Key
```http
POST /aes
```
**Description**: Sends RSA-encrypted AES key for subsequent requests
**Request Body**: RSA-encrypted AES key (base64 encoded)
**Status Codes**:
- 200: Success
- 400: Invalid request body
- 500: Decryption failed

### Search Robot Control

#### 3. Start Crawling
```http
POST /crawl/start
```
**Description**: Initiates a new crawling job
**Request Body**:
```json
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
    "job_id": "uuid-string",
    "status": "started"
}
```
**Status Codes**:
- 200: Job started successfully
- 400: Invalid request parameters
- 500: Internal server error

#### 4. Stop Crawling
```http
POST /crawl/stop
```
**Description**: Stops an active crawling job
**Request Body**:
```json
{
    "job_id": "uuid-string"
}
```
**Response**:
```json
{
    "status": "stopped"
}
```
**Status Codes**:
- 200: Job stopped successfully
- 400: Invalid job ID
- 404: Job not found

#### 5. Get Crawling Status
```http
GET /crawl/status?job_id=uuid-string
```
**Description**: Retrieves the current status of a crawling job
**Query Parameters**:
- job_id: UUID of the crawling job
**Response**:
```json
{
    "status": "running",
    "pages_crawled": 150
}
```
**Possible Status Values**:
- "initializing": Job is starting up
- "running": Job is actively crawling
- "stopping": Job is gracefully shutting down
- "completed": Job finished successfully
- "failed": Job encountered an error
- "not_found": Job ID doesn't exist

**Status Codes**:
- 200: Status retrieved successfully
- 400: Missing or invalid job ID
- 404: Job not found

### Search Operations

#### 6. Search Indexed Content
```http
POST /search
```
**Description**: Searches the indexed content using hybrid ranking
**Request Body**:
```json
{
    "job_id": "uuid-string",
    "query": "search terms",
    "max_results": 10
}
```
**Response**:
```json
[
    {
        "url": "https://example.com/page1",
        "description": "Page description with highlighted search terms"
    }
]
```
**Query Parameters**:
- query: Search terms (required)
- max_results: Maximum number of results to return (default: 10)
- job_id: Optional ID to search within specific crawl results

**Ranking Factors**:
- Word frequency (TF-IDF)
- BM25 score
- Semantic similarity (BERT embeddings)
- Word inclusion count

**Status Codes**:
- 200: Search completed successfully
- 400: Invalid search parameters
- 500: Search operation failed

### Response Encryption
All successful responses (except `/public`) are encrypted using the following format:
```json
{
    "data": "base64-encoded-encrypted-response"
}
```

### Error Responses
Error responses follow the standard HTTP status codes and include a plain text error message in the response body.

## Technical Requirements

- **Go**: 1.23.3
- **Dependencies**:
  - `github.com/dgraph-io/badger/v3` (BadgerDB)
  - `github.com/google/uuid` (UUID generation)
  - `golang.org/x/net` (html handling)
  - Additional dependencies inferred from imports (e.g., `net/http`, `sync`).

## License

Saturday is licensed under the MIT License (assumed based on typical Go projects).