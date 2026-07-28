package service

import (
	"context"
	"errors"

	"github.com/na0fu3y/ochakai/internal/domain"
	"github.com/na0fu3y/ochakai/internal/embed"
	"github.com/na0fu3y/ochakai/internal/store"
)

// File operations (design doc 0046 §§3.2-3.3). ochakai stores and serves
// a file's bytes; it never interprets them — no OCR, no captioning, no
// parsing. Reading a file and writing what it says into a body is the
// client agent's job, like every other interpretation.
//
// A file is addressed by its bundle path, and writing one touches no
// entry: which entry it belongs to is derived from the bodies that link
// to it, so there is no relationship to record and nothing about the
// entry that changed.

// PutFile stores data at a bundle path, replacing whatever was there.
// The media type is sniffed from the bytes, never taken from the caller,
// and nothing is refused for being the wrong kind of file — a bundle
// whose files ochakai will not hold is a bundle that does not round-trip
// (design doc 0046 §3.2). What a browser is allowed to render is decided
// where it is served.
func (s *Service) PutFile(ctx context.Context, path string, data []byte, actor domain.Actor) (*domain.File, error) {
	if err := s.readOnly(); err != nil {
		return nil, err
	}
	path = domain.Normalize(path)
	if !s.Store.HasBlobStore() {
		return nil, Unsupportedf("files are not supported without GCS: this instance stores markdown entries only; set OCHAKAI_GCS_BUCKET (design doc 0013)")
	}
	if err := domain.ValidateFile(path, len(data)); err != nil {
		return nil, Invalidf("%v", err)
	}
	f, err := s.Store.PutFile(ctx, path, domain.DetectMediaType(data), data, actor)
	if errors.Is(err, store.ErrPathTaken) {
		return nil, Invalidf("a concept already lives at %q: write it as a document, or choose another path", path)
	}
	if err != nil {
		return nil, err
	}
	s.updateFileEmbedding(ctx, f, data)
	return f, nil
}

// File returns one file with its bytes, recording a fetch against every
// entry the file belongs to — reading the evidence is using the
// knowledge that cites it.
func (s *Service) File(ctx context.Context, path string) (*domain.File, []byte, error) {
	path = domain.Normalize(path)
	f, data, err := s.Store.GetFile(ctx, path)
	if err != nil {
		return nil, nil, err
	}
	if owners, err := s.Store.EntriesOwning(ctx, path); err == nil && len(owners) > 0 {
		s.recordUsage(ctx, domain.EventFetched, owners)
	}
	return f, data, nil
}

// FileMeta returns one file's metadata without its bytes — enough for a
// conditional GET (ETag = content hash) to answer 304 without a
// blob-store read, and without recording a fetch: a cache revalidation
// is not a use of the knowledge.
func (s *Service) FileMeta(ctx context.Context, path string) (*domain.File, error) {
	return s.Store.GetFileMeta(ctx, domain.Normalize(path))
}

// ListFiles returns the live files under a path prefix.
func (s *Service) ListFiles(ctx context.Context, prefix string) ([]domain.File, error) {
	prefix, err := normalizePrefix(prefix)
	if err != nil {
		return nil, err
	}
	return s.Store.ListFiles(ctx, prefix)
}

// DeleteFile removes a file from the bundle.
func (s *Service) DeleteFile(ctx context.Context, path string, actor domain.Actor) error {
	if err := s.readOnly(); err != nil {
		return err
	}
	return s.Store.DeleteFile(ctx, domain.Normalize(path), actor)
}

// FilesOf returns the files belonging to one entry, derived from its
// body and its namespace (design doc 0046 §3.3).
func (s *Service) FilesOf(ctx context.Context, k *domain.Knowledge) ([]domain.File, error) {
	if k == nil {
		return nil, nil
	}
	// Two candidate sets, one query each: what the body links to (which
	// may be anywhere in the bundle) and what sits in the entry's own
	// directory. Listing the whole bundle to answer one entry would grow
	// with the bundle rather than with the answer.
	files, err := s.Store.ListFiles(ctx, k.ID)
	if err != nil {
		return nil, err
	}
	for _, path := range domain.FileLinksFromBody(k.ID, k.Body) {
		f, err := s.Store.GetFileMeta(ctx, path)
		if errors.Is(err, store.ErrNotFound) {
			continue // a link to a file nobody has written yet
		}
		if err != nil {
			return nil, err
		}
		files = append(files, *f)
	}
	return domain.FilesOf(k.ID, k.Body, dedupeFiles(files)), nil
}

func dedupeFiles(in []domain.File) []domain.File {
	seen := map[string]bool{}
	out := in[:0]
	for _, f := range in {
		if seen[f.Path] {
			continue
		}
		seen[f.Path] = true
		out = append(out, f)
	}
	return out
}

// FillFiles fills each entry's file metadata in one batch query. The
// REST list surfaces carry it so a UI can render previews without a
// fetch per entry; MCP results stay lean (design doc 0015).
func (s *Service) FillFiles(ctx context.Context, ks []*domain.Knowledge) error {
	if len(ks) == 0 {
		return nil
	}
	ids := make([]string, len(ks))
	for i, k := range ks {
		ids[i] = k.ID
	}
	files, err := s.Store.FilesForEntries(ctx, ids)
	if err != nil {
		return err
	}
	for _, k := range ks {
		k.Files = files[k.ID]
	}
	return nil
}

// updateFileEmbedding refreshes a file's vector (design doc 0020).
// Embedding a file is encoding, not interpretation, so the
// no-interpretation principle above holds. Text embeds via the text path
// and works on any model; image and PDF bytes need a model that takes
// file input (gemini-embedding-2) and are skipped otherwise — the file
// stays findable by name. A file of any other kind is not sent at all.
// Failures are logged, not returned: a write must not depend on the
// embedding provider being up.
func (s *Service) updateFileEmbedding(ctx context.Context, f *domain.File, data []byte) {
	if s.Embedder == nil || !domain.Embeddable(f.MediaType) {
		return
	}
	var vec []float32
	if f.MediaType == "text/plain" {
		max := s.embedBytes()
		head := data
		if len(head) > max {
			head = head[:max] // cap before the copy
		}
		// truncateUTF8 still runs on the capped slice: the cut above may
		// have landed inside a character.
		body := truncateUTF8(string(head), max)
		vecs, err := s.embedDocument(ctx, truncateUTF8(f.Path+"\n"+body, max))
		if err != nil {
			s.Log.Warn("file embedding failed; the file remains findable by name", "path", f.Path, "error", err)
			return
		}
		vec = vecs[0]
	} else {
		fe, ok := s.Embedder.(embed.FileEmbedder)
		if !ok {
			return
		}
		var err error
		vec, err = fe.EmbedFile(ctx, f.Name(), f.MediaType, data)
		if errors.Is(err, embed.ErrFileEmbeddingUnsupported) {
			return // text-only model: findable by name, no noise in the log
		}
		if err != nil {
			s.Log.Warn("file embedding failed; the file remains findable by name", "path", f.Path, "error", err)
			return
		}
	}
	if err := s.Store.UpsertFileEmbedding(ctx, f.Path, s.Embedder.Model(), vec); err != nil {
		s.Log.Warn("storing a file embedding failed", "path", f.Path, "error", err)
	}
}
