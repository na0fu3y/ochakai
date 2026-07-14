package embed

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
)

// Vertex calls the Vertex AI text embedding API directly (:predict).
// Authentication uses Application Default Credentials: on Cloud Run the
// service identity works with no API keys; locally, use
// `gcloud auth application-default login`.
//
// Note: Vertex AI's OpenAI-compatible surface covers chat.completions only,
// not /v1/embeddings, hence this native driver (design doc §4).
type Vertex struct {
	project  string
	location string
	model    string
	dim      int
	client   *http.Client
}

func NewVertex(ctx context.Context, project, location, model string, dim int) (*Vertex, error) {
	ts, err := google.DefaultTokenSource(ctx, "https://www.googleapis.com/auth/cloud-platform")
	if err != nil {
		return nil, fmt.Errorf("Vertex AI credentials (ADC) not found: %w", err)
	}
	return &Vertex{
		project:  project,
		location: location,
		model:    model,
		dim:      dim,
		client:   oauth2.NewClient(ctx, ts),
	}, nil
}

func (v *Vertex) Model() string { return v.model }

func (v *Vertex) endpoint() string {
	host := v.location + "-aiplatform.googleapis.com"
	if v.location == "global" {
		host = "aiplatform.googleapis.com"
	}
	return fmt.Sprintf("https://%s/v1/projects/%s/locations/%s/publishers/google/models/%s:predict",
		host, v.project, v.location, v.model)
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
	req := vertexRequest{}
	req.Parameters.OutputDimensionality = v.dim
	for _, t := range texts {
		req.Instances = append(req.Instances, vertexInstance{Content: t, TaskType: string(task)})
	}
	body, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, v.endpoint(), bytes.NewReader(body))
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

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
