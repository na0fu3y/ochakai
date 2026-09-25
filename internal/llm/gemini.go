// Package llm is the one place ochakai talks to a generative model
// (design doc 0142). It speaks Vertex AI's generateContent with function
// calling and nothing else: the model runs in the deployment's region and
// is called with the service identity, so there is no API key to read.
package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
)

// Model generates the next turn of a conversation. The agent loop is
// written against this so its tests do not need Vertex AI.
type Model interface {
	Generate(ctx context.Context, req Request) (*Turn, error)
	// Name is the model id, e.g. "gemini-2.5-flash" — what a write made
	// through the agent says produced it.
	Name() string
}

// Request is one call: the instructions, the conversation so far, and
// the tools the model may ask for.
type Request struct {
	System   string
	Contents []Content
	Tools    []Tool
}

// Content is one turn. Role is "user" or "model"; a function's answer
// travels back as a "user" turn, which is how generateContent spells it.
type Content struct {
	Role  string `json:"role"`
	Parts []Part `json:"parts"`
}

// Part is exactly one of text, a call the model asks for, or the answer
// to one.
//
// ThoughtSignature is opaque and belongs to the model: a thinking model
// attaches one to the parts of its turn, and a function call sent back
// without its signature is refused on the next round. The loop hands a
// model turn back exactly as it arrived, so keeping the field is all it
// takes.
type Part struct {
	Text             string            `json:"text,omitempty"`
	FunctionCall     *FunctionCall     `json:"functionCall,omitempty"`
	FunctionResponse *FunctionResponse `json:"functionResponse,omitempty"`
	ThoughtSignature string            `json:"thoughtSignature,omitempty"`
}

type FunctionCall struct {
	Name string         `json:"name"`
	Args map[string]any `json:"args,omitempty"`
}

type FunctionResponse struct {
	Name     string         `json:"name"`
	Response map[string]any `json:"response"`
}

// Tool declares one function. Parameters is an OpenAPI-subset schema
// object, as generateContent takes it.
type Tool struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Parameters  map[string]any `json:"parameters,omitempty"`
}

// Turn is what the model answered: its parts, and whether it stopped for
// a reason other than being done.
type Turn struct {
	Content      Content
	FinishReason string
}

// Calls returns the function calls in the turn, in order.
func (t *Turn) Calls() []FunctionCall {
	var calls []FunctionCall
	for _, p := range t.Content.Parts {
		if p.FunctionCall != nil {
			calls = append(calls, *p.FunctionCall)
		}
	}
	return calls
}

// Text joins the turn's text parts.
func (t *Turn) Text() string {
	var b bytes.Buffer
	for _, p := range t.Content.Parts {
		b.WriteString(p.Text)
	}
	return b.String()
}

// Gemini calls a Gemini model on Vertex AI.
type Gemini struct {
	model  string
	url    string
	client *http.Client
}

// NewGemini builds a client for projects/<project>/locations/<location>/
// publishers/google/models/<model>, authenticated with Application
// Default Credentials — the Cloud Run service identity, or a developer's
// `gcloud auth application-default login`.
func NewGemini(ctx context.Context, project, location, model string) (*Gemini, error) {
	ts, err := google.DefaultTokenSource(ctx, "https://www.googleapis.com/auth/cloud-platform")
	if err != nil {
		return nil, fmt.Errorf("no credentials for Vertex AI (ADC): %w", err)
	}
	client := oauth2.NewClient(ctx, ts)
	// One generation, not the whole conversation: the agent loop makes
	// several of these and bounds the total itself.
	client.Timeout = 90 * time.Second
	return newGemini(client, endpoint(project, location, model), model), nil
}

func newGemini(client *http.Client, url, model string) *Gemini {
	return &Gemini{model: model, url: url, client: client}
}

func (g *Gemini) Name() string { return g.model }

// endpoint is the regional generateContent URL. A regional location gets
// its regional host so the text is processed where the operator chose
// (design doc 0142 §5, the rule 0147 §1.2 set for embeddings); global and
// the us/eu multi-regions use the plain one.
func endpoint(project, location, model string) string {
	host := location + "-aiplatform.googleapis.com"
	switch location {
	case "global", "us", "eu":
		host = "aiplatform.googleapis.com"
	}
	return fmt.Sprintf("https://%s/v1/projects/%s/locations/%s/publishers/google/models/%s:generateContent",
		host, project, location, model)
}

type generateRequest struct {
	Contents          []Content        `json:"contents"`
	SystemInstruction *Content         `json:"systemInstruction,omitempty"`
	Tools             []toolSet        `json:"tools,omitempty"`
	GenerationConfig  generationConfig `json:"generationConfig"`
}

type toolSet struct {
	FunctionDeclarations []Tool `json:"functionDeclarations"`
}

type generationConfig struct {
	Temperature     float64 `json:"temperature"`
	MaxOutputTokens int     `json:"maxOutputTokens,omitempty"`
}

type generateResponse struct {
	Candidates []struct {
		Content      Content `json:"content"`
		FinishReason string  `json:"finishReason"`
	} `json:"candidates"`
	PromptFeedback *struct {
		BlockReason string `json:"blockReason"`
	} `json:"promptFeedback"`
}

// ErrNoAnswer is a response with no candidate — a prompt the model
// declined, or an empty generation.
var ErrNoAnswer = errors.New("the model returned no answer")

func (g *Gemini) Generate(ctx context.Context, req Request) (*Turn, error) {
	body := generateRequest{
		Contents:         req.Contents,
		GenerationConfig: generationConfig{Temperature: 0.2, MaxOutputTokens: 8192},
	}
	if req.System != "" {
		body.SystemInstruction = &Content{Role: "system", Parts: []Part{{Text: req.System}}}
	}
	if len(req.Tools) > 0 {
		body.Tools = []toolSet{{FunctionDeclarations: req.Tools}}
	}
	raw, err := g.post(ctx, body)
	if err != nil {
		return nil, err
	}
	var out generateResponse
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, fmt.Errorf("generateContent on Vertex AI: %w", err)
	}
	if len(out.Candidates) == 0 {
		if out.PromptFeedback != nil && out.PromptFeedback.BlockReason != "" {
			return nil, fmt.Errorf("%w: blocked (%s)", ErrNoAnswer, out.PromptFeedback.BlockReason)
		}
		return nil, ErrNoAnswer
	}
	c := out.Candidates[0]
	c.Content.Role = "model"
	return &Turn{Content: c.Content, FinishReason: c.FinishReason}, nil
}

// backoff is the wait before the first retry; each further one waits
// four times longer. A variable so tests do not sleep for real.
var backoff = time.Second

func (g *Gemini) post(ctx context.Context, req any) ([]byte, error) {
	body, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}
	wait := backoff
	var lastErr error
	for attempt := range 3 {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(wait):
			}
			wait *= 4
		}
		out, retryable, err := g.postOnce(ctx, body)
		if err == nil {
			return out, nil
		}
		if !retryable || ctx.Err() != nil {
			return nil, err
		}
		lastErr = err
	}
	return nil, lastErr
}

func (g *Gemini) postOnce(ctx context.Context, body []byte) ([]byte, bool, error) {
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, g.url, bytes.NewReader(body))
	if err != nil {
		return nil, false, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	resp, err := g.client.Do(httpReq)
	if err != nil {
		return nil, true, fmt.Errorf("generateContent on Vertex AI: %w", err)
	}
	defer resp.Body.Close()
	out, err := io.ReadAll(io.LimitReader(resp.Body, 16<<20))
	if err != nil {
		return nil, true, err
	}
	if resp.StatusCode != http.StatusOK {
		msg := string(out)
		if len(msg) > 500 {
			msg = msg[:500] + "…"
		}
		// 429 and 5xx are the model being busy; everything else is this
		// request being wrong, and asking again would be wrong again.
		retryable := resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500
		return nil, retryable, fmt.Errorf("generateContent on Vertex AI: %s: %s", resp.Status, msg)
	}
	return out, false, nil
}
