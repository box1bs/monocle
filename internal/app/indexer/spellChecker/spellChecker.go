package spellChecker

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

//выбирается наименьшая(случайная, если все варинты имеют одинаковую длину) опция замены слова, поэтому надо имплементировать цепи маркова
func (s *SpellChecker) BestReplacement(s1 string, lenBeforeSpell int, candidates []string) string {
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

    scores := s.markovChains(nil, nil, nil, lenBeforeSpell, [2]string{"", ""}, st) //????
    bestScore := 0.0
    bestChoise := ""

    for i := range stackLen {
        if score := scores[i] / float64(1 + st[i].score); score > bestScore {
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

// если проблема во втором слове, первое надо записать в prev[1]
func (sc *SpellChecker) markovChains(treegram map[string]map[string]map[string]int, bigram map[string]map[string]int, unigram map[string]int, queryLenBeforeWord int, prev [2]string, condidates []token) []float64 {
    out := make([]float64, len(condidates))
    total := 0
    switch queryLenBeforeWord {
    case 0:
        for _, freq := range unigram {
            total += freq
        }
        for i, c := range condidates {
            out[i] = float64(unigram[c.s] + 1) / float64(total) / float64(1 + c.score)
        }

    case 1:
        for _, freq := range bigram[prev[1]] {
            total += freq
        }
        for i, c := range condidates {
            out[i] = float64(bigram[prev[1]][c.s] + 1) / float64(total) / float64(1 + c.score)
        }

    case 2:
        for _, freq := range treegram[prev[0]][prev[1]] {
            total += freq
        }
        for i, c := range condidates {
            out[i] = float64(treegram[prev[0]][prev[1]][c.s] + 1) / float64(total) / float64(1 + c.score)
        }

    default:
        panic("we don't do this here")
    }
    return out
}