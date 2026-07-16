package okf

import (
	"reflect"
	"testing"

	"github.com/na0fu3y/ochakai/internal/domain"
)

func TestParseRoundTrip(t *testing.T) {
	entries := sample()
	for i := range entries {
		want := entries[i]
		doc, err := Document(&want)
		if err != nil {
			t.Fatal(err)
		}
		got, err := Parse(doc)
		if err != nil {
			t.Fatalf("Parse(%s): %v", want.URI(), err)
		}
		if got.Type != want.Type || got.ID != want.ID || got.Title != want.Title ||
			got.Description != want.Description || got.Status != want.Status {
			t.Errorf("envelope mismatch: got %+v, want %+v", got, want)
		}
		if !reflect.DeepEqual(got.Tags, want.Tags) {
			t.Errorf("tags = %v, want %v", got.Tags, want.Tags)
		}
		if !reflect.DeepEqual(got.Links, want.Links) {
			t.Errorf("links = %v, want %v", got.Links, want.Links)
		}
		if len(got.Attrs) != len(want.Attrs) {
			t.Errorf("attrs = %v, want %v", got.Attrs, want.Attrs)
		}
		if wantBody := "12月は+40%が通常。"; want.Body != "" && got.Body != wantBody {
			t.Errorf("body = %q, want %q", got.Body, wantBody)
		}
	}
}

func TestParseAcceptsRawTypeAndHandWrittenDoc(t *testing.T) {
	k, err := Parse([]byte(`---
type: query
id: monthly-revenue
title: 月次売上
status: draft
attrs:
  question: 月ごとの売上は？
  sql: SELECT 1
---

Body text here.
`))
	if err != nil {
		t.Fatal(err)
	}
	if k.Type != domain.TypeQuery || k.ID != "monthly-revenue" {
		t.Errorf("got %s/%s", k.Type, k.ID)
	}
	if k.Attrs["sql"] != "SELECT 1" {
		t.Errorf("attrs = %v", k.Attrs)
	}
	if k.Body != "Body text here." {
		t.Errorf("body = %q", k.Body)
	}
}

func TestParseRejectsGarbage(t *testing.T) {
	for _, doc := range []string{
		"just markdown, no frontmatter",
		"---\ntype: metric\n", // unterminated
		"---\ntype: nonsense\ntitle: x\n---\n",
	} {
		if _, err := Parse([]byte(doc)); err == nil {
			t.Errorf("Parse(%q) succeeded, want error", doc)
		}
	}
}

func TestSplitLinksLeavesForeignSectionsAlone(t *testing.T) {
	body := "Intro.\n\n# Links\n\nSee [the dashboard](https://example.com) for details."
	gotBody, links := splitLinks(body)
	if links != nil || gotBody != body {
		t.Errorf("splitLinks rewrote a non-generated section: body=%q links=%v", gotBody, links)
	}
}
