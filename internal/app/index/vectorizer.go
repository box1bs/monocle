package index

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

type Vectorizer struct {
	ctx    context.Context
	client *http.Client
}

type VecRequest struct {
	Words 	[]string 	`json:"words"`
}

type VecResponce struct {
	Vec 	[]float64 	`json:"vec"`
}

func (v *Vectorizer) Vectorize(text string) ([]float64, error) {
	tokens := strings.Split(text, " ")

	var buf bytes.Buffer
	json.NewEncoder(&buf).Encode(VecRequest{
		Words: tokens[:min(len(tokens), 512)],
	})

	req, err := http.NewRequestWithContext(v.ctx, http.MethodPost, "http://127.0.0.1:50920/vectorize", &buf)
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

func NewVectorizer(ctx context.Context) *Vectorizer {
	client := &http.Client{
		Timeout: 15 * time.Second,
	}
	return &Vectorizer{
		ctx:    ctx,
		client: client,
	}
}

func (v *Vectorizer) SetContext(parent context.Context, timeout time.Duration) {
	v.ctx, _ = context.WithTimeout(parent, timeout)
}