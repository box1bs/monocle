package validation

import (
	"errors"
	"net/url"
	"strings"
	"unicode/utf8"
)

type URLValidator struct {
	MaxURLLenght   int
	AllowedSchemes []string
}

var (
	ErrEmptyQuery     = errors.New("search query cannot be empty")
	ErrQueryTooLong   = errors.New("search query too long")
	ErrInvalidURL     = errors.New("invalid URL format")
	ErrUnsupportedURL = errors.New("unsupported URL scheme")
)

func NewURLValidator() *URLValidator {
	return &URLValidator{
		MaxURLLenght:   2048,
		AllowedSchemes: []string{"http", "https"},
	}
}

func (uv *URLValidator) ValidateURLs(uri string) error {
	if len(uri) > uv.MaxURLLenght {
		return errors.New("URL exceeds maximum length")
	}

	parsedURL, err := url.Parse(uri)
	if err != nil {
		return ErrInvalidURL
	}

	if !contains(uv.AllowedSchemes, parsedURL.Scheme) {
		return ErrUnsupportedURL
	}

	return nil
}

type QueryValidator struct {
	MaxQueryLength int
	MinQueryLength int
}

func NewQueryValidator() *QueryValidator {
	return &QueryValidator{
		MaxQueryLength: 256,
		MinQueryLength: 2,
	}
}

func (qv *QueryValidator) ValidateQuery(query string) error {
	query = strings.TrimSpace(query)
	if query == "" {
		return ErrEmptyQuery
	}

	length := utf8.RuneCountInString(query)
	if length < qv.MinQueryLength {
		return errors.New("search query is too short")
	}

	if length > qv.MaxQueryLength {
		return ErrQueryTooLong
	}

	return nil
}

func contains(slice []string, item string) bool {
	for _, v := range slice {
		if v == item {
			return true
		}
	}
	return false
}