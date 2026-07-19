// Package importer loads Apache Ossie semantic model YAML into ochakai:
// the spec is stored verbatim for compile_sql, and metrics/table knowledge
// entries are derived so definitions are searchable alongside golden
// queries, insights, and terms.
package importer

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/na0fu3y/ochakai/internal/compiler"
	"github.com/na0fu3y/ochakai/internal/domain"
	"github.com/na0fu3y/ochakai/internal/service"
	"github.com/na0fu3y/ochakai/internal/store"
)

// Report summarizes what an import did. The JSON tags are the wire
// contract of POST /api/v1/import/ossie (mirrored in apiclient.ImportReport).
type Report struct {
	Models  []string `json:"models"`
	Created []string `json:"created"`
	Updated []string `json:"updated"`
	// Unchanged lists entries whose refreshed definition matched what was
	// already stored, so nothing was written (no revision).
	Unchanged []string `json:"unchanged,omitempty"`
}

// ImportOssie parses Ossie YAML and stores each semantic model plus derived
// metrics/table knowledge. Existing entries keep their status, tags, and
// body: re-import refreshes definitions without clobbering human curation.
func ImportOssie(ctx context.Context, svc *service.Service, yamlSrc []byte, actor domain.Actor) (*Report, error) {
	var spec map[string]any
	if err := yaml.Unmarshal(yamlSrc, &spec); err != nil {
		return nil, service.Invalidf("invalid semantic model YAML: %v", err)
	}
	models, err := compiler.ModelsFromSpec(spec)
	if err != nil {
		return nil, service.Invalidf("invalid semantic model: %v", err)
	}

	report := &Report{}
	for _, m := range models {
		if m.Name == "" {
			return nil, service.Invalidf("invalid semantic model: missing name")
		}
		modelSpec, err := toSpecMap(m)
		if err != nil {
			return nil, err
		}
		if err := svc.Store.UpsertSemanticModel(ctx, m.Name, modelSpec); err != nil {
			return nil, err
		}
		report.Models = append(report.Models, m.Name)

		// Derived entries live at the conventional "<type>/<name>" paths —
		// where compile_sql resolves metrics by name (design doc 0017 §4.4).
		for _, metric := range m.Metrics {
			attrs := map[string]any{"model": m.Name, "expression": metric.Expression}
			if err := upsertKnowledge(ctx, svc, report, actor, &domain.Knowledge{
				Type:        domain.TypeMetrics,
				ID:          "metrics/" + slugify(metric.Name),
				Title:       metric.Name,
				Description: metric.Description,
				Attrs:       attrs,
			}); err != nil {
				return nil, err
			}
		}
		for _, ds := range m.Datasets {
			attrs := map[string]any{"model": m.Name}
			if err := upsertKnowledge(ctx, svc, report, actor, &domain.Knowledge{
				Type:        domain.TypeTables,
				ID:          "tables/" + slugify(ds.Name),
				Title:       ds.Name,
				Description: ds.Description,
				Resource:    bqResource(ds.Source),
				Attrs:       attrs,
				Links:       []domain.Link{{Rel: "defined_in", Target: "model/" + m.Name}},
			}); err != nil {
				return nil, err
			}
		}
	}
	return report, nil
}

func upsertKnowledge(ctx context.Context, svc *service.Service, report *Report, actor domain.Actor, k *domain.Knowledge) error {
	existing, err := svc.Get(ctx, k.ID)
	switch {
	case errors.Is(err, store.ErrNotFound):
		if _, err := svc.Create(ctx, k, actor); err != nil {
			return err
		}
		report.Created = append(report.Created, k.URI())
		return nil
	case err != nil:
		return err
	}
	// Refresh the definition; keep human-curated fields.
	for key, v := range k.Attrs {
		existing.Attrs[key] = v
	}
	if k.Resource != "" {
		existing.Resource = k.Resource
	}
	existing.Title = k.Title
	if k.Description != "" {
		existing.Description = k.Description
	}
	_, changed, err := svc.Update(ctx, existing, actor)
	if err != nil {
		return err
	}
	if changed {
		report.Updated = append(report.Updated, k.URI())
	} else {
		report.Unchanged = append(report.Unchanged, k.URI())
	}
	return nil
}

// toSpecMap converts a parsed model back to a JSON-safe map for storage.
func toSpecMap(m compiler.Model) (map[string]any, error) {
	raw, err := json.Marshal(m)
	if err != nil {
		return nil, err
	}
	var out map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// bqResource turns a fully-qualified BigQuery source
// ("project.dataset.table") into the canonical REST resource URL the OKF
// knowledge-catalog bundles use. A source that is not fully qualified
// (no project part) has no canonical URL and is kept verbatim.
func bqResource(source string) string {
	parts := strings.Split(strings.Trim(source, "`"), ".")
	if len(parts) != 3 || parts[0] == "" || parts[1] == "" || parts[2] == "" {
		return source
	}
	return fmt.Sprintf("https://bigquery.googleapis.com/v2/projects/%s/datasets/%s/tables/%s",
		parts[0], parts[1], parts[2])
}

var slugInvalid = regexp.MustCompile(`[^a-z0-9_-]+`)

func slugify(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	s = slugInvalid.ReplaceAllString(s, "-")
	return strings.Trim(s, "-")
}
