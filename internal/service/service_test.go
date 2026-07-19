package service

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/na0fu3y/ochakai/internal/domain"
)

func hit(id string, status domain.Status) domain.SearchHit {
	return domain.SearchHit{Knowledge: domain.Knowledge{Type: domain.TypeMetrics, ID: id, Status: status}}
}

func TestRRFFuseMergesAndRanks(t *testing.T) {
	lexical := []domain.SearchHit{hit("a", domain.StatusDraft), hit("b", domain.StatusDraft)}
	vector := []domain.SearchHit{hit("b", domain.StatusDraft), hit("c", domain.StatusDraft)}
	out := rrfFuse(10, lexical, vector)
	if len(out) != 3 {
		t.Fatalf("want 3 fused hits, got %d", len(out))
	}
	// "b" appears in both lists and must rank first.
	if out[0].ID != "b" {
		t.Errorf("want b first, got %s", out[0].ID)
	}
}

func TestRRFFuseBoostsVerified(t *testing.T) {
	// Same single-list rank; verified must win the tie.
	lexical := []domain.SearchHit{hit("draft-doc", domain.StatusDraft)}
	vector := []domain.SearchHit{hit("verified-doc", domain.StatusVerified)}
	out := rrfFuse(10, lexical, vector)
	if out[0].ID != "verified-doc" {
		t.Errorf("verified entry should outrank draft at equal RRF score, got %s first", out[0].ID)
	}
}

func TestApplyVerificationStampsProvenance(t *testing.T) {
	svc := &Service{}
	human := domain.Actor{Kind: domain.ActorHuman, Name: "na0"}
	agent := domain.Actor{Kind: domain.ActorAgent, Name: "claude-code"}

	verified := &domain.Knowledge{Status: domain.StatusVerified}
	svc.applyVerification(verified, nil, human)
	if verified.VerifiedBy == nil || verified.VerifiedAt == nil {
		t.Fatal("verifying must stamp verified_by/verified_at")
	}
	if verified.RejectedBy != nil || verified.RejectedAt != nil {
		t.Error("verified entry must not carry rejection provenance")
	}

	rejected := &domain.Knowledge{Status: domain.StatusRejected, StatusNote: "duplicate of revenue-v2"}
	svc.applyVerification(rejected, verified, human)
	if rejected.RejectedBy == nil || rejected.RejectedAt == nil {
		t.Fatal("rejecting must stamp rejected_by/rejected_at")
	}
	if rejected.VerifiedBy != nil || rejected.VerifiedAt != nil {
		t.Error("leaving verified must clear verification provenance")
	}

	// A later edit that keeps status=rejected must not re-stamp: the
	// original rejecter stays on record.
	edited := &domain.Knowledge{Status: domain.StatusRejected,
		RejectedBy: rejected.RejectedBy, RejectedAt: rejected.RejectedAt}
	svc.applyVerification(edited, rejected, agent)
	if edited.RejectedBy.Name != "na0" {
		t.Errorf("rejected_by re-stamped to %q, want original na0", edited.RejectedBy.Name)
	}

	// Back to draft clears rejection provenance.
	redraft := &domain.Knowledge{Status: domain.StatusDraft,
		RejectedBy: rejected.RejectedBy, RejectedAt: rejected.RejectedAt}
	svc.applyVerification(redraft, rejected, human)
	if redraft.RejectedBy != nil || redraft.RejectedAt != nil {
		t.Error("leaving rejected must clear rejection provenance")
	}
}

func TestRRFFuseLimit(t *testing.T) {
	var list []domain.SearchHit
	for _, id := range []string{"a", "b", "c", "d"} {
		list = append(list, hit(id, domain.StatusDraft))
	}
	if got := len(rrfFuse(2, list)); got != 2 {
		t.Errorf("want limit 2, got %d", got)
	}
}

// TestReportOutcomeValidation pins the input checks that run before any
// store access: an unknown outcome and an oversized note are client
// errors (InvalidInputError → 400), never a nil-store panic.
func TestReportOutcomeValidation(t *testing.T) {
	s := &Service{}
	ctx := context.Background()
	var inputErr *InvalidInputError

	_, err := s.ReportOutcome(ctx, "queries/q", "misleading", "")
	if !errors.As(err, &inputErr) || !strings.Contains(err.Error(), "invalid outcome") {
		t.Errorf("unknown outcome: got %v, want an invalid-outcome InvalidInputError", err)
	}

	_, err = s.ReportOutcome(ctx, "queries/q", domain.EventWorked, strings.Repeat("x", maxOutcomeNote+1))
	if !errors.As(err, &inputErr) || !strings.Contains(err.Error(), "note exceeds") {
		t.Errorf("oversized note: got %v, want a note-exceeds InvalidInputError", err)
	}
}

func TestEmbeddingText(t *testing.T) {
	k := &domain.Knowledge{
		Title:       "Revenue",
		Description: "total sales",
		Tags:        []string{"finance", "kpi"},
		Attrs:       map[string]any{"question": "monthly revenue?"},
		Body:        "body text",
	}
	got := embeddingText(k)
	for _, want := range []string{"Revenue", "total sales", "finance kpi", "monthly revenue?", "body text"} {
		if !strings.Contains(got, want) {
			t.Errorf("embeddingText misses %q:\n%s", want, got)
		}
	}
}

// The body is truncated to stay within embedding-model input limits;
// the envelope fields must survive untouched.
func TestEmbeddingTextTruncatesBody(t *testing.T) {
	k := &domain.Knowledge{Title: "T", Body: strings.Repeat("x", 5000)}
	got := embeddingText(k)
	if len(got) > 4100 {
		t.Errorf("embeddingText length = %d, want body capped at 4000", len(got))
	}
	if !strings.HasPrefix(got, "T") {
		t.Errorf("title must lead the text: %q", got[:20])
	}
}

func TestValidateRejectsBadInput(t *testing.T) {
	base := func() *domain.Knowledge {
		return &domain.Knowledge{Type: "metric", ID: "revenue", Title: "Revenue"}
	}
	if err := validate(base()); err != nil {
		t.Errorf("valid entry rejected: %v", err)
	}
	for name, mutate := range map[string]func(*domain.Knowledge){
		"bad type":    func(k *domain.Knowledge) { k.Type = "no/slash" },
		"bad id":      func(k *domain.Knowledge) { k.ID = "UPPER//bad" },
		"index id":    func(k *domain.Knowledge) { k.ID = "sales/index" },
		"log id":      func(k *domain.Knowledge) { k.ID = "sales/log" },
		"empty title": func(k *domain.Knowledge) { k.Title = "   " },
		"bad status":  func(k *domain.Knowledge) { k.Status = "published" },
	} {
		k := base()
		mutate(k)
		err := validate(k)
		var invalid *InvalidInputError
		if err == nil || !errors.As(err, &invalid) {
			t.Errorf("%s: want InvalidInputError, got %v", name, err)
		}
	}
}
