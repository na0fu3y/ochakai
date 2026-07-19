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

// Detach removes an attachment (the change is kept as a revision).
func (s *Service) Detach(ctx context.Context, id, name string, actor domain.Actor) error {
	return s.Store.DeleteAttachment(ctx, id, name, actor)
}
