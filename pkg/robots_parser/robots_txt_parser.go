package robots_parser

import (
	"fmt"
	"io"
	"net/http"
	"strings"
)

type Rule struct {
	Allow    []string
	Disallow []string
}

type RobotsTxt struct {
	Rules map[string][]Rule
}

func ParseRobotsTxt(content string) *RobotsTxt {
    robots := &RobotsTxt{Rules: make(map[string][]Rule)}
    var currentAgent string
    var currentRule *Rule

    lines := strings.Split(content, "\n")
    for _, line := range lines {
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
            currentRule = &Rule{Allow: []string{}, Disallow: []string{}}
            robots.Rules[currentAgent] = append(robots.Rules[currentAgent], *currentRule)
        case "allow:":
            if currentRule != nil {
                currentRule.Allow = append(currentRule.Allow, value)
            }
        case "disallow:":
            if currentRule != nil {
                currentRule.Disallow = append(currentRule.Disallow, value)
            }
        }
    }

    return robots
}

func (r *RobotsTxt) IsAllowed(userAgent, url string) bool {
    if rules, ok := r.Rules[userAgent]; ok {
        for _, rule := range rules {
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
        }
        return true
    }

    if rules, ok := r.Rules["*"]; ok {
        for _, rule := range rules {
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
        }
        return true
    }

    return true
}

func FetchRobotsTxt(url string) (string, error) {
    resp, err := http.Get(url + "/robots.txt")
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