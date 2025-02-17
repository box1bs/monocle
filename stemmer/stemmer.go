package stemmer

import "strings"

type Stemmer interface {
	Stem(string) string
}

type EnglishStemmer struct {
	step1aRules map[string]string
	step1bRules map[string]string
	step2Rules  map[string]string
	step3Rules  map[string]string
	step4Rules  map[string]string
}

type StopWords struct {
	words map[string]struct{}
}

func NewStopWords() *StopWords {
    sw := &StopWords{
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

func (sw *StopWords) IsStopWord(word string) bool {
    _, exists := sw.words[strings.ToLower(word)]
    return exists
}

func NewEnglishStemmer() *EnglishStemmer {
	return &EnglishStemmer{
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
	}
}

func (s *EnglishStemmer) hasSuffix(word, suffix string) bool {
	return len(word) > len(suffix) && strings.HasSuffix(word, suffix)
}

func (s *EnglishStemmer) measure(word string) int {
    isVowel := func(c byte) bool {
        return strings.ContainsRune("aeiou", rune(c))
    }
    
    var m int
    var hasVowel bool
    
    for i := 0; i < len(word); i++ {
        if isVowel(word[i]) {
            hasVowel = true
        } else if hasVowel {
            m++
            hasVowel = false
        }
    }
    
    return m
}

func (s *EnglishStemmer) Stem(word string) string {
    if len(word) <= 2 {
        return word
    }
    
    // Шаг 1a: обработка множественного числа
    for suffix, replacement := range s.step1aRules {
        if s.hasSuffix(word, suffix) {
            word = strings.TrimSuffix(word, suffix) + replacement
            break
        }
    }
    
    // Шаг 1b: обработка глагольных форм
    for suffix, replacement := range s.step1bRules {
        if s.hasSuffix(word, suffix) && s.measure(strings.TrimSuffix(word, suffix)) > 0 {
            word = strings.TrimSuffix(word, suffix) + replacement
            break
        }
    }
    
    // Шаг 2: преобразование суффиксов
    for suffix, replacement := range s.step2Rules {
        if s.hasSuffix(word, suffix) && s.measure(strings.TrimSuffix(word, suffix)) > 0 {
            word = strings.TrimSuffix(word, suffix) + replacement
            break
        }
    }
    
    // Шаг 3: дальнейшее преобразование суффиксов
    for suffix, replacement := range s.step3Rules {
        if s.hasSuffix(word, suffix) && s.measure(strings.TrimSuffix(word, suffix)) > 0 {
            word = strings.TrimSuffix(word, suffix) + replacement
            break
        }
    }
    
    // Шаг 4: удаление длинных суффиксов
    for suffix, replacement := range s.step4Rules {
        if s.hasSuffix(word, suffix) && s.measure(strings.TrimSuffix(word, suffix)) > 1 {
            word = strings.TrimSuffix(word, suffix) + replacement
            break
        }
    }
    
    return word
}