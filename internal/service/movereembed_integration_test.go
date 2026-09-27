package service

import (
	"context"
	"strings"
	"sync"
	"testing"

	"github.com/na0fu3y/ochakai/internal/domain"
	"github.com/na0fu3y/ochakai/internal/embed"
)

// recordingEncoder is fakeEncoder that remembers every document it was
// asked to embed.
type recordingEncoder struct {
	fakeEncoder
	mu   sync.Mutex
	docs []string
}

func (r *recordingEncoder) Embed(ctx context.Context, task embed.Task, texts []string) ([][]float32, error) {
	if task == embed.TaskDocument {
		r.mu.Lock()
		r.docs = append(r.docs, texts...)
		r.mu.Unlock()
	}
	return r.fakeEncoder.Embed(ctx, task, texts)
}

func (r *recordingEncoder) embeddedAs(id string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, d := range r.docs {
		if strings.HasPrefix(d, id+"\n") {
			return true
		}
	}
	return false
}

// A concept's vector is made from text that opens with its id, so a move
// — of one concept or of a directory — makes it again under the new one.
func TestMoveEmbedsTheConceptUnderItsNewIDIntegration(t *testing.T) {
	ctx := context.Background()
	svc := newScopedEmbeddingService(t, ctx)
	fake, ok := svc.Embedder.(fakeEncoder)
	if !ok {
		t.Fatalf("the helper's embedder is %T", svc.Embedder)
	}
	rec := &recordingEncoder{fakeEncoder: fake}
	svc.Embedder = rec
	actor := domain.Actor{Kind: domain.ActorHuman, Name: "mover"}
	for _, id := range []string{"sales/revenue", "old/margin"} {
		if _, _, _, err := svc.Put(ctx, &domain.Knowledge{Type: domain.TypeMetrics, ID: id, Body: "body"}, actor, nil); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := svc.Move(ctx, "sales/revenue", "finance/revenue", actor); err != nil {
		t.Fatal(err)
	}
	if !rec.embeddedAs("finance/revenue") {
		t.Error("the moved concept was not embedded under its new id")
	}
	if _, err := svc.MovePrefix(ctx, "old", "new", actor); err != nil {
		t.Fatal(err)
	}
	if !rec.embeddedAs("new/margin") {
		t.Error("a concept a directory move carried was not embedded under its new id")
	}
}
