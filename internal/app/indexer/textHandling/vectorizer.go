package textHandling

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"time"
)

type Vectorizer struct {
	client 		*http.Client
	docQueue 	chan reqBody
}

type VecResponce struct {
	Vec 	[][][]float64 	`json:"vec"`
}

type reqBody struct {
	Text 		string 				`json:"text"`
	out 		chan [][]float64 	`json:"-"`
	localCTX 	context.Context 	`json:"-"`
}

func (v *Vectorizer) PutDocQuery(t string, ctx context.Context) <- chan [][]float64 {
	resChan := make(chan [][]float64, 1)
	select {
	case v.docQueue <- reqBody{Text: t, out: resChan, localCTX: ctx}:
		return resChan
	case <-ctx.Done():
		return resChan
	}
}

func (v *Vectorizer) vectorize(reqData []reqBody) {
	var buf bytes.Buffer
	if err := json.NewEncoder(&buf).Encode(reqData); err != nil {
		return
	}

	ctx, c := context.WithTimeout(context.Background(), 10 * time.Second)
	defer c()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "http://127.0.0.1:50920/vectorize", &buf)
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
		select{
		case <-r.localCTX.Done():
			close(r.out)
			continue
		default:
		}
		r.out <- vecResponce.Vec[i]
	}
}

func NewVectorizer(ctx context.Context, cap int) *Vectorizer {
	v := &Vectorizer{
		client: &http.Client{},
		docQueue: make(chan reqBody, cap),
	}

	go func() {
		t := time.NewTicker(1 * time.Second)
		defer t.Stop()
		for range t.C {
			select {
			case <-ctx.Done():
				return
			default:
				if len(v.docQueue) < 1 {
					continue
				}
				batchSize := len(v.docQueue)
				reqData := make([]reqBody, 0, batchSize)
				for range batchSize {
					select {
					case <-ctx.Done():
						return
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
			}
		}
	}()

	return v
}

func (v *Vectorizer) Close() {
	close(v.docQueue)
}