package textHandling

import "strings"

type englishStemmer struct {
	step1aRules map[string]string
	step1bRules map[string]string
	step2Rules  map[string]string
	step3Rules  map[string]string
	step4Rules  map[string]string
	stopWords 	*stopWords
	tokenizer   *tokenizer
}

type stopWords struct {
	words map[string]struct{}
}

func newStopWords() *stopWords {
    sw := &stopWords{
        words: make(map[string]struct{}),
    }
    
    englishStops := []string{
        "a", "an", "and", "are", "as", "at", "be", "by", "for", "from",
        "has", "he", "in", "is", "it", "its", "of", "on", "that", "the",
        "to", "was", "were", "will", "with", "the", "this", "but", "they",
        "have", "had", "what", "when", "where", "who", "which", "why",
        "how", "all", "any", "both", "each", "few", "more", "most",
        "other", "some", "such", "no", "nor", "not", "only", "own",
        "same", "so", "than", "too", "very",
    }
    
    for _, word := range englishStops {
        sw.words[word] = struct{}{}
    }
    return sw
}

func (sw *stopWords) isStopWord(word string) bool {
    _, exists := sw.words[strings.ToLower(word)]
    return exists
}

func NewEnglishStemmer() *englishStemmer {
	return &englishStemmer{
		step1aRules: map[string]string{
			"sses": "ss", // possesses -> possess
			"ies":  "i",  // ponies -> poni
			"ss":   "ss", // possess -> possess
			"s":    "",   // cats -> cat
		},
		step1bRules: map[string]string{
			"eed": "ee", // agreed -> agree
			"ed":  "",   // jumped -> jump
			"ing": "",   // jumping -> jump
		},
		step2Rules: map[string]string{
			"ational": "ate",  // relational -> relate
			"tional":  "tion", // conditional -> condition
			"enci":    "ence", // valenci -> valence
			"anci":    "ance", // hesitanci -> hesitance
			"izer":    "ize",  // digitizer -> digitize
			"abli":    "able", // conformabli -> conformable
			"alli":    "al",   // radically -> radical
			"entli":   "ent",  // differently -> different
			"eli":     "e",    // namely -> name
			"ousli":   "ous",  // analogously -> analogous
			"ization": "ize",  // visualization -> visualize
			"ation":   "ate",  // predication -> predicate
			"ator":    "ate",  // operator -> operate
			"alism":   "al",   // feudalism -> feudal
			"iveness": "ive",  // decisiveness -> decisive
			"fulness": "ful",  // hopefulness -> hopeful
			"ousness": "ous",  // callousness -> callous
			"aliti":   "al",   // formality -> formal
			"iviti":   "ive",  // sensitivity -> sensitive
			"biliti":  "ble",  // sensibility -> sensible
		},
		step3Rules: map[string]string{
			"icate": "ic", // certificate -> certific
			"ative": "",   // formative -> form
			"alize": "al", // formalize -> formal
			"iciti": "ic", // electricity -> electric
			"ical":  "ic", // electrical -> electric
			"ful":   "",   // hopeful -> hope
			"ness":  "",   // goodness -> good
		},
		step4Rules: map[string]string{
			"al":    "", // revival -> reviv
			"ance":  "", // allowance -> allow
			"ence":  "", // inference -> infer
			"er":    "", // airliner -> airlin
			"ic":    "", // gyroscopic -> gyroscop
			"able":  "", // adjustable -> adjust
			"ible":  "", // defensible -> defens
			"ant":   "", // contestant -> contest
			"ement": "", // replacement -> replac
			"ment":  "", // adjustment -> adjust
			"ent":   "", // dependent -> depend
			"ion":   "", // adoption -> adopt
			"ou":    "", // homologou -> homolog
			"ism":   "", // mechanism -> mechan
			"ate":   "", // activate -> activ
			"iti":   "", // angulariti -> angular
			"ous":   "", // homologous -> homolog
			"ive":   "", // effective -> effect
			"ize":   "", // bowdlerize -> bowdler
		},
		stopWords: newStopWords(),
		tokenizer: &tokenizer{
			rules: []*entityRule{
				newEntityRule(complieEmailRegex(), EMAIL_ADDR, 2),
				newEntityRule(compileIPV4Regex(), IP_V4_ADDR, 1),
				newEntityRule(complieURLRegex(), URL_ADDR, 2),
			},
		},
	}
}

func (s *englishStemmer) hasSuffix(word, suffix string) bool {
	return len(word) > len(suffix) && strings.HasSuffix(word, suffix)
}

func (s *englishStemmer) measure(word string) int {
    isVowel := func(c byte) bool {
        return strings.ContainsRune("aeiou", rune(c))
    }
    
    var m int
    var hasVowel bool
    
    for i := range len(word) {
        if isVowel(word[i]) {
            hasVowel = true
        } else if hasVowel {
            m++
            hasVowel = false
        }
    }
    
    return m
}

func (s *englishStemmer) TokenizeAndStem(text string) []string {
	tokens := s.tokenizer.entityTokenize(text)

	stemmedTokens := []string{}
	for _, token := range tokens {
		if token.Type == WORD || token.Type == ALPHANUMERIC {
			stemmedTokens = append(stemmedTokens, s.stem(token.Value))
		} else if token.Type != WHITESPACE {
			stemmedTokens = append(stemmedTokens, token.Value)
		}
	}

	return stemmedTokens
}

func (s *englishStemmer) stem(word string) string {
	if s.stopWords.isStopWord(word) {
		return ""
	}

    if len(word) <= 2 {
        return word
    }
    
    for suffix, replacement := range s.step1aRules {
        if s.hasSuffix(word, suffix) {
            word = strings.TrimSuffix(word, suffix) + replacement
            break
        }
    }
    
    for suffix, replacement := range s.step1bRules {
        if s.hasSuffix(word, suffix) && s.measure(strings.TrimSuffix(word, suffix)) > 0 {
            word = strings.TrimSuffix(word, suffix) + replacement
            break
        }
    }
    
    for suffix, replacement := range s.step2Rules {
        if s.hasSuffix(word, suffix) && s.measure(strings.TrimSuffix(word, suffix)) > 0 {
            word = strings.TrimSuffix(word, suffix) + replacement
            break
        }
    }
    
    for suffix, replacement := range s.step3Rules {
        if s.hasSuffix(word, suffix) && s.measure(strings.TrimSuffix(word, suffix)) > 0 {
            word = strings.TrimSuffix(word, suffix) + replacement
            break
        }
    }
    
    for suffix, replacement := range s.step4Rules {
        if s.hasSuffix(word, suffix) && s.measure(strings.TrimSuffix(word, suffix)) > 1 {
            word = strings.TrimSuffix(word, suffix) + replacement
            break
        }
    }
    
    return word
}