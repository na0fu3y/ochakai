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
		{At: at, Change: "add_file", Path: "metrics/revenue/chart.png", By: actor},
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

// A bundle-root index.md's frontmatter is what the bundle says about
// itself (SPEC §8.1), and a producer that regenerates index.md must keep
// it. BundleMetadata keeps it line for line, less the okf_version that
// ochakai writes itself; the root index renders it back after that line.
func TestBundleMetadataKeepsTheRootFrontmatter(t *testing.T) {
	for _, tc := range []struct {
		name, doc, want string
	}{
		{"no frontmatter", "* [sales](sales/) - Revenue\n", ""},
		{"empty frontmatter", "---\n---\n\n# x\n", ""},
		{"okf_version alone", "---\nokf_version: \"0.2\"\n---\n\n# x\n", ""},
		{"as written, comments and quoting kept",
			"---\nokf_version: \"0.3\"\n# who may use it\nlicense: CC-BY-4.0\ntitle: 'Acme retail'\ndescription: Tables and metrics. Use it for sales questions.\nx-acme-owner: {team: retail}\n---\n\n# Acme\n",
			"# who may use it\nlicense: CC-BY-4.0\ntitle: 'Acme retail'\ndescription: Tables and metrics. Use it for sales questions.\nx-acme-owner: {team: retail}\n"},
		{"version written last", "---\ntitle: Acme\nokf_version: \"0.3\"\n---\n", "title: Acme\n"},
		{"a flow mapping is rendered again without the version",
			"---\n{okf_version: \"0.2\", title: Acme, license: MIT}\n---\n", "title: Acme\nlicense: MIT\n"},
		{"CRLF", "---\r\ntitle: Acme\r\n---\r\n", "title: Acme\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := BundleMetadata([]byte(tc.doc))
			if err != nil {
				t.Fatal(err)
			}
			if got != tc.want {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}
	for _, tc := range []struct{ name, doc string }{
		{"unclosed", "---\ntitle: Acme\n"},
		{"not YAML", "---\ntitle: [Acme\n---\n"},
		{"not a mapping", "---\n- Acme\n---\n"},
		{"a key twice", "---\ntitle: Acme\ntitle: Other\n---\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got, err := BundleMetadata([]byte(tc.doc)); err == nil {
				t.Errorf("accepted, kept %q", got)
			}
		})
	}
}

// The root index — at its address and in an archive, whole or a subtree
// — says what the bundle is after okf_version, and a deeper index has no
// frontmatter even when the bundle has metadata. What the archive writes
// is what BundleMetadata reads back: the round trip is unchanged.
func TestRootIndexCarriesTheBundleMetadata(t *testing.T) {
	const meta = "title: Acme retail\nlicense: CC-BY-4.0\n"
	entries := []domain.Knowledge{{Type: "Metric", ID: "sales/revenue", Description: "Revenue"}}
	described := map[string]string{"sales": "Revenue, orders and returns by channel"}
	for _, scope := range []string{"", "sales"} {
		files := Indexes(entries, nil, scope, meta, described)
		root := string(files["index.md"])
		if want := "---\nokf_version: \"" + Version + "\"\n" + meta + "---\n\n# ochakai knowledge bundle"; !strings.HasPrefix(root, want) {
			t.Errorf("scope %q: root index =\n%s\nwant it to begin\n%s", scope, root, want)
		}
		if !strings.Contains(root, "* [sales/](sales/index.md) - Revenue, orders and returns by channel") {
			t.Errorf("scope %q: the directory description is not in the archive's index:\n%s", scope, root)
		}
		if back, err := BundleMetadata(files["index.md"]); err != nil || back != meta {
			t.Errorf("scope %q: read back %q, %v", scope, back, err)
		}
		if sub := string(files["sales/index.md"]); strings.HasPrefix(sub, "---") {
			t.Errorf("scope %q: a deeper index carries frontmatter:\n%s", scope, sub)
		}
	}
	if root := string(IndexDocument("", "", nil)); !strings.HasPrefix(root, "---\nokf_version: \"0.3\"\n---\n\n") {
		t.Errorf("with no metadata the root index =\n%s", root)
	}
}
