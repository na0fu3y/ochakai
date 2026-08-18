package main

import (
	"os"
	"regexp"
	"testing"
)

// Five files carry a version number that tagging does not update, and
// CONTRIBUTING.md's release checklist is the only thing that has kept them
// together — "it describes the wire surface, so it drifts silently:
// nothing fails when it is stale" was true of api/openapi.yaml until this
// test. They are all set in the same release-prep PR, so they either agree
// or one of them was forgotten (design doc 0035: check the invariant from
// outside rather than trust the convention).
//
// The two deployment pins were the ones the checklist did not hold:
// terraform.tfvars.example is copied to terraform.tfvars and applied
// verbatim, so a stale pin there is not a stale sentence but a deployment
// of a version nobody is running — it sat nine releases behind before this
// check existed.
func TestReleaseVersionsAgree(t *testing.T) {
	changelog := versionIn(t, "../../CHANGELOG.md",
		`(?m)^## \[(\d+\.\d+\.\d+)\] - \d{4}-\d{2}-\d{2}$`)
	spec := versionIn(t, "../../api/openapi.yaml",
		`(?m)^  version: (\d+\.\d+\.\d+)$`)
	template := versionIn(t, "../../.github/ISSUE_TEMPLATE/bug_report.yml",
		`(?m)^      placeholder: "(\d+\.\d+\.\d+)"$`)
	guide := versionIn(t, "../../deploy/cloudrun/README.md",
		`export VERSION=(\d+\.\d+\.\d+)`)
	tfvars := versionIn(t, "../../deploy/terraform/terraform.tfvars.example",
		`(?m)^image_tag = "(\d+\.\d+\.\d+)"$`)

	if spec != changelog {
		t.Errorf("api/openapi.yaml says %s; the newest release in CHANGELOG.md is %s. "+
			"The spec's info.version is bumped in the release PR (CONTRIBUTING.md, Releases)",
			spec, changelog)
	}
	if template != changelog {
		t.Errorf(".github/ISSUE_TEMPLATE/bug_report.yml suggests %s; the newest release is %s. "+
			"A stale placeholder invites bug reports against a version nobody runs",
			template, changelog)
	}
	if guide != changelog {
		t.Errorf("deploy/cloudrun/README.md §1 hand-sets %s; the newest release is %s. "+
			"That line is what a reader without the gh CLI types, so it is the version they deploy",
			guide, changelog)
	}
	if tfvars != changelog {
		t.Errorf("deploy/terraform/terraform.tfvars.example pins %s; the newest release is %s. "+
			"That file is copied to terraform.tfvars and applied as written, so a stale pin "+
			"deploys the stale version rather than merely describing it",
			tfvars, changelog)
	}
}

// versionIn returns the first capture of re in the named file. The first
// match is the newest entry in every file this is used on: CHANGELOG.md is
// newest-first, and the other four carry one version each.
func versionIn(t *testing.T, path, re string) string {
	t.Helper()
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	m := regexp.MustCompile(re).FindSubmatch(content)
	if m == nil {
		t.Fatalf("%s carries no version matching %s — the file's shape changed, "+
			"and this check now guards nothing", path, re)
	}
	return string(m[1])
}
