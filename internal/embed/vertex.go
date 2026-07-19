package embed

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
)

// Vertex calls the Vertex AI embedding APIs directly. Authentication uses
// Application Default Credentials: on Cloud Run the service identity works
// with no API keys; locally, use `gcloud auth application-default login`.
//
// Two wire dialects, selected by model name (design doc 0020 §2.3):
// gemini-embedding-2* uses :embedContent — task instructions folded into
// the prompt, file input accepted, available in global/us/eu only —
// while earlier models (gemini-embedding-001) use :predict with a
// task_type field, text only.
//
// Note: Vertex AI's OpenAI-compatible surface covers chat.completions only,
// not /v1/embeddings, hence this native driver (design doc §4).
type Vertex struct {
	model  string
	dim    int
	base   string // …/publishers/google/models/{model}, method suffix appended
	client *http.Client
	// embedContent marks the gemini-embedding-2 dialect.
	embedContent bool
}

func NewVertex(ctx context.Context, project, location, model string, dim int) (*Vertex, error) {
	ts, err := google.DefaultTokenSource(ctx, "https://www.googleapis.com/auth/cloud-platform")
	if err != nil {
		return nil, fmt.Errorf("Vertex AI credentials (ADC) not found: %w", err)
	}
	return &Vertex{
		model: model,
		dim:   dim,
		base: fmt.Sprintf("https://%s/v1/projects/%s/locations/%s/publishers/google/models/%s",
			vertexHost(location), project, location, model),
		client:       oauth2.NewClient(ctx, ts),
		embedContent: strings.HasPrefix(model, "gemini-embedding-2"),
	}, nil
}

func (v *Vertex) Model() string { return v.model }

// vertexHost maps a location to its API host: regional locations get a
// regional host, while global and the us/eu multi-regions (where
// gemini-embedding-2 lives) use the plain one.
func vertexHost(location string) string {
	switch location {
	case "global", "us", "eu":
		return "aiplatform.googleapis.com"
	}
	return location + "-aiplatform.googleapis.com"
}

type vertexInstance struct {
	Content  string `json:"content"`
	TaskType string `json:"task_type"`
}

type vertexRequest struct {
	Instances  []vertexInstance `json:"instances"`
	Parameters struct {
		OutputDimensionality int `json:"outputDimensionality,omitempty"`
	} `json:"parameters"`
}

type vertexResponse struct {
	Predictions []struct {
		Embeddings struct {
			Values []float32 `json:"values"`
		} `json:"embeddings"`
	} `json:"predictions"`
}

func (v *Vertex) Embed(ctx context.Context, task Task, texts []string) ([][]float32, error) {
	if len(texts) == 0 {
		return nil, nil
	}
	if v.embedContent {
		return v.embedContentTexts(ctx, task, texts)
	}
	req := vertexRequest{}
	req.Parameters.OutputDimensionality = v.dim
	for _, t := range texts {
		req.Instances = append(req.Instances, vertexInstance{Content: t, TaskType: string(task)})
	}
	respBody, err := v.post(ctx, v.base+":predict", req)
	if err != nil {
		return nil, err
	}
	var out vertexResponse
	if err := json.Unmarshal(respBody, &out); err != nil {
		return nil, err
	}
	if len(out.Predictions) != len(texts) {
		return nil, fmt.Errorf("Vertex AI embeddings: got %d predictions for %d inputs", len(out.Predictions), len(texts))
	}
	vecs := make([][]float32, len(out.Predictions))
	for i, p := range out.Predictions {
		if len(p.Embeddings.Values) != v.dim {
			return nil, fmt.Errorf("Vertex AI embeddings: got dimension %d, expected %d", len(p.Embeddings.Values), v.dim)
		}
		vecs[i] = p.Embeddings.Values
	}
	return vecs, nil
}

// --- gemini-embedding-2 dialect (design doc 0020 §2.3) ---

type contentPart struct {
	Text       string      `json:"text,omitempty"`
	InlineData *inlineData `json:"inline_data,omitempty"`
}

type inlineData struct {
	MimeType string `json:"mime_type"`
	Data     string `json:"data"` // base64
}

type embedContentRequest struct {
	Content struct {
		Parts []contentPart `json:"parts"`
	} `json:"content"`
	OutputDimensionality int `json:"outputDimensionality,omitempty"`
}

type embedContentResponse struct {
	Embedding struct {
		Values []float32 `json:"values"`
	} `json:"embedding"`
}

// wrapContentTask folds the task into the prompt: gemini-embedding-2 has
// no task_type field; the multimodal embeddings guide frames queries as
// "task: search result | query: …" and documents as "title: … | text: …".
func wrapContentTask(task Task, text string) string {
	if task == TaskQuery {
		return "task: search result | query: " + text
	}
	return "title: none | text: " + text
}

// embedContentTexts embeds each text with one :embedContent call — the
// method takes a single content per request.
func (v *Vertex) embedContentTexts(ctx context.Context, task Task, texts []string) ([][]float32, error) {
	vecs := make([][]float32, len(texts))
	for i, t := range texts {
		req := embedContentRequest{OutputDimensionality: v.dim}
		req.Content.Parts = []contentPart{{Text: wrapContentTask(task, t)}}
		vec, err := v.embedContentOne(ctx, req)
		if err != nil {
			return nil, err
		}
		vecs[i] = vec
	}
	return vecs, nil
}

// EmbedFile embeds one file as a retrieval document, the filename as a
// text part alongside the bytes. Only the embedContent dialect takes
// file input.
func (v *Vertex) EmbedFile(ctx context.Context, name, mediaType string, data []byte) ([]float32, error) {
	if !v.embedContent {
		return nil, ErrFileEmbeddingUnsupported
	}
	req := embedContentRequest{OutputDimensionality: v.dim}
	req.Content.Parts = []contentPart{
		{Text: "title: " + name},
		{InlineData: &inlineData{MimeType: mediaType, Data: base64.StdEncoding.EncodeToString(data)}},
	}
	return v.embedContentOne(ctx, req)
}

func (v *Vertex) embedContentOne(ctx context.Context, req embedContentRequest) ([]float32, error) {
	respBody, err := v.post(ctx, v.base+":embedContent", req)
	if err != nil {
		return nil, err
	}
	var out embedContentResponse
	if err := json.Unmarshal(respBody, &out); err != nil {
		return nil, err
	}
	if len(out.Embedding.Values) != v.dim {
		return nil, fmt.Errorf("Vertex AI embeddings: got dimension %d, expected %d", len(out.Embedding.Values), v.dim)
	}
	return out.Embedding.Values, nil
}

func (v *Vertex) post(ctx context.Context, url string, req any) ([]byte, error) {
	body, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := v.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("Vertex AI embeddings: %w", err)
	}
	defer resp.Body.Close()
	respBody, err := io.ReadAll(io.LimitReader(resp.Body, 32<<20))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("Vertex AI embeddings: %s: %s", resp.Status, truncate(string(respBody), 500))
	}
	return respBody, nil
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
