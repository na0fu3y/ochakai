package projstore

import (
	"strings"

	"github.com/na0fu3y/ochakai/internal/domain"
)

// Filter mirrors store.Filter: an entry passes when its type is one of
// Types (matched case-insensitively per design doc 0023 §3.3), its status
// is one of Statuses, and it carries every tag in Tags. An empty slice is
// "no constraint on this axis".
type Filter struct {
	Types    []domain.Type
	Statuses []domain.Status
	Tags     []string
}

func (f Filter) match(k *domain.Knowledge) bool {
	if len(f.Types) > 0 {
		ok := false
		for _, t := range f.Types {
			if strings.EqualFold(string(t), string(k.Type)) {
				ok = true
				break
			}
		}
		if !ok {
			return false
		}
	}
	if len(f.Statuses) > 0 {
		ok := false
		for _, s := range f.Statuses {
			if s == k.Status {
				ok = true
				break
			}
		}
		if !ok {
			return false
		}
	}
	for _, want := range f.Tags {
		if !hasTag(k.Tags, want) {
			return false
		}
	}
	return true
}

func hasTag(tags []string, want string) bool {
	for _, t := range tags {
		if t == want {
			return true
		}
	}
	return false
}
