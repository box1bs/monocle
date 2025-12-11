package indexer

import (
	"hash/fnv"
	"math/rand"
	"strings"
)

const (
	nGramSize = 4
	prime = (1 << 61) - 1 // 61 единица в бинарном представлении
)

type minHash struct {
	a, b [128]uint64
}

func NewHasher(rsid *rand.Rand, a, b *[128]uint64) *minHash {
	if a == nil || b == nil {
		a = &[128]uint64{}
		b = &[128]uint64{}

		for i := range 128 {
			a[i] = uint64(rsid.Int63n(int64(prime - 1))) + 1
			b[i] = uint64(rsid.Int63n(int64(prime)))
		}
	}

	return &minHash{a: *a, b: *b}
}

func (mh *minHash) CreateSignature(rawTokens []string) [128]uint64 {
	shingles := getWordNGrams(rawTokens)
	sign := [128]uint64{}
	for i := range 128 {
		sign[i] = ^uint64(0)
	}

	for _, shingle := range shingles {
		hash := hash64(shingle)
		for i := range 128 {
			x := mh.a[i] * hash + mh.b[i]
			x %= prime
			if x < sign[i] {
				sign[i] = x
			}
		}
	}
	return sign
}

func getWordNGrams(rawTokens []string) []string {
	result := []string{}
	for i := 0; i <= len(rawTokens) - nGramSize; i++ {
		result = append(result, strings.Join(rawTokens[i: i + nGramSize], ""))
	}
	return result
}

func hash64(s string) uint64 {
	h := fnv.New64a()
	h.Write([]byte(s))
	return h.Sum64()
}