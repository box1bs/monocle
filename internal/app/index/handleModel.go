package searchIndex

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"strings"

	"github.com/box1bs/Saturday/internal/model"
	"github.com/google/uuid"
)

type modelRequest struct {
	QueryWords 	[]string	`json:"query"`
	Docs  	[]modelRequestData 	`json:"documents"`
}

type modelRequestData struct {
	Words 	[]string	`json:"words"`
	Bm25	float64		`json:"bm25"`
	Tf_Idf	float64		`json:"tf_idf"`
}

type modelResponse struct {
	Document string 	`json:"document"`
	Score    float64 	`json:"score"`
}

func handleBinaryScore(query []string, docs []*model.Document, rank map[uuid.UUID]*requestRanking) error {
	cash := make(map[string]*model.Document)
	mr := modelRequest{
		QueryWords: query, 
		Docs: make([]modelRequestData, 0),
	}

	for _, doc := range docs {
		cash[strings.Join(doc.Words, " ")] = doc
		mr.Docs = append(mr.Docs, modelRequestData{
			Words: doc.Words,
			Bm25: rank[doc.Id].bm25,
			Tf_Idf: rank[doc.Id].tf_idf,
		})
	}
	b, err := json.Marshal(mr)
	if err != nil {
		return err
	}

	req, err := http.NewRequest("POST", "http://0.0.0.0:8000/predict", bytes.NewBuffer(b))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	predictions := make([]modelResponse, 0, len(docs))
	
	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}

	if err := json.Unmarshal(body, &predictions); err != nil {
		return err
	}

	for _, predict := range predictions {
		rank[cash[predict.Document].Id].relation = predict.Score
	}

	return nil
}