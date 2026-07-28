package okf

import (
	"strings"
	"testing"
	"time"

	"github.com/na0fu3y/ochakai/internal/domain"
)

func testEntry() *domain.Knowledge {
	at := time.Date(2026, 7, 28, 4, 12, 0, 0, time.UTC)
	return &domain.Knowledge{
		CreatedBy: domain.Actor{Kind: domain.ActorHuman, Name: "tanaka@example.co.jp"},
		UpdatedBy: domain.Actor{Kind: domain.ActorProcess, Name: "analyst@example.iam.gserviceaccount.com"},
		UpdatedAt: at,
		Verifications: []domain.Verification{
			{By: domain.Actor{Kind: domain.ActorHuman, Name: "tanaka@example.co.jp"}, At: at},
		},
	}
}

// The document a writer sends is the document that is stored: comments,
// key order and quoting style are the writer's, and OKF asks nothing of
// any of them (design doc 0046 §2.2). This is the property the whole
// change exists for, so it is asserted on a document that would look
// nothing like the canonical rendering.
func TestParseKeepsTheDocumentAsWritten(t *testing.T) {
	in := "---\n" +
		"# what this entry is for\n" +
		"title: 'Revenue'   # single quotes, and a trailing comment\n" +
		"type: Metric\n" + // type after title: not the canonical order
		"tags:\n- finance\n- 売上\n" + // sequence at the parent's indentation
		"stale_after: 2026-12-31\n" +
		"question: どうやって売上を数えるか\n" +
		"---\n\n本文。\n"
	d, _, err := Parse([]byte(in))
	if err != nil {
		t.Fatal(err)
	}
	if d.Doc != in {
		t.Errorf("the stored document is not what was written:\n--- want ---\n%s\n--- got ---\n%s", in, d.Doc)
	}
	// The index derived from it still reads every key.
	if d.Title != "Revenue" || d.Type != domain.TypeMetrics || d.StaleAfter != "2026-12-31" ||
		len(d.Tags) != 2 || d.Attrs["question"] != "どうやって売上を数えるか" {
		t.Errorf("the index does not reproduce the document: %+v", d.Knowledge)
	}
}

// Only the line endings and the trailing newline are touched.
func TestNormalizeText(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{"---\r\ntype: Metric\r\n---\r\n", "---\ntype: Metric\n---\n"},
		{"a", "a\n"},
		{"a\n\n\n", "a\n"},
		{"\ufeff---\n", "---\n"},
		{"", ""},
	} {
		if got := string(NormalizeText([]byte(tc.in))); got != tc.want {
			t.Errorf("NormalizeText(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

// Stripping takes out the keys this instance owns — in every block shape
// YAML allows for them — and nothing else. A producer key whose name
// merely starts like an owned one stays: the match is on the key, not on
// a prefix.
func TestStripServerKeysRemovesOnlyTheInstanceKeys(t *testing.T) {
	in := "---\n" +
		"type: Metric\n" +
		"generated:\n    by: process:x\n    at: 2026-07-28T04:12:00Z\n" +
		"verified:\n- by: human:tanaka\n  at: 2026-07-27T00:00:00Z\n" +
		"\n" +
		"created_by: human:tanaka\n" +
		"generated_at_source: yes\n" +
		"title: Revenue\n" +
		"---\n\n本文。\n"
	want := "---\ntype: Metric\ngenerated_at_source: yes\ntitle: Revenue\n---\n\n本文。\n"
	if got := string(StripServerKeys([]byte(in))); got != want {
		t.Errorf("StripServerKeys:\n--- want ---\n%s\n--- got ---\n%s", want, got)
	}
	// The v0.1 spellings are owned too (SPEC §13.1).
	old := "---\ntype: Metric\ntimestamp: 2026-01-01T00:00:00Z\nverified_by: human:x\nverified_at: 2026-01-02T00:00:00Z\n---\n"
	if got := string(StripServerKeys([]byte(old))); got != "---\ntype: Metric\n---\n" {
		t.Errorf("v0.1 spellings survived stripping: %q", got)
	}
}

// The export form is the stored document plus this instance's keys, and
// taking them off again gets the stored document back — which is what
// makes get → edit → put a round trip that changes only what the editor
// changed.
func TestServerKeysRoundTrip(t *testing.T) {
	stored := "---\n# a comment\ntitle: 'Revenue'\ntype: Metric\n---\n\n本文。\n"
	out, err := WithServerKeys([]byte(stored), testEntry())
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"generated:", "verified:", "created_by: human:tanaka@example.co.jp"} {
		if !strings.Contains(string(out), want) {
			t.Errorf("the export form is missing %q:\n%s", want, out)
		}
	}
	if got := string(StripServerKeys(out)); got != stored {
		t.Errorf("round trip:\n--- want ---\n%s\n--- got ---\n%s", stored, got)
	}
	// And what comes back parses as the same entry it went in as.
	d, _, err := Parse(out)
	if err != nil {
		t.Fatal(err)
	}
	if d.Doc != stored {
		t.Errorf("re-parsing the export form stored %q, want %q", d.Doc, stored)
	}
}

// A move rewrites a referring entry's body. Its frontmatter is not what
// changed, and must come out byte for byte (design doc 0046 §2.2).
func TestReplaceBodyLeavesFrontmatterAlone(t *testing.T) {
	in := "---\n# a comment\ntitle: 'Revenue'\ntype: Metric\n---\n\nold body\n"
	got := string(ReplaceBody([]byte(in), "new [body](/metrics/net.md)"))
	want := "---\n# a comment\ntitle: 'Revenue'\ntype: Metric\n---\n\nnew [body](/metrics/net.md)\n"
	if got != want {
		t.Errorf("ReplaceBody:\n--- want ---\n%s\n--- got ---\n%s", want, got)
	}
	if got := string(ReplaceBody([]byte(in), "")); got != "---\n# a comment\ntitle: 'Revenue'\ntype: Metric\n---\n" {
		t.Errorf("an emptied body left something behind: %q", got)
	}
}

// The property the two operations owe each other, over whatever the
// fuzzer can produce: appending this instance's keys and taking them off
// again is the identity on any document ochakai stores (design doc 0035 —
// invariants the type system cannot carry are checked from outside).
func FuzzServerKeyRoundTrip(f *testing.F) {
	f.Add("---\ntype: Metric\n---\n")
	f.Add("---\ntype: Metric\ntags:\n- a\n---\n\nbody\n")
	f.Add("---\n# comment\ntype: Metric\n\ntitle: x\n---\n")
	f.Add("---\ntype: Metric\ngenerated: {by: x}\n---\n")
	f.Add("---\n---\nbody only\n")
	f.Add("not a document at all\n")

	f.Fuzz(func(t *testing.T, raw string) {
		// Only documents ochakai would have stored: the write path parses
		// before it stores, so a text Parse refuses is not one this
		// property is about.
		d, _, err := Parse(NormalizeText([]byte(raw)))
		if err != nil {
			t.Skip()
		}
		stored := d.Doc
		out, err := WithServerKeys([]byte(stored), testEntry())
		if err != nil {
			t.Fatalf("WithServerKeys(%q): %v", stored, err)
		}
		if got := string(StripServerKeys(out)); got != stored {
			t.Errorf("round trip lost bytes:\n--- stored ---\n%q\n--- back ---\n%q", stored, got)
		}
		// And the export form is still a document that reads the same.
		back, _, err := Parse(out)
		if err != nil {
			t.Fatalf("the export form no longer parses: %v\n%s", err, out)
		}
		if back.Doc != stored {
			t.Errorf("re-parsing the export form stored %q, want %q", back.Doc, stored)
		}
	})
}
