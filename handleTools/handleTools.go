package handleTools

import (
	"encoding/xml"
	"io"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strings"

	"golang.org/x/net/html"
	"github.com/google/uuid"
)

var urlRegex = regexp.MustCompile(`^https?://`)

type Document struct {
	Id      	uuid.UUID
	URL     	string
	Description string
	Score		float32
}

func ParseHTMLStream(htmlContent, baseURL string, maxLinks int) (description, fullText string, links []string) {
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
			//
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
			case "p":
				if firstParagraph == "" {
					inParagraph = true
				}
			case "a":
				for _, attr := range t.Attr {
					if strings.ToLower(attr.Key) == "href" {
						link := MakeAbsoluteURL(baseURL, attr.Val)
						if link != "" && len(links) < maxLinks {
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
    
    var normalized strings.Builder
    normalized.Grow(len(parsedUrl.Host) + len(parsedUrl.Path))
    normalized.WriteString(strings.ToLower(parsedUrl.Host))
    normalized.WriteString(strings.ToLower(parsedUrl.Path))
    
    result := normalized.String()
    return strings.TrimSuffix(result, "/"), nil
}

func GetLocalConfigUrls(path string) ([]string, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	return decodeSitemap(file, 20)
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