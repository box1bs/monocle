package spellChecker

type SpellChecker struct {
	maxTypo int
}

func NewSpellChecker(maxTypoLen int) *SpellChecker {
	return &SpellChecker{
		maxTypo: maxTypoLen,
	}
}

func (s *SpellChecker) BestReplacement(s1 string, dict []string) string {
	for _, s2 := range dict {
		distance := s.levenshteinDistance(s1, s2)
		if s.maxTypo >= distance {
			return s2
		}
	}
	return ""
}

func (s *SpellChecker) levenshteinDistance(word1 string, word2 string) int {
    w1, w2 := len(word1), len(word2)
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