package service

import "testing"

func TestSubdirectoryOf(t *testing.T) {
	for _, c := range []struct {
		dir, target, want string
		ok                bool
	}{
		{"", "tables/", "tables", true},
		{"", "tables/index.md", "tables", true},
		{"", "./tables", "tables", true},
		{"", "/tables/", "tables", true},
		{"metrics", "sales/", "metrics/sales", true},
		{"metrics", "sales/index.md#top", "metrics/sales", true},
		{"metrics", "/metrics/sales/index.md", "metrics/sales", true},
		{"metrics", "revenue.md", "", false},
		{"metrics", "./", "", false},
		{"metrics", "/other/", "", false},
		{"metrics", "sales/deeper/", "", false},
		{"metrics", "../tables/", "", false},
		{"", "https://example.com/x/", "", false},
		{"", "a/b/", "", false},
	} {
		got, ok := subdirectoryOf(c.dir, c.target)
		if got != c.want || ok != c.ok {
			t.Errorf("subdirectoryOf(%q, %q) = %q, %v; want %q, %v", c.dir, c.target, got, ok, c.want, c.ok)
		}
	}
}
