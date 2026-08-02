package embed

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"sort"
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

// modelDims is the vector width ochakai asks each model it knows for, and
// stores. It lives here rather than in a setting because a vector is a
// derived value the product owns (design doc 0073 §2): the width is a
// property of the model, not of the deployment, and the two numbers a
// deployment could disagree about — the column it created and the vector
// it writes — must never be two numbers.
//
// 768 is what every deployment that ever ran has stored. Changing a value
// here invalidates every vector already written at the old width: the
// tables are rebuilt at the new one on the next start and `ochakai
// reembed` pays to refill them, so this is a decision, not a default to
// tune. Gemini's embedding models are trained with Matryoshka
// representation learning, so a shorter output is a prefix of the full
// one rather than a different embedding, which is why one width serves
// both models.
var modelDims = map[string]int{
	"gemini-embedding-001": 768,
	"gemini-embedding-2":   768,
}

// Dimension returns the vector width for a model, and whether ochakai
// knows the model at all. A model it does not know has no width it could
// ask for, and guessing at one is the failure that shows up in the
// knowledge rather than in the log — every write rejected for its width,
// search quietly lexical (design doc 0078 §3).
func Dimension(model string) (int, bool) {
	dim, ok := modelDims[model]
	return dim, ok
}

// Models lists the models ochakai knows, sorted, for the startup error
// that names them.
func Models() []string {
	names := make([]string, 0, len(modelDims))
	for name := range modelDims {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// MaxInputBytes is the text budget for this model's input window, in
// UTF-8 bytes: 2048 tokens for gemini-embedding-001, 8192 for
// gemini-embedding-2. Bytes are the unit because the token count is not
// knowable here and bytes track it across scripts better than characters
// do; the ratio used is the worst case (Japanese, three bytes and up to
// one token per character), so the budget is roughly a third of the
// window in bytes. English leaves most of it unused, which is the right
// way round — an overrun removes the entry from vector search, while
// unused budget costs nothing.
func (v *Vertex) MaxInputBytes() int {
	if v.embedContent {
		return 20000 // gemini-embedding-2: 8192 tokens
	}
	return ConservativeInputBytes // gemini-embedding-001: 2048 tokens
}

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
		err := fmt.Errorf("Vertex AI embeddings: %s: %s", resp.Status, truncate(string(respBody), 500))
		// A token-limit rejection is a 400 naming the limit. Byte budgets
		// are only an estimate of a token count — the ratio moves with the
		// script — so the caller needs to be able to react by shortening
		// rather than by giving up on the entry.
		if resp.StatusCode == http.StatusBadRequest {
			if inputTooLong(string(respBody)) {
				return nil, fmt.Errorf("%w: %w", ErrInputTooLong, err)
			}
			// Every other 400 is logged loudly, because the phrase list
			// below is the only thing standing between a token-limit
			// rejection and an entry vanishing from vector search. When
			// the API rewords its complaint this line is how anyone finds
			// out; without it the failure looks exactly like an outage.
			slog.Warn("Vertex AI rejected the request and the reason was not recognized as a token limit;"+
				" if it is one, the shorten-and-retry path is not running", "body", truncate(string(respBody), 200))
		}
		return nil, err
	}
	return respBody, nil
}

// inputTooLong recognizes the model's complaint about input length in a
// 400 body. Matching on prose is unlovely, but the API returns no code
// that distinguishes this from any other invalid argument — so the
// caller above logs the 400s this does not recognize, and a reworded
// message shows up as a warning rather than as entries quietly missing
// from vector search.
func inputTooLong(body string) bool {
	b := strings.ToLower(body)
	for _, phrase := range []string{"token limit", "too many tokens", "input is too long",
		"exceeds the maximum", "maximum number of tokens", "input token count"} {
		if strings.Contains(b, phrase) {
			return true
		}
	}
	return false
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
