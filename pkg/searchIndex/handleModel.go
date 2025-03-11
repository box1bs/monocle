package searchIndex

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"

	handle "github.com/box1bs/Saturday/pkg/handleTools"
)

type modelRequest struct {
	Query string	`json:"query"`
	Docs  []string 	`json:"documents"`
}

type modelResponse struct {
	Document string 	`json:"document"`
	Score    float64 	`json:"score"`
}

type relation struct {
	Doc 	*handle.Document
	Score 	float64
}

func handleBinaryScore(query string, docs []*handle.Document) ([]relation, error) {
	cash := make(map[string]*handle.Document)
	mr := modelRequest{Query: query, Docs: make([]string, 0, len(docs))}

	for _, doc := range docs {
		cash[doc.FullText] = doc
		mr.Docs = append(mr.Docs, doc.FullText)
	}

	b, err := json.Marshal(mr)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequest("POST", "http://0.0.0.0:8080/predict", bytes.NewBuffer(b))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")

	predictions := make([]modelResponse, 0, len(docs))
	
	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	if err := json.Unmarshal(body, &predictions); err != nil {
		return nil, err
	}

	predicted := make([]relation, 0)
	for _, predict := range predictions {
		predicted = append(predicted, relation{Doc: cash[predict.Document], Score: predict.Score})
	}

	return predicted, nil
}