package spellChecker

//Использование триграмм отнимает слишком много оперативной памяти
type SpellChecker struct {
	maxTypo     int
    NGramCount  int
}

func NewSpellChecker(maxTypoLen, ngc int) *SpellChecker {
	return &SpellChecker{
		maxTypo: maxTypoLen,
        NGramCount: ngc,
	}
}

type token struct {
    s       string
    score   int
}

func (s *SpellChecker) BestReplacement(s1 string, candidates []string) string {
    if len(candidates) == 0 {
        return s1
    }

    st := []token{}
    orig := []rune(s1)
	for _, candidate := range candidates {
		distance := s.levenshteinDistance(orig, []rune(candidate))
        if distance <= s.maxTypo {
            return candidate
        }
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

    return st[stackLen - 1].s
}

func (s *SpellChecker) levenshteinDistance(word1 []rune, word2 []rune) int {
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