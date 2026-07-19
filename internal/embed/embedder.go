// Package embed provides text embeddings for hybrid search. Embeddings are
// opt-in: without a configured provider, ochakai serves trigram-only search
// and never calls out of the network. Encoders are deterministic and do no
// interpretation, so this does not conflict with the no-LLM principle.
package embed

import (
	"context"
	"errors"
)

// Task distinguishes document indexing from query lookup; some models
// (including Vertex AI's) are trained with asymmetric task types.
type Task string

const (
	TaskDocument Task = "RETRIEVAL_DOCUMENT"
	TaskQuery    Task = "RETRIEVAL_QUERY"
)

// Embedder turns text into a fixed-dimension vector.
type Embedder interface {
	// Embed returns one vector per input text.
	Embed(ctx context.Context, task Task, texts []string) ([][]float32, error)
	// Model identifies the embedding model, stored alongside vectors.
	Model() string
}

// FileEmbedder is implemented by embedders whose model takes file bytes
// (gemini-embedding-2, design doc 0020). Files are always retrieval
// documents — queries are text.
type FileEmbedder interface {
	// EmbedFile returns the vector for one file, in the same semantic
	// space as Embed's text vectors. name joins the input as a text part,
	// so the filename carries signal alongside the bytes.
	EmbedFile(ctx context.Context, name, mediaType string, data []byte) ([]float32, error)
}

// ErrFileEmbeddingUnsupported reports that the configured model embeds
// text only; callers skip the file rather than treating this as a
// provider failure.
var ErrFileEmbeddingUnsupported = errors.New("the configured embedding model does not take file input (set OCHAKAI_VERTEX_MODEL=gemini-embedding-2, design doc 0020)")
