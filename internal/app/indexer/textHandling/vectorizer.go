package textHandling

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

type vectorizer struct {
	client *http.Client
}

type VecResponce struct {
	Vec 	[][]float64 	`json:"vec"`
}

func (v *vectorizer) Vectorize(text string, ctx context.Context) ([][]float64, error) {
	var buf bytes.Buffer
	if err := json.NewEncoder(&buf).Encode(map[string]string{
		"text": text,
	}); err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "http://127.0.0.1:50920/vectorize", &buf)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := v.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
        return nil, fmt.Errorf("unexpected status: %v", resp.Status)
    }

	var vecResponce VecResponce
	if err := json.NewDecoder(resp.Body).Decode(&vecResponce); err != nil {
		return nil, err
	}
	
	return vecResponce.Vec, nil
}

func NewVectorizer() *vectorizer {
	client := &http.Client{
		Timeout: 15 * time.Second,
	}
	return &vectorizer{
		client: client,
	}
}