package handleTools

import (
	"encoding/json"
	"encoding/xml"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strings"

	"github.com/box1bs/Saturday/pkg/robots_parser"
	"github.com/google/uuid"
	"golang.org/x/net/html"
)

var urlRegex = regexp.MustCompile(`^https?://`)

type Document struct {
	Id      	uuid.UUID
	URL     	string
	Description string
	FullText 	string
	LineCount	int
	Score		float32
	WordsCount	int
}

func ParseHTMLStream(htmlContent, baseURL, userAgent string, maxLinks int, onlySameOrigin bool, rules *robots_parser.RobotsTxt) (description, fullText string, links []string, lineCount int) {
	tokenizer := html.NewTokenizer(strings.NewReader(htmlContent))
	var metaDesc, ogDesc, firstParagraph string
	var inParagraph, inScriptOrStyle bool
	var fullTextBuilder strings.Builder
	links = make([]string, 0, maxLinks)

	for {
		tokenType := tokenizer.Next()
		if tokenType == html.ErrorToken {
			if tokenizer.Err() == io.EOF {
				break
			}
			log.Println("error parsing HTML with url: " + baseURL)
			break
		}

		switch tokenType {
		case html.StartTagToken:
			t := tokenizer.Token()
			tagName := strings.ToLower(t.Data)
			switch tagName {
			case "meta":
				var isDesc, isOG bool
				var content string
				for _, attr := range t.Attr {
					key := strings.ToLower(attr.Key)
					val := attr.Val
					if key == "name" && strings.ToLower(val) == "description" {
						isDesc = true
					}
					if key == "property" && strings.ToLower(val) == "og:description" {
						isOG = true
					}
					if key == "content" {
						content = attr.Val
					}
				}
				if isDesc && content != "" && metaDesc == "" {
					metaDesc = content
				}
				if isOG && content != "" && ogDesc == "" {
					ogDesc = content
				}
			case "p", "div", "br", "h1", "h2", "h3", "h4", "h5", "h6", "li":
                lineCount++
                if firstParagraph == "" && tagName == "p" {
                    inParagraph = true
                }
			case "a":
				for _, attr := range t.Attr {
					if strings.ToLower(attr.Key) == "href" {
						link := MakeAbsoluteURL(baseURL, attr.Val)
						if link != "" && len(links) < maxLinks {
							if rules != nil {
								uri, _ := url.Parse(link)
								if !rules.IsAllowed(userAgent, uri.Path) {
									break
								}
							}
							if onlySameOrigin {
								same, err := isSameOrigin(link, baseURL)
								if err != nil {
									break
								}
								if same {
									links = append(links, link)
								}
								break
							}
							links = append(links, link)
						}
						break
					}
				}
			case "script", "style":
				inScriptOrStyle = true
			}
		case html.EndTagToken:
			t := tokenizer.Token()
			tagName := strings.ToLower(t.Data)
			if tagName == "p" && inParagraph {
				inParagraph = false
			} else if tagName == "script" || tagName == "style" {
				inScriptOrStyle = false
			}
		case html.TextToken:
			if inScriptOrStyle {
				continue
			}
			text := strings.TrimSpace(string(tokenizer.Text()))
			if text != "" {
				lineCount += strings.Count(text, "\n")
				
				fullTextBuilder.WriteString(text + " ")
				if inParagraph && firstParagraph == "" {
					firstParagraph += text + " "
				}
			}
		}
	}

	fullText = strings.TrimSpace(fullTextBuilder.String())
	if metaDesc != "" {
		description = metaDesc
	} else if ogDesc != "" {
		description = ogDesc
	} else {
		description = strings.TrimSpace(firstParagraph)
	}
	return
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