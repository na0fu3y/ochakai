package store

import (
	"testing"

	"github.com/na0fu3y/ochakai/internal/domain"
	"github.com/na0fu3y/ochakai/internal/testdb"
)

// A directory's description goes where the directory goes (decision
// 0154), its subdirectories' with it.
func TestMovePrefixCarriesDirectoryDescriptionsIntegration(t *testing.T) {
	ctx, s := movePrefixStore(t)
	run := testdb.Unique(t, "stit-dirdesc-")
	actor := domain.Actor{Kind: domain.ActorHuman, Name: "test"}
	k := &domain.Knowledge{Type: domain.TypeMetrics, ID: run + "/old/sales/revenue", Status: domain.StatusDraft, CreatedBy: actor}
	if err := s.Create(ctx, k, false); err != nil {
		t.Fatal(err)
	}
	if err := s.SetDirectoryDescriptions(ctx, map[string]string{
		run + "/old":       "the old tree",
		run + "/old/sales": "revenue by channel",
		run + "/older":     "a sibling sharing the spelling",
	}, actor); err != nil {
		t.Fatal(err)
	}
	if _, err := s.MovePrefix(ctx, run+"/old", run+"/new", actor, nil); err != nil {
		t.Fatal(err)
	}
	got, err := s.DirectoryDescriptions(ctx, []string{run + "/new", run + "/new/sales", run + "/old", run + "/older"})
	if err != nil {
		t.Fatal(err)
	}
	if got[run+"/new"] != "the old tree" || got[run+"/new/sales"] != "revenue by channel" {
		t.Errorf("the descriptions did not follow the move: %v", got)
	}
	if _, stale := got[run+"/old"]; stale {
		t.Errorf("the old directory kept its description: %v", got)
	}
	if got[run+"/older"] != "a sibling sharing the spelling" {
		t.Errorf("a sibling directory was moved too: %v", got)
	}
}
