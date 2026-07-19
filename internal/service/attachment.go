package service

import (
	"context"

	"github.com/na0fu3y/ochakai/internal/domain"
)

// Attachment operations (design docs 0008, 0013). ochakai stores and
// serves attachment bytes; it never interprets them — no OCR, no
// captioning, no parsing. Reading a file and writing what it says back
// into the body is the client agent's job, like every other
// interpretation.

// Attach stores data as an attachment of the entry, replacing any
// attachment of the same name. The media type is sniffed from the bytes,
// never taken from the caller. okfPath preserves a foreign bundle
// location for round-trips; "" for attachments born here.
func (s *Service) Attach(ctx context.Context, id, name, okfPath string, data []byte, actor domain.Actor) (*domain.Attachment, error) {
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
	return s.Store.PutAttachment(ctx, id, name, mediaType, okfPath, data, actor)
}

// Attachment returns one attachment with its bytes and records a fetch
// against the owning entry — reading the image is using the knowledge.
func (s *Service) Attachment(ctx context.Context, id, name string) (*domain.Attachment, []byte, error) {
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
	return s.Store.GetAttachmentMeta(ctx, id, name)
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
	return s.Store.DeleteAttachment(ctx, id, name, actor)
}
