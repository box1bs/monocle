package spellChecker

//Использование триграмм отнимает слишком много оперативной памяти
type SpellChecker struct {
	maxTypo     int
    nGramCount  int
}

func NewSpellChecker(maxTypoLen, nGramCount int) *SpellChecker {
	return &SpellChecker{
		maxTypo: maxTypoLen,
        nGramCount: nGramCount,
	}
}

type token struct {
    s       string
    score   int
}

func (s *SpellChecker) BestReplacement(s1 string, wordsBefore string, candidates []string, f func(...string) (int, error)) string {
    if len(candidates) == 0 {
        return s1
    }

    st := []token{}
	for _, candidate := range candidates {
		distance := s.levenshteinDistance(s1, candidate)
		if len(st) == 0 || distance <= st[len(st) - 1].score {
            st = append(st, token{
                s: candidate,
                score: distance,
            })
        }
	}

    stackLen := len(st)
    if st[stackLen - 1].score > s.maxTypo {
        return s1
    }

    scores := s.markovChains(wordsBefore, st, f)
    bestScore := 0.0
    bestChoise := ""

    for i := range stackLen {
        if score := scores[i] / float64(1 + st[i].score); score > bestScore {
            bestScore = score
            bestChoise = st[i].s
        }
    }
    
    return bestChoise
}

func (s *SpellChecker) levenshteinDistance(word1 string, word2 string) int {
    w1, w2 := len(word1), len(word2)
    if w1 - w2 > s.maxTypo || w2 - w1 > s.maxTypo {
        return s.maxTypo + 1
    }
    dp := make([][]int, w1 + 1)
    for i := range w1 + 1 {
        dp[i] = make([]int, w2 + 1)
    }

    for i := 1; i <= w1; i++ {
        dp[i][0] = i
    }
    for j := 1; j <= w2; j++ {
        dp[0][j] = j
    }

    for i := 1; i <= w1; i++ {
        for j := 1; j <= w2; j++ {
            if word1[i - 1] == word2[j - 1] {
                dp[i][j] = dp[i - 1][j - 1]
            } else {
                insert := dp[i - 1][j]
                delete := dp[i][j - 1]
                replace := dp[i - 1][j - 1]
                dp[i][j] = min(delete, insert, replace) + 1
            }
        }
    }

    return dp[w1][w2]
}

func (s *SpellChecker) BreakToNGrams(word string) []string {
    runes := []rune(word)
    if len(runes) < s.nGramCount {
        return nil
    }
    nGrams := make([]string, 0, len(runes) - s.nGramCount + 1)
    for i := range len(runes) - s.nGramCount + 1 {
        nGram := make([]rune, s.nGramCount)
        copy(nGram, runes[i:i + s.nGramCount])
        nGrams = append(nGrams, string(nGram))
    }
    return nGrams
}

func (s *SpellChecker) markovChains(prev string, condidates []token, f func(...string) (int, error)) []float64 {
    out := make([]float64, len(condidates))
    total := 0
    if prev == "" {
        cnts := []int{}
        for _, t := range condidates {
            c, err := f(t.s)
            if err != nil {
                return nil
            }
            total += c
            cnts = append(cnts, c)
        }
        for i, c := range condidates {
            out[i] = float64(cnts[i] + 1) / float64(total) / float64(1 + c.score)
        }
    } else {
        cnts := []int{}
        for _, t := range condidates {
            c, err := f(prev, t.s)
            if err != nil {
                return nil
            }
            total += c
            cnts = append(cnts, c)
        }
        for i, c := range condidates {
            out[i] = float64(cnts[i] + 1) / float64(total) / float64(1 + c.score)
        }
    }
    return out
}