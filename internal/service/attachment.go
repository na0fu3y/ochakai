package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"

	"github.com/na0fu3y/ochakai/internal/domain"
	"github.com/na0fu3y/ochakai/internal/embed"
	"github.com/na0fu3y/ochakai/internal/store"
)

// File operations (design docs 0008, 0013, 0046). ochakai stores and
// serves file bytes; it never interprets them — no OCR, no
// captioning, no parsing. Reading a file and writing what it says back
// into the body is the client agent's job, like every other
// interpretation.

// updateAttachmentEmbedding refreshes an attachment's document vector
// (design doc 0020). Embedding a file is encoding, not interpretation,
// so the no-interpretation principle above holds. Text embeds via the
// text path and works on any model; image/PDF bytes need a model that
// takes file input (gemini-embedding-2) and are skipped otherwise —
// the file stays findable by name. Failures are logged, not returned:
// attach must not depend on the embedding provider being up.
func (s *Service) updateAttachmentEmbedding(ctx context.Context, att *domain.File, data []byte) {
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
		// A file's text is capped the same way a concept's is, and the
		// tail is lost the same way. It is not counted yet: the vectors
		// live in their own table with their own key, so it is a second
		// column and a second number, and a file stays findable by name
		// whatever happens to its bytes (design doc 0020). The concept is
		// where the knowledge is, and that is what stats answers for.
		vecs, _, err := s.embedDocument(ctx, truncateUTF8(att.Name+"\n"+body, max))
		if err != nil {
			s.Log.Warn("attachment embedding failed; attachment remains findable by name", "path", att.Path, "error", err)
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
			s.Log.Warn("attachment embedding failed; attachment remains findable by name", "path", att.Path, "error", err)
			return
		}
	}
	if err := s.Store.UpsertFileEmbedding(ctx, att.Path, s.Embedder.Model(), vec); err != nil {
		s.Log.Warn("storing attachment embedding failed", "path", att.Path, "error", err)
	}
}

// FillFiles fills file metadata on entries in one batch
// query. The REST list surfaces (search hits, backlinks) carry it so a
// UI can render image previews without a fetch per entry; MCP search
// results stay lean for agent context (design doc 0015).
func (s *Service) FillFiles(ctx context.Context, ks []*domain.Knowledge) error {
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
		k.Files = atts[k.ID]
	}
	return nil
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
//
// created reports which way it went, so a surface can answer 201 or 200
// — the same signal Put already gives a concept (design doc 0064).
func (s *Service) PutFile(ctx context.Context, p string, data []byte, actor domain.Actor) (att *domain.File, created bool, err error) {
	p, mediaType, err := s.settleFile(p, data)
	if err != nil {
		return nil, false, err
	}
	att, created, err = s.Store.PutFile(ctx, p, mediaType, data, actor)
	if err != nil {
		return nil, false, err
	}
	// Every file gets its vector when it lands, including one no entry
	// names yet: the vector is keyed by the path, so there is nothing to
	// wait for an owner to supply. Attribution happens at search time,
	// which is where it belongs — the day a body links this path, the
	// file ranks that concept without anything being re-embedded
	// (design doc 0091).
	s.updateAttachmentEmbedding(ctx, att, data)
	return att, created, nil
}

// settleFile is everything a file write decides before it writes: where
// the file may live, whether these bytes may be stored at all, and what
// they are. PlanFile is this step and no more (design doc 0061).
func (s *Service) settleFile(p string, data []byte) (path, mediaType string, err error) {
	if err := s.readOnly(); err != nil {
		return "", "", err
	}
	p = domain.Normalize(p)
	if !s.Store.HasBlobStore() {
		return "", "", Unsupportedf("files are not supported without GCS: this instance stores markdown concepts only; set OCHAKAI_GCS_BUCKET (design doc 0075 §1)")
	}
	if !domain.ValidBundlePath(p) {
		return "", "", Invalidf("invalid bundle path %q (path segments separated by \"/\"; segments must not start with \".\", and index.md and log.md are generated rather than stored)", p)
	}
	if len(data) == 0 {
		return "", "", Invalidf("the file is empty")
	}
	if len(data) > domain.MaxFileSize {
		return "", "", Invalidf("the file exceeds %d MiB", domain.MaxFileSize>>20)
	}
	mediaType, err = domain.DetectFileMediaType(data)
	if err != nil {
		return "", "", Invalidf("%v", err)
	}
	return p, mediaType, nil
}

// PlanFile is PutFile with the write withheld: the same refusals, and the
// same three-way answer about what the write would do (design doc 0061).
//
// The file's own address is the one thing a concept's plan does not have
// to ask about: a path a concept holds is not a path a file may take, and
// the store says so only from inside the write. Asking here is what keeps
// a bundle carrying a markdown file that lost its type key from passing a
// dry run and failing the import.
func (s *Service) PlanFile(ctx context.Context, p string, data []byte, actor domain.Actor) (att *domain.File, created, changed bool, err error) {
	p, mediaType, err := s.settleFile(p, data)
	if err != nil {
		return nil, false, false, err
	}
	sum := sha256.Sum256(data)
	hash := hex.EncodeToString(sum[:])
	old, err := s.Store.GetFileMeta(ctx, p)
	switch {
	case err == nil:
		return old, false, old.SHA256 != hash, nil
	case !errors.Is(err, store.ErrNotFound):
		return nil, false, false, err
	}
	if id, isMarkdown := strings.CutSuffix(p, ".md"); isMarkdown {
		if _, err := s.Store.Get(ctx, id); err == nil {
			return nil, false, false, fmt.Errorf("%w: %s is a concept", store.ErrAlreadyExists, p)
		}
	}
	// The values the write would stamp, stamped without the write: a
	// created file resolves both, so the plan does too rather than
	// putting an empty actor and a zero time on the wire (design doc
	// 0064). A file row keeps kind and name only, so the plan says no
	// more than a later GET would.
	return &domain.File{
		Name: p[strings.LastIndex(p, "/")+1:], MediaType: mediaType,
		Size: int64(len(data)), SHA256: hash, Path: p,
		CreatedBy: domain.Actor{Kind: actor.Kind, Name: actor.Name},
		CreatedAt: store.NowStored(),
	}, true, true, nil
}

// GetFile returns the file at a bundle path with its bytes.
func (s *Service) GetFile(ctx context.Context, p string) (*domain.File, []byte, error) {
	if !s.Store.HasBlobStore() {
		return nil, nil, Unsupportedf("files are not supported without GCS: set OCHAKAI_GCS_BUCKET (design doc 0075 §1)")
	}
	return s.Store.GetFile(ctx, domain.Normalize(p))
}

// GetFileMeta returns a file's metadata without its bytes — the only way
// to answer Accept: application/json on a file's own address without
// paying for the download (design doc 0064; 0046 §3.5's table already
// promised this column, the REST face just ignored the header).
func (s *Service) GetFileMeta(ctx context.Context, p string) (*domain.File, error) {
	if !s.Store.HasBlobStore() {
		return nil, Unsupportedf("files are not supported without GCS: set OCHAKAI_GCS_BUCKET (design doc 0075 §1)")
	}
	return s.Store.GetFileMeta(ctx, domain.Normalize(p))
}

// DeleteFile removes the file at a bundle path, and then whatever bytes
// the removal left unreferenced.
func (s *Service) DeleteFile(ctx context.Context, p string, actor domain.Actor) error {
	if err := s.readOnly(); err != nil {
		return err
	}
	if err := s.Store.DeleteFile(ctx, domain.Normalize(p), actor); err != nil {
		return err
	}
	s.sweepBlobs(ctx)
	return nil
}

// sweepBlobs reclaims bytes nothing references any more. An error is a
// warning, not a failure: the rows the caller asked to destroy are
// already gone, and the sweep is self-healing — the next destructive
// operation finishes the job (store/sweep.go).
func (s *Service) sweepBlobs(ctx context.Context) {
	n, err := s.Store.SweepBlobs(ctx)
	if err != nil {
		s.Log.Warn("blob sweep failed; unreferenced bytes remain until the next sweep", "error", err)
		return
	}
	if n > 0 {
		s.Log.Info("blob sweep", "deleted", n)
	}
}
