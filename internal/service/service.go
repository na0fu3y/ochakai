// Package service implements ochakai's behavior shared by the MCP server
// and the REST API: knowledge CRUD with the verification policy, hybrid
// search, and deterministic SQL compilation.
package service

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"time"

	"github.com/na0fu3y/ochakai/internal/compiler"
	"github.com/na0fu3y/ochakai/internal/config"
	"github.com/na0fu3y/ochakai/internal/domain"
	"github.com/na0fu3y/ochakai/internal/embed"
	"github.com/na0fu3y/ochakai/internal/store"
)

// ErrForbidden is returned when the acting client may not perform the
// operation (e.g. an agent promoting knowledge to verified).
var ErrForbidden = errors.New("forbidden")

type Service struct {
	Store    *store.Store
	Embedder embed.Embedder // nil when semantic search is disabled
	Config   *config.Config
	Log      *slog.Logger
}

// --- knowledge CRUD ---

func (s *Service) Get(ctx context.Context, typ domain.Type, id string) (*domain.Knowledge, error) {
	return s.Store.Get(ctx, typ, id)
}

func (s *Service) Create(ctx context.Context, k *domain.Knowledge, actor domain.Actor) (*domain.Knowledge, error) {
	if err := validate(k); err != nil {
		return nil, err
	}
	if k.Status == "" {
		k.Status = domain.StatusDraft
	}
	if err := s.applyVerification(k, nil, actor); err != nil {
		return nil, err
	}
	k.CreatedBy = actor
	if err := s.Store.Create(ctx, k); err != nil {
		return nil, err
	}
	s.updateEmbedding(ctx, k)
	return k, nil
}

func (s *Service) Update(ctx context.Context, k *domain.Knowledge, actor domain.Actor) (*domain.Knowledge, error) {
	if err := validate(k); err != nil {
		return nil, err
	}
	old, err := s.Store.Get(ctx, k.Type, k.ID)
	if err != nil {
		return nil, err
	}
	if k.Status == "" {
		k.Status = old.Status
	}
	k.CreatedBy = old.CreatedBy
	k.CreatedAt = old.CreatedAt
	k.VerifiedBy, k.VerifiedAt = old.VerifiedBy, old.VerifiedAt
	if err := s.applyVerification(k, old, actor); err != nil {
		return nil, err
	}
	if err := s.Store.Update(ctx, k, actor); err != nil {
		return nil, err
	}
	s.updateEmbedding(ctx, k)
	return k, nil
}

func (s *Service) Delete(ctx context.Context, typ domain.Type, id string, actor domain.Actor) error {
	return s.Store.SoftDelete(ctx, typ, id, actor)
}

// applyVerification enforces the promotion policy: only configured actor
// kinds (human by default) may set status=verified.
func (s *Service) applyVerification(k *domain.Knowledge, old *domain.Knowledge, actor domain.Actor) error {
	wasVerified := old != nil && old.Status == domain.StatusVerified
	if k.Status == domain.StatusVerified && !wasVerified {
		if !s.Config.CanVerify(actor) {
			return fmt.Errorf("%w: actor kind %q may not set status=verified (allowed: %s); leave as draft and ask a human to verify",
				ErrForbidden, actor.Kind, strings.Join(s.Config.VerifyActorKinds, ", "))
		}
		now := time.Now().UTC()
		k.VerifiedBy, k.VerifiedAt = &actor, &now
	}
	if k.Status != domain.StatusVerified {
		k.VerifiedBy, k.VerifiedAt = nil, nil
	}
	return nil
}

func validate(k *domain.Knowledge) error {
	if !domain.ValidType(k.Type) {
		return fmt.Errorf("invalid type %q (valid: metric, query, insight, term, table)", k.Type)
	}
	if !domain.ValidID(k.ID) {
		return fmt.Errorf("invalid id %q (lowercase slug: a-z 0-9 _ -)", k.ID)
	}
	if strings.TrimSpace(k.Title) == "" {
		return fmt.Errorf("title is required")
	}
	if k.Status != "" && !domain.ValidStatus(k.Status) {
		return fmt.Errorf("invalid status %q (valid: draft, verified, deprecated)", k.Status)
	}
	return nil
}

// --- search ---

// Search runs trigram search, and when an embedder is configured, fuses it
// with vector search via reciprocal rank fusion.
func (s *Service) Search(ctx context.Context, query string, f store.Filter, limit int) ([]domain.SearchHit, error) {
	if limit <= 0 || limit > 50 {
		limit = 10
	}
	lexical, err := s.Store.SearchLexical(ctx, query, f, limit*2)
	if err != nil {
		return nil, err
	}
	if s.Embedder == nil {
		if len(lexical) > limit {
			lexical = lexical[:limit]
		}
		return lexical, nil
	}

	vecs, err := s.Embedder.Embed(ctx, embed.TaskQuery, []string{query})
	if err != nil {
		// Degrade to lexical-only rather than failing the search.
		s.Log.Warn("query embedding failed; falling back to trigram-only", "error", err)
		if len(lexical) > limit {
			lexical = lexical[:limit]
		}
		return lexical, nil
	}
	vector, err := s.Store.SearchVector(ctx, vecs[0], f, limit*2)
	if err != nil {
		return nil, err
	}
	fused := rrfFuse(limit, lexical, vector)
	return fused, nil
}

// rrfFuse merges ranked lists with reciprocal rank fusion (k=60), adding a
// small boost for verified entries so certified knowledge surfaces first.
func rrfFuse(limit int, lists ...[]domain.SearchHit) []domain.SearchHit {
	const k = 60
	type entry struct {
		hit   domain.SearchHit
		score float64
	}
	byKey := map[string]*entry{}
	for _, list := range lists {
		for rank, hit := range list {
			key := string(hit.Type) + "/" + hit.ID
			e, ok := byKey[key]
			if !ok {
				e = &entry{hit: hit}
				byKey[key] = e
			}
			e.score += 1.0 / float64(k+rank+1)
		}
	}
	out := make([]domain.SearchHit, 0, len(byKey))
	for _, e := range byKey {
		if e.hit.Status == domain.StatusVerified {
			e.score += 0.002
		}
		e.hit.Score = e.score
		out = append(out, e.hit)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Score != out[j].Score {
			return out[i].Score > out[j].Score
		}
		return out[i].ID < out[j].ID
	})
	if len(out) > limit {
		out = out[:limit]
	}
	return out
}

// updateEmbedding refreshes the stored document vector. Failures are logged,
// not returned: writes must not depend on the embedding provider being up.
func (s *Service) updateEmbedding(ctx context.Context, k *domain.Knowledge) {
	if s.Embedder == nil {
		return
	}
	vecs, err := s.Embedder.Embed(ctx, embed.TaskDocument, []string{embeddingText(k)})
	if err != nil {
		s.Log.Warn("document embedding failed; entry remains searchable via trigram", "type", k.Type, "id", k.ID, "error", err)
		return
	}
	if err := s.Store.UpsertEmbedding(ctx, k.Type, k.ID, s.Embedder.Model(), vecs[0]); err != nil {
		s.Log.Warn("storing embedding failed", "type", k.Type, "id", k.ID, "error", err)
	}
}

// embeddingText builds the document text to embed: envelope fields plus the
// golden query question, body truncated to keep within model input limits.
func embeddingText(k *domain.Knowledge) string {
	parts := []string{k.Title, k.Description, strings.Join(k.Tags, " ")}
	if q, ok := k.Attrs["question"].(string); ok {
		parts = append(parts, q)
	}
	body := k.Body
	if len(body) > 4000 {
		body = body[:4000]
	}
	parts = append(parts, body)
	return strings.TrimSpace(strings.Join(parts, "\n"))
}

// --- compile ---

// CompileRequest wraps a compiler request with the semantic model reference.
// Model may be empty when the first metric's knowledge entry links to its
// model via attrs.model.
type CompileRequest struct {
	Model string `json:"model,omitempty"`
	compiler.Request
}

// CompileResult carries the SQL plus verified golden queries related to the
// requested metrics, which clients should prefer when applicable.
type CompileResult struct {
	compiler.Result
	VerifiedQueries []domain.SearchHit `json:"verified_queries,omitempty"`
}

func (s *Service) Compile(ctx context.Context, req CompileRequest) (*CompileResult, error) {
	if len(req.Metrics) == 0 {
		return nil, &compiler.Error{Reason: "at least one metric is required"}
	}

	modelName := req.Model
	if modelName == "" {
		k, err := s.Store.Get(ctx, domain.TypeMetric, req.Metrics[0])
		if err == nil {
			if m, ok := k.Attrs["model"].(string); ok {
				modelName = m
			}
		}
		if modelName == "" {
			return nil, &compiler.Error{Reason: fmt.Sprintf("cannot resolve a semantic model for metric %q; pass model explicitly or import one with `ochakai import-ossie`", req.Metrics[0])}
		}
	}
	spec, err := s.Store.GetSemanticModel(ctx, modelName)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return nil, &compiler.Error{Reason: fmt.Sprintf("semantic model %q is not imported", modelName)}
		}
		return nil, err
	}
	model, err := compiler.ModelFromSpec(spec, modelName)
	if err != nil {
		return nil, err
	}
	result, err := compiler.Compile(model, req.Request)
	if err != nil {
		return nil, err
	}

	// Surface verified golden queries about the requested metrics: a human-
	// checked query beats a compiled one when it answers the question.
	hits, err := s.Store.SearchLexical(ctx, strings.Join(req.Metrics, " "),
		store.Filter{Types: []domain.Type{domain.TypeQuery}, Statuses: []domain.Status{domain.StatusVerified}}, 3)
	if err != nil {
		s.Log.Warn("verified query lookup failed", "error", err)
		hits = nil
	}
	return &CompileResult{Result: *result, VerifiedQueries: hits}, nil
}
