package parser

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

type Rule struct {
	Allow    []string
	Disallow []string
    Delay    int
}

type RobotsTxt struct {
	Rules map[string]Rule
}

func ParseRobotsTxt(content string) *RobotsTxt {
    robots := &RobotsTxt{Rules: make(map[string]Rule)}
    var currentAgent string
    var currentRule *Rule

    lines := strings.SplitSeq(content, "\n")
    for line := range lines {
        line = strings.TrimSpace(line)
        if len(line) == 0 || strings.HasPrefix(line, "#") {
            continue
        }

        parts := strings.Fields(line)
        if len(parts) < 2 {
            continue
        }

        directive := strings.ToLower(parts[0])
        value := parts[1]

        switch directive {
        case "user-agent:":
            currentAgent = value
            if _, ex := robots.Rules[currentAgent]; !ex {
                currentRule = &Rule{Allow: []string{}, Disallow: []string{}}
            }
            robots.Rules[currentAgent] = *currentRule
        case "allow:":
            if currentRule != nil {
                currentRule.Allow = append(currentRule.Allow, value)
            }
        case "disallow:":
            if currentRule != nil {
                currentRule.Disallow = append(currentRule.Disallow, value)
            }
        case "crawl-delay:":
            if currentRule != nil {
                currentRule.Delay, _ = strconv.Atoi(value)
            }
        }
    }

    return robots
}

func (r *RobotsTxt) IsAllowed(userAgent, url string) bool {
    if rule, ok := r.Rules[userAgent]; ok {
        for _, disallow := range rule.Disallow {
            if strings.HasPrefix(url, disallow) {
                return false
            }
        }
        for _, allow := range rule.Allow {
            if strings.HasPrefix(url, allow) {
                return true
            }
        }
        return true
    }

    if rule, ok := r.Rules["*"]; ok {
        for _, disallow := range rule.Disallow {
            if strings.HasPrefix(url, disallow) {
                return false
            }
        }
        for _, allow := range rule.Allow {
            if strings.HasPrefix(url, allow) {
                return true
            }
        }
        return true
    }

    return true
}

func FetchRobotsTxt(ctx context.Context, url string, cli *http.Client) (string, error) {
    c, cancel := context.WithTimeout(ctx, time.Second * 3)
    defer cancel()
    req, err := http.NewRequestWithContext(c, "GET", url + "/robots.txt", nil)
    if err != nil {
        return "", err
    }

    resp, err := cli.Do(req)
    if err != nil {
        return "", err
    }
    defer resp.Body.Close()

    if resp.StatusCode != http.StatusOK {
        return "", fmt.Errorf("invalid status code: %s", resp.Status)
    }

    body, err := io.ReadAll(resp.Body)
    if err != nil {
        return "", err
    }

    return string(body), nil
}