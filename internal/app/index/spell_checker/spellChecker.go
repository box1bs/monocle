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
	minDist := int(1e9)
	fixed := make(map[string]int)
	for _, s2 := range dict {
		distance := s.wagnerFisherAlgorithm(s1, s2)
		if minDist > distance {
			minDist = distance
			fixed[s2] = distance
		}
	}
	if minDist == 1e9 {
		return ""
	}
	for k, v := range fixed {
		if v != minDist {
			delete(fixed, k)
		}
	}
	return s.checkFixProbability(fixed, s1)
}

func (s *SpellChecker) checkFixProbability(fixes map[string]int, orig string) string {
	//if len(fixes) == 1 {
		for k := range fixes {
			return k
		}
	//}
	return ""
}

func (s *SpellChecker) wagnerFisherAlgorithm(s1, s2 string) int {
	w1, w2 := len(s1), len(s1)
    dp := make([][]int, w1+1)
    for i := range w1+1 {
        dp[i] = make([]int, w2+1)
        dp[i][0] = i
    }
    for j := range w2+1 {
        dp[0][j] = j
    }
    for i := range w1 {
        for j := range w2 {
            if s1[i] == s2[j] {
                dp[i+1][j+1] = dp[i][j]
            } else {
                dp[i+1][j+1] = 1 + min(dp[i][j], min(dp[i+1][j], dp[i][j+1]))
            }
			if dp[i+1][j+1] > s.maxTypo {
				return 1e9
			}
        }
    }
    return dp[w1][w2]
}