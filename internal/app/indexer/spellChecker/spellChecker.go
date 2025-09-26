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

func (s *SpellChecker) BestReplacement(s1 string, candidates []string) string {
    st := []token{}
	for _, candidate := range candidates {
		distance := s.levenshteinDistance(s1, candidate)
		if len(st) == 0 || distance < st[len(st) - 1].score {
            st = append(st, token{
                s: candidate,
                score: distance,
            })
        }
	}
	return st[len(st) - 1].s
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

func (sc *SpellChecker) BreakToNGrams(word string) []string {
    runes := []rune(word)
    if len(runes) < sc.nGramCount {
        return nil
    }
    nGrams := make([]string, 0, len(runes) - sc.nGramCount + 1)
    for i := range len(runes) - sc.nGramCount + 1 {
        nGram := make([]rune, sc.nGramCount)
        copy(nGram, runes[i:i + sc.nGramCount])
        nGrams = append(nGrams, string(nGram))
    }
    return nGrams
}