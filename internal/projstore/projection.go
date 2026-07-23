package projstore

import (
	"encoding/binary"
	"math"
	"strings"

	"github.com/na0fu3y/ochakai/internal/domain"
)

// indexed is one entry's in-RAM projection: the entry itself, the
// generation of entries/<id>.json it was built from, the trigram set of
// the similarity() field, the lowercased substring-boost field, and the
// embedding (nil if none).
type indexed struct {
	k        domain.Knowledge
	gen      int64
	simTrig  map[string]struct{}
	likeText string
	vec      []float32
}

func buildIndexed(k domain.Knowledge, gen int64) *indexed {
	return &indexed{
		k:        k,
		gen:      gen,
		simTrig:  trigramSet(simText(&k)),
		likeText: strings.ToLower(likeText(&k)),
	}
}

// simText is the field pg_trgm.similarity() scores against, matching
// store.SearchLexical: id, title, description, tags, body (attachment
// names are appended by the store when available).
func simText(k *domain.Knowledge) string {
	return strings.Join([]string{k.ID, k.Title, k.Description, strings.Join(k.Tags, " "), k.Body}, " ")
}

// likeText is the substring-boost field: id, title, description, body —
// note it excludes tags, exactly as the ILIKE arm of store.SearchLexical.
func likeText(k *domain.Knowledge) string {
	return strings.Join([]string{k.ID, k.Title, k.Description, k.Body}, " ")
}

// --- vector helpers (0026 §4.1: flat float32, full scan) -------------------

func encodeF32(v []float32) []byte {
	b := make([]byte, 4*len(v))
	for i, f := range v {
		binary.LittleEndian.PutUint32(b[i*4:], math.Float32bits(f))
	}
	return b
}

func decodeF32(b []byte) []float32 {
	n := len(b) / 4
	v := make([]float32, n)
	for i := 0; i < n; i++ {
		v[i] = math.Float32frombits(binary.LittleEndian.Uint32(b[i*4:]))
	}
	return v
}

// cosine is the cosine similarity pgvector reports as 1 - (a <=> b).
func cosine(a, b []float32) float64 {
	if len(a) != len(b) || len(a) == 0 {
		return 0
	}
	var dot, na, nb float64
	for i := range a {
		dot += float64(a[i]) * float64(b[i])
		na += float64(a[i]) * float64(a[i])
		nb += float64(b[i]) * float64(b[i])
	}
	if na == 0 || nb == 0 {
		return 0
	}
	return dot / (math.Sqrt(na) * math.Sqrt(nb))
}
