package apiclient

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/na0fu3y/ochakai/internal/domain"
	"github.com/na0fu3y/ochakai/internal/service"
	"github.com/na0fu3y/ochakai/internal/store"
)

// The apiclient types deliberately mirror api/openapi.yaml instead of
// importing the server's types (which drag in the store and embedding
// dependency trees). These tests pin the two shapes together so they
// cannot drift apart silently. Importing service here is fine: test
// files don't ship in the binary.

func TestBrowseResultMatchesServerWire(t *testing.T) {
	when := time.Date(2026, 7, 18, 0, 0, 0, 0, time.UTC)
	server := service.BrowseResult{
		Dirs: []store.DirCount{{Name: "sales", Count: 4}},
		Entries: []store.BrowseEntry{
			{Type: domain.TypeComputations, ID: "sales/monthly-revenue", Title: "月次売上",
				Description: "月次の確定売上", Status: domain.StatusStable, UpdatedAt: when},
		},
		Truncated: true,
	}
	data, err := json.Marshal(server)
	if err != nil {
		t.Fatal(err)
	}
	var got BrowseResult
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("client cannot decode the server response: %v", err)
	}
	want := BrowseResult{
		Dirs: []BrowseDir{{Name: "sales", Count: 4}},
		Entries: []BrowseEntry{
			{Type: "Attested Computation", ID: "sales/monthly-revenue", Title: "月次売上",
				Description: "月次の確定売上", Status: domain.StatusStable, UpdatedAt: when},
		},
		Truncated: true,
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("client decoded:\n%+v\nwant:\n%+v", got, want)
	}
}

func TestContextResultMatchesServerWire(t *testing.T) {
	server := service.ContextResult{
		Hits: []domain.ContextRank{
			{Type: domain.TypeMetrics, ID: "revenue", Title: "Revenue", Score: 0.9},
		},
		Entries: []domain.View{
			{ID: "revenue-seasonality", Document: "---\ntype: Insight\ntitle: Seasonality\n---\n\nQ4 peaks.\n",
				Summary: domain.Summary{Type: domain.TypeInsights, ID: "revenue-seasonality", Title: "Seasonality"}},
		},
	}
	data, err := json.Marshal(server)
	if err != nil {
		t.Fatal(err)
	}
	var got ContextResult
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("client cannot decode the server response: %v", err)
	}
	if len(got.Hits) != 1 || got.Hits[0].ID != "revenue" || len(got.Entries) != 1 ||
		!strings.Contains(got.Entries[0].Document, "Q4 peaks.") ||
		got.Entries[0].Summary.Title != "Seasonality" {
		t.Errorf("client decoded: %+v", got)
	}
}
