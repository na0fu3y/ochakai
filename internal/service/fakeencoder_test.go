package service

import (
	"context"
	"hash/fnv"
	"math"
	"strings"
	"unicode"

	"github.com/na0fu3y/ochakai/internal/embed"
)

// A stand-in encoder, so the configuration every Google Cloud deployment
// actually runs can be measured.
//
// The eval harness beside this file measured the lexical half alone,
// because the vector half needed a network call to Vertex AI and a test
// suite does not make one. That left the *default* configuration
// unmeasured (design doc 0080 §1.1: embeddings are on unless a
// deployment turns them off), which is the half of the product where a
// ranking change is least visible by reading the code.
//
// **What this does and does not stand in for.** It is a hashing bag of
// terms: the same windows and words the lexical side cuts a text into,
// hashed into a fixed-width vector and normalized, so cosine similarity
// rises with shared vocabulary. That makes it a real second ranking with
// real ties, real disagreements with the lexical list, and real
// behaviour under fusion — which is what the fused numbers measure.
//
// It is **not** a model, and no number produced with it predicts what a
// trained encoder would rank. A real encoder's whole value is matching
// text that shares no vocabulary at all, and this shares the lexical
// side's blind spot exactly there. So the harness reports both halves
// and names which is which: the lexical number is quality, the fused
// number is the arithmetic of the merge.
type fakeEncoder struct{ dim int }

func (f fakeEncoder) Model() string { return "fake-encoder-v1" }

func (f fakeEncoder) Embed(_ context.Context, task embed.Task, texts []string) ([][]float32, error) {
	out := make([][]float32, len(texts))
	for i, t := range texts {
		out[i] = f.vector(t, task)
	}
	return out, nil
}

// vector hashes each term into one dimension and normalizes, so two texts
// sharing terms have a cosine near their overlap. The task type is
// deliberately not read: an asymmetric model would place a query and a
// document differently, and pretending to do that with a hash would put
// a difference in the numbers that stands for nothing.
func (f fakeEncoder) vector(text string, _ embed.Task) []float32 {
	v := make([]float32, f.dim)
	for _, term := range fakeTerms(text) {
		h := fnv.New32a()
		_, _ = h.Write([]byte(term))
		v[int(h.Sum32())%f.dim]++
	}
	var norm float64
	for _, x := range v {
		norm += float64(x) * float64(x)
	}
	if norm == 0 {
		// A text with no terms still needs a unit vector: pgvector's
		// cosine distance is undefined against a zero vector, and a
		// document that silently stopped being comparable is exactly the
		// failure this harness exists to catch.
		v[0] = 1
		return v
	}
	norm = math.Sqrt(norm)
	for i := range v {
		v[i] = float32(float64(v[i]) / norm)
	}
	return v
}

// fakeTerms cuts a text the way the store cuts a query: latin words
// whole and lowercased, space-less scripts into two-character windows.
// Sharing the tokenization is what makes the two rankings comparable
// rather than merely different.
func fakeTerms(text string) []string {
	var terms []string
	for _, token := range strings.FieldsFunc(strings.ToLower(text), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	}) {
		runes := []rune(token)
		spaceless := unicode.Is(unicode.Han, runes[0]) ||
			unicode.Is(unicode.Hiragana, runes[0]) || unicode.Is(unicode.Katakana, runes[0])
		if !spaceless || len(runes) <= 2 {
			terms = append(terms, token)
			continue
		}
		for i := 0; i+2 <= len(runes); i++ {
			terms = append(terms, string(runes[i:i+2]))
		}
	}
	return terms
}
