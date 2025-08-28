package textHandling

import (
	"bytes"
	"regexp"
	"sort"
	"strings"
	"unicode"
)

type tokenType int

const (
	WORD tokenType = iota
	NUMBER
	WHITESPACE
	SYMBOL
	ALPHANUMERIC
	UNKNOWN
	EMAIL_ADDR
	URL_ADDR
	IP_V4_ADDR
)

type token struct {
	Type  		tokenType
	Value 		string
	startPos 	int
	endPos  	int
}

type entityToken struct {
	token
	Priority int
}

type entityRule struct {
	Regex 		*regexp.Regexp
	TokenType 	tokenType
	Priority 	int
}

func newEntityRule(regex *regexp.Regexp, tokenType tokenType, priority int) *entityRule {
	return &entityRule{
		Regex: regex,
		TokenType: tokenType,
		Priority: priority,
	}
}

type tokenizer struct {
	rules []*entityRule
}

func complieEmailRegex() *regexp.Regexp {
	return regexp.MustCompile(`^[a-zA-Z0-9._%+-]+@[a-zA-Z0-9.-]+\.[a-zA-Z]{2,}$`)
}

func complieURLRegex() *regexp.Regexp {
	return regexp.MustCompile(`https?://[a-zA-Z0-9.-]+(?:\.[a-zA-Z]{2,})+/?[^\s]*`)
}

func compileIPV4Regex() *regexp.Regexp {
	octetRegex := `(25[0-5]|2[0-4][0-9]|[01]?[0-9][0-9]?)`
	return regexp.MustCompile(`\b` + octetRegex + `\.` + octetRegex + `\.` + octetRegex + `\.` + octetRegex + `\b`)
}

func getRuneType(r rune) tokenType {
	if unicode.IsLetter(r) {
		return WORD
	}
	if unicode.IsDigit(r) {
		return NUMBER
	}
	if unicode.IsSpace(r) {
		return WHITESPACE
	}
	if unicode.IsPunct(r) || unicode.IsSymbol(r) {
		return SYMBOL
	}
	return UNKNOWN
}

func (o *tokenizer) entityTokenize(input string) []token {
	var AllPotentialTokens []entityToken

	for _, rule := range o.rules {
		matches := rule.Regex.FindAllStringIndex(input, -1)
		for _, matchIndices := range matches {
			start := matchIndices[0]
			end := matchIndices[1]
			value := input[start:end]

			AllPotentialTokens = append(AllPotentialTokens, entityToken{
				token: token{
					Type:     rule.TokenType,
					Value:    value,
					startPos: start,
					endPos:   end,
				},
				Priority: rule.Priority,
			})
		}
	}

	sort.SliceStable(AllPotentialTokens, func(i, j int) bool {
		if AllPotentialTokens[i].startPos != AllPotentialTokens[j].startPos {
			return AllPotentialTokens[i].startPos < AllPotentialTokens[j].startPos
		}
		if AllPotentialTokens[i].Priority != AllPotentialTokens[j].Priority {
			return AllPotentialTokens[i].Priority > AllPotentialTokens[j].Priority
		}
		return AllPotentialTokens[i].startPos - AllPotentialTokens[i].endPos > AllPotentialTokens[j].startPos - AllPotentialTokens[j].endPos
	})

	var selectedEntityTokens []token
	lastSelectedPos := -1
	for _, pt := range AllPotentialTokens {
		if pt.startPos >= lastSelectedPos {
			selectedEntityTokens = append(selectedEntityTokens, pt.token)
			lastSelectedPos = pt.endPos
		}
	}

	var finalTokens []token
	currentTextPos := 0
	for _, entityToken := range selectedEntityTokens {
		if entityToken.startPos > currentTextPos {
			stdTokens := o.fragmentTokenize(input[currentTextPos:entityToken.startPos], currentTextPos)
			finalTokens = append(finalTokens, stdTokens...)
		}
		finalTokens = append(finalTokens, entityToken)
		currentTextPos = entityToken.endPos
	}

	if currentTextPos < len(input) {
		stdTokens := o.fragmentTokenize(input[currentTextPos:], currentTextPos)
		finalTokens = append(finalTokens, stdTokens...)
	}

	return finalTokens
}

func (o *tokenizer) fragmentTokenize(textFragment string, globalStartPos int) []token {
	var tokens []token
	var currentTokenBuffer bytes.Buffer
	var currentTokenType tokenType = UNKNOWN
	startPos := 0
	textFragment = strings.ToLower(textFragment)
	runes := []rune(textFragment)

	
	for i := range runes {
		r := runes[i]
		rType := getRuneType(r)

		if currentTokenBuffer.Len() == 0 {
			currentTokenBuffer.WriteRune(r)
			currentTokenType = rType
		} else {
			shouldCombineNow := false
			if (currentTokenType == WORD && rType == NUMBER) ||
				(currentTokenType == NUMBER && rType == WORD) {
				shouldCombineNow = true
			}

			isContinuingAlphanumeric := false
			if currentTokenType == ALPHANUMERIC && (rType == WORD || rType == NUMBER) {
				isContinuingAlphanumeric = true
			}

			if rType == currentTokenType || shouldCombineNow || isContinuingAlphanumeric {
				currentTokenBuffer.WriteRune(r)
				if shouldCombineNow {
					currentTokenType = ALPHANUMERIC
				}
			} else {
				if currentTokenBuffer.Len() > 0 {
					tokens = append(tokens, token{
						Value: currentTokenBuffer.String(),
						Type:  currentTokenType,
						startPos: globalStartPos + startPos,
						endPos:   globalStartPos + startPos + currentTokenBuffer.Len(),
					})
				}

				currentTokenBuffer.Reset()
				currentTokenBuffer.WriteRune(r)
				currentTokenType = rType
			}
		}
	}

	if currentTokenBuffer.Len() > 0 {
		tokens = append(tokens, token{
			Type:     currentTokenType,
			Value:    currentTokenBuffer.String(),
			startPos: startPos,
			endPos:   startPos + currentTokenBuffer.Len(),
		})
	}

	return tokens
}