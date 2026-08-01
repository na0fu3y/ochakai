package main

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// conflictMarkerSkipExt are the extensions this guard does not read as
// text — content nothing checks for well-formedness because it is not
// meant to be read as prose or source, so a marker-shaped byte run inside
// it says nothing about an unresolved merge.
var conflictMarkerSkipExt = map[string]bool{
	".png": true, ".jpg": true, ".jpeg": true, ".gif": true, ".ico": true,
	".pdf": true, ".zip": true, ".gz": true, ".tar": true,
	".woff": true, ".woff2": true, ".ttf": true,
}

// conflictMarkers are the lines a merge leaves behind when a conflict is
// not resolved — the three-way form and the plain form both, since either
// can be left by an editor depending on merge.conflictstyle.
var conflictMarkers = []string{"<<<<<<< ", "=======", ">>>>>>> ", "||||||| "}

// TestNoUnresolvedConflictMarkers reads every tracked text file for a
// conflict marker a merge left behind unresolved.
//
// #393 merged with `<<<<<<<`, `=======` and `>>>>>>>` sitting inside
// docs/surface.md, and nothing here noticed: TestSurfaceDocCountsUserDocs
// checks that file's *numbers*, not whether it is well-formed prose, so a
// conflict marker between two of them was invisible to CI. #392 rebased
// over the markers without seeing them; a later PR cleaned them up by
// hand. This reads the whole tree instead of one file, because the same
// gap exists everywhere else a merge can land one.
func TestNoUnresolvedConflictMarkers(t *testing.T) {
	const root = "../.."
	skipDirs := map[string]bool{".git": true, "node_modules": true}
	checked := 0
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, relErr := filepath.Rel(root, path)
		if relErr != nil {
			return relErr
		}
		rel = filepath.ToSlash(rel)
		if d.IsDir() {
			if skipDirs[rel] {
				return fs.SkipDir
			}
			return nil
		}
		if conflictMarkerSkipExt[filepath.Ext(rel)] || strings.Contains(rel, "/testdata/fuzz/") {
			return nil
		}
		content, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		checked++
		for i, line := range strings.Split(string(content), "\n") {
			for _, marker := range conflictMarkers {
				if strings.HasPrefix(line, marker) {
					t.Errorf("%s:%d carries an unresolved conflict marker\n\t%s",
						rel, i+1, strings.TrimSpace(line))
				}
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk %s: %v", root, err)
	}
	if checked == 0 {
		t.Fatal("no files walked: this check now guards nothing")
	}
}
