// Package embed provides text embeddings for hybrid search. Embeddings are
// opt-in: without a configured provider, ochakai serves trigram-only search
// and never calls out of the network. Encoders are deterministic and do no
// interpretation, so this does not conflict with the no-LLM principle.
package embed

import "context"

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
