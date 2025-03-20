package index

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"

	"github.com/google/uuid"
)

type modelRequest struct {
	QueryWords 	[]string			`json:"query"`
	Docs  		[]modelRequestData 	`json:"documents"`
}

type modelRequestData struct {
	Id 		string		`json:"id"`
	Words 	[]string	`json:"words"`
	Bm25	float64		`json:"bm25"`
	Tf_Idf	float64		`json:"tf_idf"`
}

type modelResponse struct {
	Id 		string 		`json:"id"`
	Score   float64 	`json:"score"`
}

func handleBinaryScore(mr modelRequest, rank map[uuid.UUID]*requestRanking) error {
	b, err := json.Marshal(mr)
	if err != nil {
		return err
	}

	req, err := http.NewRequest("POST", "http://0.0.0.0:8000/predict", bytes.NewBuffer(b))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	predictions := make([]modelResponse, 0, len(mr.Docs))
	
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
	/*
	for _, predict := range predictions {
		uid, err := uuid.Parse(predict.Id)
		if err != nil {
			return err
		}
		rank[uid].relation = predict.Score
	}*/

	return nil
}