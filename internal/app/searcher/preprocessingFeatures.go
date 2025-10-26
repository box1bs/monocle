package searcher

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"

	"github.com/box1bs/monocle/internal/model"
)

func calcCosineSimilarity(vec1, vec2 []float64) float64 {
	if len(vec1) != len(vec2) {
		return 0.0
	}

	dotProduct := 0.0
	magnitude1 := 0.0
	magnitude2 := 0.0

	for i := range vec1 {
		dotProduct += vec1[i] * vec2[i]
		magnitude1 += vec1[i] * vec1[i]
		magnitude2 += vec2[i] * vec2[i]
	}

	if magnitude1 == 0 || magnitude2 == 0 {
		return 0.0
	}

	return dotProduct / (math.Sqrt(magnitude1) * math.Sqrt(magnitude2))
}

func calcEuclidianDistance(v1, v2 []float64) float64 {
	tmp := 0.0
	for i := range v1 {
		tmp += math.Pow(v2[i]-v1[i], 2.0)
	}
	return math.Sqrt(tmp)
}

func culcBM25(idf float64, tf float64, doc *model.Document, avgLen float64) float64 {
	k1 := 1.2
	b := 0.75
	return idf * (tf * (k1 + 1)) / (tf + k1*(1-b+b*float64(doc.WordCount)/avgLen))
}

func getMinQueryDistInDoc(positions []*[]model.Position, lenQuery int) int {
	minDencity := math.MaxInt

	bs := func(cur, target int) int {
		arr := *positions[cur]
		l, r := 0, len(arr)
		for l < r {
			m := (l + r) / 2
			if arr[m].I <= target {
				l = m + 1
			} else {
				r = m
			}
		}
		return l
	}

	for _, startPos := range *positions[0] {
		cur := startPos.I
		last := cur
		valid := true
		for j := 1; j < lenQuery; j++ {
			arr := *positions[j]
			if len(arr) == 0 {
				return minDencity //minDencity не успеет измениться до возврата
			}
			position := bs(j, last)
			if position >= len(arr) {
				valid = false
				break
			}
			last = arr[position].I
		}
		if !valid || last-cur < 0 {
			continue
		}
		minDencity = min(minDencity, last-cur)
	}

	return minDencity
}

type rankingResponse struct {
	Relevances []float64 `json:"rel"`
}

func callRankAPI(conds [][32]byte, features map[[32]byte]requestRanking) (int, error) {
	var requestBody []requestRanking
	for _, c := range conds {
		requestBody = append(requestBody, features[c])
	}

	jsonData, err := json.Marshal(requestBody)
	if err != nil {
		return 0, fmt.Errorf("failed to marshal request body: %w", err)
	}

	resp, err := http.Post("http://localhost:50920/rank", "application/json", bytes.NewBuffer(jsonData))
	if err != nil {
		return 0, fmt.Errorf("http post request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return 0, fmt.Errorf("rank api returned non-200 status: %d, body: %s", resp.StatusCode, string(bodyBytes))
	}

	respData := rankingResponse{}
	if err := json.NewDecoder(resp.Body).Decode(&respData); err != nil {
		return 0, fmt.Errorf("failed to decode response body: %w", err)
	}

	for i := range respData.Relevances {
		if respData.Relevances[i] > 0.0 {
			return i, nil
		}
	}

	return 0, fmt.Errorf("somehow all candidates has been received with 0")
}

func boyerMoorAlgorithm(url string, queryWords []string) (bool, float64) {
	wordInUrl := 0.0
	urlRunes := []rune(url)
	ul := len(urlRunes)
	for _, word := range queryWords {
		queryWord := []rune(word)
		l := len(queryWord)
		shift := map[rune]int{}
		for i, r := range word {
			shift[r] = max(1, l-i-1)
		}

		strp := l - 1
		sstrp := l - 1
		for strp < ul {
			if queryWord[sstrp] != urlRunes[strp] {
				if sh, ex := shift[urlRunes[strp]]; ex {
					strp += sh
				} else {
					strp += l
				}
			} else {
				tmp := l // чтобы если отрезки не равны не вернуться в ту же точку
				for sstrp >= 0 && queryWord[sstrp] == urlRunes[strp] {
					tmp++
					sstrp--
					strp--
				}
				if sstrp == -1 {
					wordInUrl += float64(l)
					break
				} else {
					strp += tmp
					sstrp = l - 1
				}
			}
		}
	}

	return wordInUrl > 0.0, math.Log(1.0 + wordInUrl)
}