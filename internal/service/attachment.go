package service

import (
	"context"

	"github.com/na0fu3y/ochakai/internal/domain"
	"github.com/na0fu3y/ochakai/internal/store"
)

// Attachment operations (design doc 0008). ochakai stores and serves
// image bytes; it never interprets them — no OCR, no captioning. Reading
// an image and writing what it shows back into the body is the client
// agent's job, like every other interpretation.

// Attach stores data as an attachment of the entry, replacing any
// attachment of the same name. The media type is sniffed from the bytes,
// never taken from the caller. okfPath preserves a foreign bundle
// location for round-trips; "" for attachments born here.
func (s *Service) Attach(ctx context.Context, typ domain.Type, id, name, okfPath string, data []byte, actor domain.Actor) (*domain.Attachment, error) {
	if err := domain.ValidateAttachment(name, len(data)); err != nil {
		return nil, err
	}
	mediaType, err := domain.DetectAttachmentMediaType(data)
	if err != nil {
		return nil, err
	}
	return s.Store.PutAttachment(ctx, typ, id, name, mediaType, okfPath, data, actor)
}

// Attachment returns one attachment with its bytes and records a fetch
// against the owning entry — reading the image is using the knowledge.
func (s *Service) Attachment(ctx context.Context, typ domain.Type, id, name string) (*domain.Attachment, []byte, error) {
	att, data, err := s.Store.GetAttachment(ctx, typ, id, name)
	if err != nil {
		return nil, nil, err
	}
	s.recordUsage(ctx, domain.EventFetched, []store.EventTarget{{Type: typ, ID: id}})
	return att, data, nil
}

// Detach removes an attachment (the change is kept as a revision).
func (s *Service) Detach(ctx context.Context, typ domain.Type, id, name string, actor domain.Actor) error {
	return s.Store.DeleteAttachment(ctx, typ, id, name, actor)
}
