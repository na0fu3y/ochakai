package service

import (
	"context"
	"errors"
	"strings"

	"github.com/na0fu3y/ochakai/internal/domain"
	"github.com/na0fu3y/ochakai/internal/embed"
)

// Attachment operations (design docs 0008, 0013). ochakai stores and
// serves attachment bytes; it never interprets them — no OCR, no
// captioning, no parsing. Reading a file and writing what it says back
// into the body is the client agent's job, like every other
// interpretation.

// Attach stores data as an attachment of the entry, replacing any
// file of the same name. The media type is sniffed from the bytes, never
// taken from the caller.
//
// The file lands at <id>/<name>. One that belongs somewhere else in the
// bundle is written at the path it belongs at (PutFile): where a file
// lives is its address, not an annotation on a write to another one
// (design doc 0046 §3.3).
func (s *Service) Attach(ctx context.Context, id, name string, data []byte, actor domain.Actor) (*domain.Attachment, error) {
	if err := s.readOnly(); err != nil {
		return nil, err
	}
	id, name = domain.Normalize(id), domain.Normalize(name)
	if !s.Store.HasBlobStore() {
		return nil, Unsupportedf("attachments are not supported without GCS: this instance stores markdown entries only; set OCHAKAI_GCS_BUCKET (design doc 0013)")
	}
	// Both are judgments about the client's bytes, so classify them as
	// input errors (REST: 400).
	if err := domain.ValidateAttachment(name, len(data)); err != nil {
		return nil, Invalidf("%v", err)
	}
	mediaType, err := domain.DetectAttachmentMediaType(data)
	if err != nil {
		return nil, Invalidf("%v", err)
	}
	att, err := s.Store.PutAttachment(ctx, id, name, mediaType, "", data, actor)
	if err != nil {
		return nil, err
	}
	s.updateAttachmentEmbedding(ctx, id, att, data)
	return att, nil
}

// updateAttachmentEmbedding refreshes an attachment's document vector
// (design doc 0020). Embedding a file is encoding, not interpretation,
// so the no-interpretation principle above holds. Text embeds via the
// text path and works on any model; image/PDF bytes need a model that
// takes file input (gemini-embedding-2) and are skipped otherwise —
// the file stays findable by name. Failures are logged, not returned:
// attach must not depend on the embedding provider being up.
func (s *Service) updateAttachmentEmbedding(ctx context.Context, id string, att *domain.Attachment, data []byte) {
	if s.Embedder == nil {
		return
	}
	var vec []float32
	if att.MediaType == "text/plain" {
		max := s.embedBytes()
		head := data
		if len(head) > max {
			head = head[:max] // cap before the copy
		}
		// truncateUTF8 still runs on the capped slice: the cut above may
		// have landed inside a character.
		body := truncateUTF8(string(head), max)
		vecs, err := s.embedDocument(ctx, truncateUTF8(att.Name+"\n"+body, max))
		if err != nil {
			s.Log.Warn("attachment embedding failed; attachment remains findable by name", "id", id, "name", att.Name, "error", err)
			return
		}
		vec = vecs[0]
	} else {
		// A file the model does not take is not embedded and not
		// complained about: since design doc 0046 §3.2 a bundle may carry
		// anything, and most of what it carries — an archive, a parquet
		// file, a font — was never embeddable. It stays findable by name.
		if !domain.Embeddable(att.MediaType) {
			return
		}
		fe, ok := s.Embedder.(embed.FileEmbedder)
		if !ok {
			return
		}
		var err error
		vec, err = fe.EmbedFile(ctx, att.Name, att.MediaType, data)
		if errors.Is(err, embed.ErrFileEmbeddingUnsupported) {
			return // text-only model: findable by name, no noise in the log
		}
		if err != nil {
			s.Log.Warn("attachment embedding failed; attachment remains findable by name", "id", id, "name", att.Name, "error", err)
			return
		}
	}
	if err := s.Store.UpsertAttachmentEmbedding(ctx, id, att.Name, s.Embedder.Model(), vec); err != nil {
		s.Log.Warn("storing attachment embedding failed", "id", id, "name", att.Name, "error", err)
	}
}

// Attachment returns one attachment with its bytes and records a fetch
// against the owning entry — reading the image is using the knowledge.
func (s *Service) Attachment(ctx context.Context, id, name string) (*domain.Attachment, []byte, error) {
	id, name = domain.Normalize(id), domain.Normalize(name)
	att, data, err := s.Store.GetAttachment(ctx, id, name)
	if err != nil {
		return nil, nil, err
	}
	s.recordUsage(ctx, domain.EventFetched, []string{id})
	return att, data, nil
}

// AttachmentMeta returns one attachment's metadata without its bytes —
// enough for a conditional GET (ETag = content hash) to answer 304
// without a blob-store read, and without recording a fetch: a cache
// revalidation is not a use of the knowledge.
func (s *Service) AttachmentMeta(ctx context.Context, id, name string) (*domain.Attachment, error) {
	return s.Store.GetAttachmentMeta(ctx, domain.Normalize(id), domain.Normalize(name))
}

// FillAttachments fills attachment metadata on entries in one batch
// query. The REST list surfaces (search hits, backlinks) carry it so a
// UI can render image previews without a fetch per entry; MCP search
// results stay lean for agent context (design doc 0015).
func (s *Service) FillAttachments(ctx context.Context, ks []*domain.Knowledge) error {
	if len(ks) == 0 {
		return nil
	}
	ids := make([]string, len(ks))
	for i, k := range ks {
		ids[i] = k.ID
	}
	atts, err := s.Store.ListAttachmentsBatch(ctx, ids)
	if err != nil {
		return err
	}
	for _, k := range ks {
		k.Attachments = atts[k.ID]
	}
	return nil
}

// Detach removes an attachment (the change is kept as a revision).
func (s *Service) Detach(ctx context.Context, id, name string, actor domain.Actor) error {
	if err := s.readOnly(); err != nil {
		return err
	}
	return s.Store.DeleteAttachment(ctx, domain.Normalize(id), domain.Normalize(name), actor)
}

// PutFile writes a file at a bundle path (design doc 0046 §§3.2, 3.5).
// It is Attach's counterpart for an object that belongs to no entry: a
// producer's seed data in a shared directory, a diagram two entries
// show, a markdown file carrying no type and so not a concept.
//
// Attribution is not asked for and not recorded. Whether an entry is
// shown by this file is a question its body answers (§3.3), so a file
// written under an entry's namespace, or one an entry already links,
// arrives attributed and one nothing points at is simply a file.
func (s *Service) PutFile(ctx context.Context, p string, data []byte, actor domain.Actor) (*domain.Attachment, error) {
	if err := s.readOnly(); err != nil {
		return nil, err
	}
	p = domain.Normalize(p)
	if !s.Store.HasBlobStore() {
		return nil, Unsupportedf("files are not supported without GCS: this instance stores markdown entries only; set OCHAKAI_GCS_BUCKET (design doc 0013)")
	}
	if !domain.ValidBundlePath(p) {
		return nil, Invalidf("invalid bundle path %q (path segments separated by \"/\"; segments must not start with \".\", and index.md and log.md are generated rather than stored)", p)
	}
	if len(data) == 0 {
		return nil, Invalidf("the file is empty")
	}
	if len(data) > domain.MaxAttachmentSize {
		return nil, Invalidf("the file exceeds %d MiB", domain.MaxAttachmentSize>>20)
	}
	mediaType, err := domain.DetectAttachmentMediaType(data)
	if err != nil {
		return nil, Invalidf("%v", err)
	}
	att, err := s.Store.PutFile(ctx, p, mediaType, data, actor)
	if err != nil {
		return nil, err
	}
	// The vector is keyed by the entry the file is attributed to, which
	// a file at a path nothing points at has none of. Reembed picks it up
	// as soon as an entry names it (design doc 0020).
	if owner, ok := strings.CutSuffix(p, "/"+att.Name); ok {
		s.updateAttachmentEmbedding(ctx, owner, att, data)
	}
	return att, nil
}

// GetFile returns the file at a bundle path with its bytes.
func (s *Service) GetFile(ctx context.Context, p string) (*domain.Attachment, []byte, error) {
	if !s.Store.HasBlobStore() {
		return nil, nil, Unsupportedf("files are not supported without GCS: set OCHAKAI_GCS_BUCKET (design doc 0013)")
	}
	return s.Store.GetFile(ctx, domain.Normalize(p))
}

// DeleteFile removes the file at a bundle path.
func (s *Service) DeleteFile(ctx context.Context, p string, actor domain.Actor) error {
	if err := s.readOnly(); err != nil {
		return err
	}
	return s.Store.DeleteFile(ctx, domain.Normalize(p), actor)
}
