package okf

import (
	"strings"
	"testing"
	"time"

	"github.com/na0fu3y/ochakai/internal/domain"
)

// A generated file links the way the bundle around it is laid out. Both
// reserved names are written into a directory of a directory tree, so
// both link relative to it — a root-absolute path resolves to the
// filesystem root the moment somebody extracts the archive, and having
// index.md and log.md disagree about that in one directory is worse than
// either answer.
func TestLogDocumentLinksAreRelativeToTheDirectoryItSitsIn(t *testing.T) {
	at := time.Date(2026, 7, 14, 2, 33, 41, 0, time.UTC)
	actor := domain.Actor{Kind: domain.ActorHuman, Name: "sato@example.co.jp"}
	lines := []LogLine{
		{At: at, Change: "verify", Path: "metrics/revenue.md", Title: "Revenue", By: actor},
		{At: at, Change: "attach", Path: "metrics/revenue/chart.png", By: actor},
		// The concept the directory is named after lives one level up:
		// a directory's history covers "<dir>.md" as well as its subtree.
		{At: at, Change: "update", Path: "metrics.md", Title: "Metrics", By: actor},
	}

	for _, tc := range []struct {
		name, dir string
		want      []string
	}{
		{
			name: "the root sees whole paths",
			dir:  "",
			want: []string{
				"[Revenue](metrics/revenue.md)",
				"[metrics/revenue/chart.png](metrics/revenue/chart.png)",
				"[Metrics](metrics.md)",
			},
		},
		{
			name: "a directory sees its own contents by name",
			dir:  "metrics",
			want: []string{
				"[Revenue](revenue.md)",
				"[metrics/revenue/chart.png](revenue/chart.png)",
				"[Metrics](../metrics.md)",
			},
		},
		{
			name: "and a nested one climbs out",
			dir:  "metrics/revenue",
			want: []string{
				"[Revenue](../revenue.md)",
				"[metrics/revenue/chart.png](chart.png)",
				"[Metrics](../../metrics.md)",
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			doc := string(LogDocument(tc.dir, tc.dir, lines))
			for _, want := range tc.want {
				if !strings.Contains(doc, want) {
					t.Errorf("log.md in %q has no %s:\n%s", tc.dir, want, doc)
				}
			}
			if strings.Contains(doc, "](/") {
				t.Errorf("log.md in %q still links from the filesystem root:\n%s", tc.dir, doc)
			}
		})
	}
}
