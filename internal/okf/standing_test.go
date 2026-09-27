package okf

import (
	"strings"
	"testing"
	"time"

	"github.com/na0fu3y/ochakai/internal/domain"
)

// TestExportCarriesOnlyTheVerificationsThatStand pins decision 0155: the
// export form's `verified` holds the confirmations of the content as it
// reads now. SPEC §5.3 ranks a concept from whatever `verified` holds, so
// a confirmation an edit outlived would make every OKF reader call the
// concept human-reviewed while ochakai calls it unverified.
func TestExportCarriesOnlyTheVerificationsThatStand(t *testing.T) {
	at := func(s string) time.Time {
		v, err := time.Parse(time.RFC3339, s)
		if err != nil {
			t.Fatal(err)
		}
		return v
	}
	k := &domain.Knowledge{
		ID: "metrics/revenue", Type: domain.TypeMetrics,
		CreatedBy:        domain.Actor{Kind: domain.ActorHuman, Name: "sato"},
		UpdatedBy:        domain.Actor{Kind: domain.ActorHuman, Name: "sato"},
		ContentChangedAt: at("2026-09-10T00:00:00Z"),
		Verifications: []domain.Verification{
			{By: domain.Actor{Kind: domain.ActorHuman, Name: "tanaka"}, At: at("2026-09-01T00:00:00Z")},
			{By: domain.Actor{Kind: domain.ActorProcess, Name: "canary"}, At: at("2026-09-12T00:00:00Z")},
		},
	}
	out, err := WithServerKeys([]byte("---\ntype: Metric\n---\n\nbody\n"), k)
	if err != nil {
		t.Fatal(err)
	}
	doc := string(out)
	if strings.Contains(doc, "human:tanaka") {
		t.Errorf("a verification the edit outlived was exported:\n%s", doc)
	}
	if !strings.Contains(doc, "process:canary") {
		t.Errorf("the standing verification was not exported:\n%s", doc)
	}
	if got := domain.TrustOf(k.Verifications, k.ContentChangedAt); got != domain.TrustMachine {
		t.Fatalf("ochakai's own tier = %s", got)
	}

	// And the export form, handed back, is still recognized as this
	// instance's own observation rather than a foreign claim.
	_, _, claim := MoveServerKeys(out)
	if claim == nil || !IsObservation(claim, k) {
		t.Errorf("the export form did not read back as this instance's observation: %+v", claim)
	}
}
