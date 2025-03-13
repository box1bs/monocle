package handleTools

import (
	"encoding/json"
	"encoding/xml"
	"io"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strings"

	"github.com/google/uuid"
)

var urlRegex = regexp.MustCompile(`^https?://`)

type Document struct {
	Id      		uuid.UUID
	URL     		string
	Description 	string
	Words 			[]string
	PartOfFullSize 	float64
}

func (d *Document) ArchiveDocument() {
	d.PartOfFullSize = 256.0 / float64(len(d.Words))
	d.Words = d.Words[:256]
}

func (d *Document) GetFullSize() float64 {
	return float64(256.0 / d.PartOfFullSize)
}

func SameDomain(baseURL, href string) bool {
	same, _ := isSameOrigin(href, baseURL)
	return same
}

func MakeAbsoluteURL(baseURL, href string) string {
    if strings.HasPrefix(href, "http://") || strings.HasPrefix(href, "https://") {
        return href
    }
    
    if strings.HasPrefix(href, "/") {
        return baseURL + href
    }
    
    return ""
}

func NormalizeUrl(rawUrl string) (string, error) {
    uri := urlRegex.ReplaceAllString(rawUrl, "")
    
    parsedUrl, err := url.Parse(uri)
    if err != nil {
        return "", err
    }

	parsedUrl = cleanUTMParams(parsedUrl)
	parsedUrl.Host = strings.TrimPrefix(parsedUrl.Host, "www.")
    
    var normalized strings.Builder
    normalized.Grow(len(parsedUrl.Host) + len(parsedUrl.Path))
    normalized.WriteString(strings.ToLower(parsedUrl.Host))
    normalized.WriteString(strings.ToLower(parsedUrl.Path))
    
    result := normalized.String()
    return strings.TrimSuffix(result, "/"), nil
}

func cleanUTMParams(rawURL *url.URL) *url.URL {
	query := rawURL.Query()
	for key := range query {
		if strings.HasPrefix(key, "utm_") {
			query.Del(key)
		}
	}
	rawURL.RawQuery = query.Encode()
	return rawURL
}

func GetSitemapURLs(URL string, cli *http.Client, limiter int) ([]string, error) {
	resp, err := cli.Get(URL)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	return decodeSitemap(resp.Body, limiter)
}

func decodeSitemap(r io.Reader, limiter int) ([]string, error) {
	var urls []string
	dec := xml.NewDecoder(r)
	for {
		token, err := dec.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}

		if element, ok := token.(xml.StartElement); ok {
			if element.Name.Local == "loc" {
				var url string
				if err := dec.DecodeElement(&url, &element); err != nil {
					continue
				}
				urls = append(urls, url)
				if len(urls) >= limiter {
					return urls, nil
				}
			}
		}
	}

	return urls, nil
}

func isSameOrigin(rawURL, baseURL string) (bool, error) {
	parsedURL, err := url.Parse(rawURL)
	if err != nil {
		return false, err
	}

	parsedBaseURL, _ := url.Parse(baseURL)
	if !strings.Contains(parsedBaseURL.Hostname(), parsedURL.Hostname()) {
		return false, nil
	}
	return true, nil
}

type ConfigData struct {
	BaseURLs 		[]string	`json:"base_urls"`
	WorkersCount	int			`json:"worker_count"`
	TasksCount		int			`json:"task_count"`
	OnlySameDomain 	bool		`json:"only_same_domain"`
	MaxLinksInPage 	int			`json:"max_links_in_page"`
	MaxDepth 		int			`json:"max_depth_crawl"`
	Rate			int			`json:"rate"`
}

func UploadLocalConfiguration(fileName string) (*ConfigData, error) {
	file, err := os.Open(fileName)
	if err != nil {
		return nil, err
	}

	var cfg ConfigData
	if err := json.NewDecoder(file).Decode(&cfg); err != nil {
		return nil, err
	}

	return &cfg, err
}