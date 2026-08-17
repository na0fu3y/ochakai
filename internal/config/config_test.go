package config

import (
	"strings"
	"testing"

	"github.com/na0fu3y/ochakai/internal/embed"
)

func TestDatabaseURLRequired(t *testing.T) {
	t.Setenv("OCHAKAI_DATABASE_URL", "")
	if _, err := FromEnv(); err == nil || !strings.Contains(err.Error(), "OCHAKAI_DATABASE_URL") {
		t.Errorf("err = %v, want OCHAKAI_DATABASE_URL requirement", err)
	}
}

func TestGCSBucket(t *testing.T) {
	t.Setenv("OCHAKAI_DATABASE_URL", "postgres://x")

	t.Run("default off", func(t *testing.T) {
		cfg, err := FromEnv()
		if err != nil {
			t.Fatal(err)
		}
		if cfg.GCSBucket != "" {
			t.Errorf("GCSBucket = %q, want empty", cfg.GCSBucket)
		}
	})

	t.Run("set", func(t *testing.T) {
		t.Setenv("OCHAKAI_GCS_BUCKET", "my-blobs")
		cfg, err := FromEnv()
		if err != nil {
			t.Fatal(err)
		}
		if cfg.GCSBucket != "my-blobs" {
			t.Errorf("GCSBucket = %q, want my-blobs", cfg.GCSBucket)
		}
	})
}

// One variable says how a deployment embeds (design doc 0080): unset or
// "on" for the product's default around a discovered project, "off" for
// none, and a Vertex AI model resource name for a deployment that needs a
// particular model, region or project. A deployment that wants no Vertex
// AI call made on its behalf must be able to say so and be believed, so a
// spelling that is none of the three is refused rather than read as one
// of them.
func TestEmbeddingsSwitch(t *testing.T) {
	t.Setenv("OCHAKAI_DATABASE_URL", "postgres://x/y")

	t.Run("off refuses discovery", func(t *testing.T) {
		t.Setenv("OCHAKAI_EMBEDDINGS", "off")
		cfg, err := FromEnv()
		if err != nil {
			t.Fatal(err)
		}
		if !cfg.EmbeddingsOff {
			t.Fatal("OCHAKAI_EMBEDDINGS=off did not turn embeddings off")
		}
		cfg.EnableDiscoveredEmbedding("some-project", "asia-northeast1")
		if cfg.Embedding != nil {
			t.Errorf("a discovered project turned embeddings back on: %+v", cfg.Embedding)
		}
	})

	t.Run("a discovered default is not a named one", func(t *testing.T) {
		cfg, err := FromEnv()
		if err != nil {
			t.Fatal(err)
		}
		if cfg.Embedding != nil {
			t.Fatalf("Embedding = %+v before discovery, want nil", cfg.Embedding)
		}
		cfg.EnableDiscoveredEmbedding("some-project", "asia-northeast1")
		if cfg.Embedding == nil || cfg.Embedding.Project != "some-project" {
			t.Fatalf("Embedding = %+v, want the discovered project", cfg.Embedding)
		}
		// The flag is what decides that a Vertex AI that does not answer
		// is a fallback rather than a failure to start.
		if !cfg.Embedding.Discovered {
			t.Error("a discovered default was recorded as named")
		}
		// The location is the discovered region, not a constant: the text
		// is embedded where the deployment runs (design doc 0080 §1.2).
		if cfg.Embedding.Model != "gemini-embedding-001" ||
			cfg.Embedding.Location != "asia-northeast1" || cfg.Embedding.Dim != 768 {
			t.Errorf("model=%q location=%q dim=%d, want the product's model in the discovered region",
				cfg.Embedding.Model, cfg.Embedding.Location, cfg.Embedding.Dim)
		}
	})

	// The whole point of reading the region: two deployments of the same
	// image embed in two different places, each its own.
	t.Run("the discovered region is where the text goes", func(t *testing.T) {
		for _, region := range []string{"asia-northeast1", "europe-west4", "us-central1"} {
			cfg, err := FromEnv()
			if err != nil {
				t.Fatal(err)
			}
			cfg.EnableDiscoveredEmbedding("some-project", region)
			if cfg.Embedding == nil || cfg.Embedding.Location != region {
				t.Errorf("deployed in %s, embedding at %+v", region, cfg.Embedding)
			}
		}
	})

	// Refusing here is the decision: any region this code picked would be
	// one nobody chose, and where the text goes is not ochakai's call to
	// make quietly (design doc 0080 §1.2).
	t.Run("no region means no discovered embedding", func(t *testing.T) {
		cfg, err := FromEnv()
		if err != nil {
			t.Fatal(err)
		}
		cfg.EnableDiscoveredEmbedding("some-project", "")
		if cfg.Embedding != nil {
			t.Errorf("Embedding = %+v; a region nobody could read was filled in anyway", cfg.Embedding)
		}
	})

	t.Run("a named model carries project, location and width", func(t *testing.T) {
		t.Setenv("OCHAKAI_EMBEDDINGS", "projects/named-project/locations/global/publishers/google/models/gemini-embedding-2")
		cfg, err := FromEnv()
		if err != nil {
			t.Fatal(err)
		}
		if cfg.Embedding == nil || cfg.Embedding.Discovered {
			t.Fatalf("Embedding = %+v, want the named model, not a discovered default", cfg.Embedding)
		}
		if cfg.Embedding.Project != "named-project" || cfg.Embedding.Location != "global" ||
			cfg.Embedding.Model != "gemini-embedding-2" {
			t.Errorf("Embedding = %+v; the resource name was not read apart", cfg.Embedding)
		}
		// The width is the model's and comes from the code, never from
		// the environment: a deployment cannot be talked into writing
		// vectors at a width its stored ones do not have.
		if cfg.Embedding.Dim != 768 {
			t.Errorf("Dim = %d, want 768 — the width ochakai carries for this model", cfg.Embedding.Dim)
		}
		cfg.EnableDiscoveredEmbedding("some-other-project", "asia-northeast1")
		if cfg.Embedding.Project != "named-project" || cfg.Embedding.Location != "global" {
			t.Errorf("Embedding = %+v; discovery overrode a named model", cfg.Embedding)
		}
	})

	t.Run("a spelling that is none of the three is refused", func(t *testing.T) {
		for _, v := range []string{
			"false", "true", "ON", "enabled",
			"my-project",         // the project alone, as OCHAKAI_VERTEX_PROJECT took it
			"gemini-embedding-2", // the model alone, as OCHAKAI_VERTEX_MODEL took it
			"projects/p/locations/l/publishers/google/models/",
			"projects/p/locations/l/publishers/acme/models/gemini-embedding-001",
			"projects//locations/l/publishers/google/models/gemini-embedding-001",
			"projects/p/locations/l/models/gemini-embedding-001",
			// A model ochakai has no width for: guessing one writes
			// vectors nobody can compare (design doc 0080 §3).
			"projects/p/locations/l/publishers/google/models/gemini-embedding-99",
		} {
			t.Run(v, func(t *testing.T) {
				t.Setenv("OCHAKAI_EMBEDDINGS", v)
				if _, err := FromEnv(); err == nil {
					t.Errorf("OCHAKAI_EMBEDDINGS=%q was accepted; a deployment that means one thing must not get another", v)
				}
			})
		}
	})
}

// The error a misspelling gets names all three forms: a startup error
// that only says "no" leaves the operator guessing at the spelling that
// would have worked.
func TestEmbeddingsErrorNamesTheThreeForms(t *testing.T) {
	t.Setenv("OCHAKAI_DATABASE_URL", "postgres://x/y")
	t.Setenv("OCHAKAI_EMBEDDINGS", "yes")
	_, err := FromEnv()
	if err == nil {
		t.Fatal("OCHAKAI_EMBEDDINGS=yes was accepted")
	}
	for _, want := range []string{"on", "off", "publishers/google/models"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not name %q", err, want)
		}
	}
}

// The width follows from the model, so the model ochakai reaches for when
// nobody names one must be a model it carries a width for. Nothing else
// in FromEnv can report that it is not.
func TestTheDefaultModelHasAWidth(t *testing.T) {
	dim, ok := embed.Dimension(defaultEmbeddingModel)
	if !ok {
		t.Fatalf("no width for the default model %q: every discovered deployment would embed at 0",
			defaultEmbeddingModel)
	}
	if dim != 768 {
		t.Errorf("the default model's width is %d, want 768 — changing it strands every vector "+
			"already stored by every deployment", dim)
	}
}

// The public posture implies read-only and cannot be separated from it:
// not reading provenance is only defensible because nothing is written
// (design doc 0042 §2.1, §3). Since 0060 the implication is the only
// thing left to check — there is no second variable that could ask for
// writes back.
func TestPublicModeImpliesReadOnly(t *testing.T) {
	t.Setenv("OCHAKAI_DATABASE_URL", "postgres://x/y")
	t.Setenv("OCHAKAI_MODE", "public")

	cfg, err := FromEnv()
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.PublicReadOnly || !cfg.ReadOnly {
		t.Errorf("public=%v read_only=%v; a publicly readable deployment must never be writable",
			cfg.PublicReadOnly, cfg.ReadOnly)
	}
}

// One word, one posture. The combination 0042 §2.3 had to refuse —
// public together with insecure dev, where a stranger may name any
// person they like — is now unspellable rather than rejected, which is
// the whole of design doc 0060 in one test.
func TestModesAreExclusive(t *testing.T) {
	t.Setenv("OCHAKAI_DATABASE_URL", "postgres://x/y")
	for _, c := range []struct {
		mode                          string
		readOnly, public, insecureDev bool
	}{
		{"", false, false, false},
		{"read-only", true, false, false},
		{"public", true, true, false},
		{"dev", false, false, true},
	} {
		t.Run(c.mode, func(t *testing.T) {
			t.Setenv("OCHAKAI_MODE", c.mode)
			cfg, err := FromEnv()
			if err != nil {
				t.Fatal(err)
			}
			if cfg.ReadOnly != c.readOnly || cfg.PublicReadOnly != c.public || cfg.InsecureDev != c.insecureDev {
				t.Errorf("mode %q: read_only=%v public=%v dev=%v, want %v/%v/%v",
					c.mode, cfg.ReadOnly, cfg.PublicReadOnly, cfg.InsecureDev,
					c.readOnly, c.public, c.insecureDev)
			}
		})
	}
}

// A misspelled posture is refused, not read as the default. A deployment
// that meant read-only and got a writable one would find out from its
// knowledge rather than from its logs (design doc 0060 §2.3) — the same
// reasoning OCHAKAI_EMBEDDINGS already used for "false".
func TestUnknownModeRefused(t *testing.T) {
	t.Setenv("OCHAKAI_DATABASE_URL", "postgres://x/y")
	for _, mode := range []string{"readonly", "read_only", "true", "public-read-only", "READ-ONLY"} {
		t.Run(mode, func(t *testing.T) {
			t.Setenv("OCHAKAI_MODE", mode)
			if _, err := FromEnv(); err == nil {
				t.Errorf("OCHAKAI_MODE=%q was accepted; a posture nobody can spell must not be silently the default", mode)
			}
		})
	}
}

// Off by default: a deployment that names no mode is unchanged.
func TestModeDefaultsToTheOrdinaryPosture(t *testing.T) {
	t.Setenv("OCHAKAI_DATABASE_URL", "postgres://x/y")
	cfg, err := FromEnv()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.PublicReadOnly || cfg.ReadOnly || cfg.InsecureDev {
		t.Errorf("public=%v read_only=%v dev=%v, want the ordinary posture",
			cfg.PublicReadOnly, cfg.ReadOnly, cfg.InsecureDev)
	}
}

// Misses are recorded unless the operator says otherwise: a measurement
// nobody switches on is a measurement nobody has (design doc 0051 §3.4).
// It is the only default-on boolean here, so it is the only one that
// reads anything but "true" as on.
func TestRecordMissesDefaultsOn(t *testing.T) {
	t.Setenv("OCHAKAI_DATABASE_URL", "postgres://x/y")
	cfg, err := FromEnv()
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.RecordMisses {
		t.Error("misses are not recorded by default")
	}
	t.Setenv("OCHAKAI_RECORD_MISSES", "false")
	if cfg, err = FromEnv(); err != nil {
		t.Fatal(err)
	} else if cfg.RecordMisses {
		t.Error("OCHAKAI_RECORD_MISSES=false did not turn recording off")
	}
}

// A public deployment reads no identity, so it keeps no query text
// either — and asking for it back is not a way out, exactly as with
// read-only (design doc 0051 §3.4).
func TestPublicModeKeepsNoQueries(t *testing.T) {
	t.Setenv("OCHAKAI_DATABASE_URL", "postgres://x/y")
	t.Setenv("OCHAKAI_MODE", "public")
	t.Setenv("OCHAKAI_RECORD_MISSES", "true")
	cfg, err := FromEnv()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.RecordMisses {
		t.Error("a public deployment is keeping what its callers typed")
	}
}

// The fourth cell of the posture square, deployed on purpose: anonymous
// and writable (design doc 0087). Each of the four spellings is pinned
// against the others, because what separates them is exactly what an
// operator would get wrong.
func TestSandboxPosture(t *testing.T) {
	t.Setenv("OCHAKAI_DATABASE_URL", "postgres://x")
	for mode, want := range map[string]struct {
		sandbox, anonymous, readOnly, dev, misses bool
	}{
		"":          {misses: true},
		"read-only": {readOnly: true, misses: true},
		"public":    {anonymous: true, readOnly: true},
		"dev":       {dev: true, misses: true},
		"sandbox":   {sandbox: true, anonymous: true},
	} {
		t.Run("mode="+mode, func(t *testing.T) {
			t.Setenv("OCHAKAI_MODE", mode)
			cfg, err := FromEnv()
			if err != nil {
				t.Fatal(err)
			}
			got := struct{ sandbox, anonymous, readOnly, dev, misses bool }{
				cfg.Sandbox, cfg.Anonymous(), cfg.ReadOnly, cfg.InsecureDev, cfg.RecordMisses,
			}
			if got != want {
				t.Errorf("mode %q = %+v, want %+v", mode, got, want)
			}
		})
	}
}

// A sandbox reads no identity, so an issuer beside it is a configuration
// saying two different things — the same refusal public and dev already
// get.
func TestSandboxRefusesAnIssuer(t *testing.T) {
	t.Setenv("OCHAKAI_DATABASE_URL", "postgres://x")
	t.Setenv("OCHAKAI_MODE", "sandbox")
	t.Setenv("OCHAKAI_OIDC_ISSUER", "https://issuer.example")
	t.Setenv("OCHAKAI_OIDC_AUDIENCE", "ochakai")
	if _, err := FromEnv(); err == nil {
		t.Error("a sandbox with an OIDC issuer was accepted; it reads no identity")
	}
}

// A variable ochakai does not read used to be read by nobody and said so
// to nobody: OCHAKAI_RECORD_MISSES typed one letter short left the
// recording on, and a deployment that kept OCHAKAI_VERTEX_PROJECT after
// design doc 0078 folded it away kept a value nothing consulted. The
// refusal is the one design doc 0064 §2 already makes for a query
// parameter, moved to the other surface an operator spells by hand
// (design doc 0112).
func TestAVariableNobodyReadsStopsTheStart(t *testing.T) {
	t.Setenv("OCHAKAI_DATABASE_URL", "postgres://x")

	t.Run("named, with what ochakai does read", func(t *testing.T) {
		t.Setenv("OCHAKAI_VERTEX_PROJECT", "my-project")
		_, err := FromEnv()
		if err == nil {
			t.Fatal("OCHAKAI_VERTEX_PROJECT was accepted; a value nothing reads must not be silent")
		}
		if !strings.Contains(err.Error(), "OCHAKAI_VERTEX_PROJECT") {
			t.Errorf("err = %v, want the variable named", err)
		}
		if !strings.Contains(err.Error(), "OCHAKAI_EMBEDDINGS") {
			t.Errorf("err = %v, want the list of what ochakai does read", err)
		}
	})

	// Both at once, because an operator fixing one variable per redeploy
	// finds the second one from a second failed revision.
	t.Run("every one of them at once", func(t *testing.T) {
		t.Setenv("OCHAKAI_VERTEX_MODEL", "gemini-embedding-001")
		t.Setenv("OCHAKAI_READ_ONLY", "true")
		_, err := FromEnv()
		if err == nil {
			t.Fatal("two variables nothing reads were accepted")
		}
		for _, want := range []string{"OCHAKAI_READ_ONLY", "OCHAKAI_VERTEX_MODEL"} {
			if !strings.Contains(err.Error(), want) {
				t.Errorf("err = %v, want %s named too", err, want)
			}
		}
	})
}

// The half of the namespace this repository's own harness keeps.
// OCHAKAI_TEST_DATABASE_URL is exported across the whole of
// `scripts/check --db`, so every test in this file runs under it in CI —
// a check that read it as a misconfiguration would fail the run that
// introduced it.
func TestTheHarnessNamespaceIsNotAMisconfiguration(t *testing.T) {
	t.Setenv("OCHAKAI_DATABASE_URL", "postgres://x")
	t.Setenv("OCHAKAI_TEST_DATABASE_URL", "postgres://t:t@localhost:55433/t")
	if _, err := FromEnv(); err != nil {
		t.Errorf("err = %v, want a start: %s is the harness's, not a deployment's", err, "OCHAKAI_TEST_DATABASE_URL")
	}
}

// What the check reads is a process environment, which carries every
// other program's variables and, on some systems, an entry with no "=".
func TestUnknownVarsReadsOnlyOchakaisNamespace(t *testing.T) {
	got := unknownVars([]string{
		"PATH=/usr/bin",
		"NO_COLOR=1",
		"PORT=8080",
		"OCHAKAI_MODE=sandbox",
		"OCHAKAI_TEST_DATABASE_URL=postgres://t",
		"OCHAKAI_TYPO",
		"OCHAKAI_ZZZ=1",
		"OCHAKAI_AAA=1",
	})
	// Sorted, so the operator reading a Cloud Run revision's logs gets
	// the same sentence every time rather than the environment's order.
	want := []string{"OCHAKAI_AAA", "OCHAKAI_ZZZ"}
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Errorf("unknownVars = %v, want %v", got, want)
	}
}
