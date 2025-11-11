package textHandling

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"time"
)

type Vectorizer struct {
	client 		*http.Client
	docQueue 	chan reqBody
	srvPath 	string
}

type VecResponce struct {
	Vec 	[][][]float64 	`json:"vec"`
}

type reqBody struct {
	Text 		string 				`json:"text"`
	out 		chan [][]float64 	`json:"-"`
}

const defCtxTime = 10 * time.Second
const BaseCanceledError = "context canceled"

func (v *Vectorizer) PutDocQuery(t string, ctx context.Context) <- chan [][]float64 {
	resChan := make(chan [][]float64, 1)
	select {
	case v.docQueue <- reqBody{Text: t, out: resChan}:
		return resChan
	case <-ctx.Done():
		close(resChan)
		return resChan
	}
}

func (v *Vectorizer) vectorize(reqData []reqBody) {
	var buf bytes.Buffer
	if err := json.NewEncoder(&buf).Encode(reqData); err != nil {
		return
	}

	ctx, c := context.WithTimeout(context.Background(), defCtxTime)
	defer c()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, v.srvPath + "/vectorize", &buf)
	if err != nil {
		return
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := v.client.Do(req)
	if err != nil {
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
        return
    }

	var vecResponce VecResponce
	if err := json.NewDecoder(resp.Body).Decode(&vecResponce); err != nil {
		return
	}
	
	for i, r := range reqData {
		r.out <- vecResponce.Vec[i]
	}
}

func (v *Vectorizer) CallRankModel(reqData []byte) (*http.Response, error) {
	ctx, c := context.WithTimeout(context.Background(), defCtxTime)
	defer c()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, v.srvPath + "/rank", bytes.NewBuffer(reqData))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")

	return v.client.Do(req)
}

func NewVectorizer(cap, tickerTime int, srvPath string) *Vectorizer {
	v := &Vectorizer{
		client: &http.Client{},
		docQueue: make(chan reqBody, cap),
		srvPath: srvPath,
	}

	go func() {
		t := time.NewTicker(time.Duration(tickerTime) * time.Millisecond)
		defer t.Stop()
		for range t.C {
			if len(v.docQueue) == 0 {
				continue
			}
			batchSize := len(v.docQueue)
			reqData := make([]reqBody, 0, batchSize)
			for range batchSize {
				select {
				case v, ok := <-v.docQueue:
					if !ok {
						return
					}
					reqData = append(reqData, v)
				default:
				}
			}
			if len(reqData) == 0 {
				continue
			}
			v.vectorize(reqData)
			//идея t в том чтобы между запросами проходила секунда, если t накапливается, то это условие не выполняется
			drain:
			for {
				select{
				case <-t.C:
				default:
					break drain
				}
			}
		}
	}()

	return v
}

func (v *Vectorizer) WaitForPythonServer(ctx context.Context) error {
	client := http.Client{Timeout: 3 * time.Second}
	it := time.NewTicker(10 * time.Second)
	req, err := http.NewRequest(http.MethodGet, v.srvPath + "/ping", nil)
	if err != nil {
		return err
	}
	for range it.C {
		select{
		case <-ctx.Done():
			return errors.New(BaseCanceledError)
		default:
			resp, err := client.Do(req)
			if err != nil || resp.StatusCode != 200 {
				continue
			}
			resp.Body.Close()
			it.Stop()
			return nil
		}
	}
	return nil
}

func (v *Vectorizer) Close() {
	close(v.docQueue)
}