// Embeddings: keeping the vector space in step with the documents. The
// text a concept is embedded as, the backfill pass over what has no vector
// for the current model, and the cursor that lets it resume (design doc
// 0053).
package service

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/na0fu3y/ochakai/internal/domain"
	"github.com/na0fu3y/ochakai/internal/embed"
)

// updateEmbedding refreshes the stored document vector. Failures are logged,
// not returned: writes must not depend on the embedding provider being up.
func (s *Service) updateEmbedding(ctx context.Context, k *domain.Knowledge) {
	if s.Embedder == nil {
		return
	}
	text, full := embeddingText(k, s.embedBytes())
	vecs, embedded, err := s.embedDocument(ctx, text)
	if err != nil {
		s.Log.Warn("document embedding failed; concept remains findable by the lexical half of search", "type", k.Type, "id", k.ID, "error", err)
		return
	}
	if err := s.Store.UpsertEmbedding(ctx, k.ID, s.Embedder.Model(), vecs[0], !full || embedded < len(text)); err != nil {
		s.Log.Warn("storing embedding failed", "id", k.ID, "error", err)
	}
}

// embedBytes is how much text this deployment's model can be handed.
//
// The real limit is tokens, and UTF-8 bytes track tokens across scripts
// far better than characters do — but only roughly, and the ratio is
// worst where it matters most: Japanese runs three bytes per character
// and can approach one token per character. So the cap is deliberately
// conservative, covers the whole text (envelope included), and comes from
// the model rather than from a constant: the window it has to fit inside
// is a property of the model, and applying the smallest model's budget to
// a model with four times the window throws away three quarters of it —
// silently, since a concept truncated for no reason still embeds fine and
// just carries less of itself.
//
// A provider that does not say (any embedder but Vertex, including the
// fakes in tests) gets the conservative floor. The estimate is still an
// estimate, so embedDocument retries shorter rather than trusting it.
func (s *Service) embedBytes() int {
	if l, ok := s.Embedder.(embed.InputLimited); ok {
		return l.MaxInputBytes()
	}
	return embed.ConservativeInputBytes
}

// ReembedResult reports what a Reembed pass did. Concepts and Files are
// this pass's own progress, not a census of the corpus. Missing
// is how many concepts and files still have no vector once the pass is
// over — the number that decides whether the operator runs it again, so
// it counts what is actually left rather than what this pass did not get
// to. Cursor is where the next pass resumes; empty means the corpus is
// exhausted.
type ReembedResult struct {
	Concepts int    `json:"concepts"`
	Files    int    `json:"files"`
	Failed   int    `json:"failed"`
	Missing  int    `json:"missing"`
	Cursor   string `json:"cursor,omitempty"`
}

// A reembed pass is one HTTP request, and one concept is one call to the
// embedding provider — gemini-embedding-001 takes a single text per
// request, so the passes are sequential by construction. At a few hundred
// milliseconds each, a pass of 500 is minutes of request time, and Cloud
// Run's default request timeout is 300 seconds: a "bounded" pass that
// cannot finish inside a request is not bounded, it just fails late and
// reports nothing (the work it did is still written).
//
// So a pass is sized to finish, and the CLI repeats it until nothing is
// left. The ceiling is for operators who know their own timeout.
const (
	defaultReembedPass = 200
	maxReembedPass     = 5000
)

// Reembed fills in the vectors that concept writes never produced. Vectors
// are written on create and update only, so a base loaded before semantic
// search was reachable has none, and hybrid search silently stays
// lexical-only until every concept happens to be rewritten. Switching models
// has the same shape: the old vectors are in a space nothing queries any
// more (design doc 0020), as does a changed dimension, which rebuilds the
// tables empty (design doc 0053 §3).
//
// Failures are counted, not fatal: one concept the provider chokes on must
// not strand the rest of the corpus.
func (s *Service) Reembed(ctx context.Context, cursor string, limit int) (*ReembedResult, error) {
	if err := s.readOnly(); err != nil {
		return nil, err
	}
	if s.Embedder == nil {
		return nil, Unsupportedf("semantic search is not enabled on this deployment: it is the default on Google Cloud once the service identity may call Vertex AI (design doc 0080 §1)")
	}
	limit, err := checkedLimit(limit, defaultReembedPass, maxReembedPass)
	if err != nil {
		return nil, err
	}
	entryCursor, filePath, err := decodeReembedCursor(cursor)
	if err != nil {
		return nil, err
	}
	ids, err := s.Store.ListUnembedded(ctx, s.Embedder.Model(), entryCursor, limit)
	if err != nil {
		return nil, err
	}
	res := &ReembedResult{}
	for _, id := range ids {
		k, err := s.Store.Get(ctx, id)
		if err != nil {
			res.Failed++
			s.Log.Warn("reembed: concept disappeared", "id", id, "error", err)
			continue
		}
		text, full := embeddingText(k, s.embedBytes())
		vecs, embedded, err := s.embedDocument(ctx, text)
		if err != nil {
			res.Failed++
			s.Log.Warn("reembed: embedding failed", "id", id, "error", err)
			continue
		}
		if err := s.Store.UpsertEmbedding(ctx, id, s.Embedder.Model(), vecs[0], !full || embedded < len(text)); err != nil {
			res.Failed++
			s.Log.Warn("reembed: storing failed", "id", id, "error", err)
			continue
		}
		res.Concepts++
	}
	if len(ids) > 0 {
		entryCursor = ids[len(ids)-1]
	}
	// Attachments carry vectors of their own (design doc 0020) and only
	// attach writes them, so dropping the embedding tables to change
	// dimensions leaves every file findable by name alone. Concepts come
	// first and attachments fill the rest of the pass, so a corpus with
	// both makes progress on both.
	if rest := limit - len(ids); rest > 0 {
		filePath, err = s.reembedFiles(ctx, res, filePath, rest)
		if err != nil {
			return nil, err
		}
	}
	// Counted after the pass, so it is what is left — not what was left
	// minus what we did, which double-counts the work and reports a
	// bounded pass as a finished one. Concepts this pass failed on are
	// still unembedded and belong in the total.
	missing, err := s.Store.CountUnembedded(ctx, s.Embedder.Model())
	if err != nil {
		// The pass itself succeeded; refusing to report it because the
		// tally failed would be worse than reporting it without one.
		s.Log.Warn("reembed: counting what is left failed", "error", err)
		return res, nil
	}
	fileMissing, err := s.Store.CountUnembeddedFiles(ctx, s.Embedder.Model(), s.embeddableFileTypes())
	if err != nil {
		s.Log.Warn("reembed: counting files left failed", "error", err)
	}
	res.Missing = missing + fileMissing
	if res.Missing > 0 {
		res.Cursor = encodeReembedCursor(entryCursor, filePath)
	}
	return res, nil
}

// embeddableFileTypes is what this deployment could hand the model, which
// is what belongs in the file queue.
//
// A text-only embedder is the case that matters: the media types a file
// vector could ever cover are a property of the model, and on
// gemini-embedding-001 an image is not a file awaiting a vector — it is a
// file that will never have one. Counting it as pending left `ochakai
// reembed` reporting work outstanding on every pass, forever, and
// pointing at a server log that said nothing, because declining a file
// the model does not take is the one failure that is deliberately silent.
func (s *Service) embeddableFileTypes() []string {
	if _, ok := s.Embedder.(embed.FileEmbedder); ok {
		return domain.EmbeddableMediaTypes()
	}
	return []string{"text/plain"}
}

// reembedFiles re-embeds up to limit files, returning the position to
// resume from. Failures are counted like concept failures: a file the
// provider rejects must not strand the rest.
func (s *Service) reembedFiles(ctx context.Context, res *ReembedResult, after string, limit int) (string, error) {
	pending, err := s.Store.ListUnembeddedFiles(ctx, s.Embedder.Model(), s.embeddableFileTypes(), after, limit)
	if err != nil {
		return after, err
	}
	for _, p := range pending {
		att, data, err := s.Store.GetFile(ctx, p)
		if err != nil {
			res.Failed++
			s.Log.Warn("reembed: file disappeared", "path", p, "error", err)
			continue
		}
		// The same path a write uses, so the vectors match what a fresh
		// write would have produced; it logs and swallows its own
		// failures, which is why the count below is of rows written.
		before := s.fileVectorCount(ctx, p)
		s.updateAttachmentEmbedding(ctx, att, data)
		if s.fileVectorCount(ctx, p) > before {
			res.Files++
		} else {
			res.Failed++
		}
		after = p
	}
	return after, nil
}

// attachmentVectorCount reports whether a current-model vector exists,
// so a pass can tell an embedding that landed from one the provider (or
// the model's file support) declined.
func (s *Service) fileVectorCount(ctx context.Context, path string) int {
	n, err := s.Store.CountFileEmbedding(ctx, path, s.Embedder.Model())
	if err != nil {
		return 0
	}
	return n
}

// reembedCursorTag marks a cursor as belonging to the reembed pass rather
// than to a listing (internal/service/cursor.go) — both share cursorVersion
// as their first field, so without a second discriminator a listing cursor
// with one ordering key and a reembed cursor decode to the same field
// count and one would silently be accepted as the other.
const reembedCursorTag = "reembed"

// The reembed cursor carries two positions — the last concept id and the
// last file path — packed and base64url-encoded the same way encodeCursor
// packs a listing position: a version prefix so a later change of shape is
// a 400 with a sentence, and an opaque token rather than raw ids so the
// wire survives proxies and WAFs that balk at a NUL byte and needs no
// client-side encoding to round-trip. "\x00" cannot occur in an id
// (design doc 0017) or a bundle path.
//
// It carried three until design doc 0091 re-keyed the file vectors, when
// the file half stopped being a (concept, file) pair. A cursor from
// before that is one field longer and is refused by the length check
// below — which is the shape change the version prefix exists for, and
// costs nothing in practice: the migration that re-keys the table empties
// it, so a pass resumed across the upgrade had nothing left to resume.
func encodeReembedCursor(entryID, filePath string) string {
	if entryID == "" && filePath == "" {
		return ""
	}
	raw := strings.Join([]string{cursorVersion, reembedCursorTag, entryID, filePath}, "\x00")
	return base64.RawURLEncoding.EncodeToString([]byte(raw))
}

// decodeReembedCursor reads a cursor back, refusing anything this server
// did not hand out — including a cursor from a listing's own cursor
// space, which carries a different tag in the same position.
func decodeReembedCursor(c string) (entryID, filePath string, err error) {
	if c == "" {
		return "", "", nil
	}
	raw, decErr := base64.RawURLEncoding.DecodeString(c)
	if decErr != nil {
		return "", "", malformedCursor()
	}
	parts := strings.Split(string(raw), "\x00")
	if len(parts) != 4 || parts[0] != cursorVersion || parts[1] != reembedCursorTag {
		return "", "", malformedCursor()
	}
	return parts[2], parts[3], nil
}

// embedDocument embeds text, halving it and retrying while the model says
// it is too long, and reports how many bytes the vector was computed
// over. Byte budgets only approximate a token count, and the
// approximation is worst for Japanese; without this, a concept that
// overruns is dropped from vector search silently — logged, but with the
// write reported as a success. Halving converges in a couple of rounds
// and only runs on the rejection, so an outage still costs one call.
//
// The returned length is what makes the halving visible past this
// function. It is the second way a concept loses its tail — the estimate
// in embeddingText is the first — and a caller that only knew about the
// first would record a vector as whole when the provider had just made
// the product throw away seven eighths of it.
func (s *Service) embedDocument(ctx context.Context, text string) ([][]float32, int, error) {
	// Four attempts halve a 5000-byte text to 625, well under any
	// plausible window; the floor below stops it before it embeds a
	// fragment too short to mean anything.
	for attempt := range 4 {
		vecs, err := s.Embedder.Embed(ctx, embed.TaskDocument, []string{text})
		if err == nil {
			if attempt > 0 {
				s.Log.Info("embedded after shortening", "bytes", len(text), "attempts", attempt+1)
			}
			return vecs, len(text), nil
		}
		if !errors.Is(err, embed.ErrInputTooLong) || len(text) < 500 {
			return nil, 0, err
		}
		text = truncateUTF8(text, len(text)/2)
	}
	return nil, 0, fmt.Errorf("still over the embedding model's input limit after shortening")
}

// embeddingText builds the document text to embed: envelope fields plus the
// golden query question, body truncated to keep within model input limits.
// The id leads (design doc 0022): with title optional, the filename may be
// the concept's only name, and the path carries the domain hierarchy.
//
// full is false when the concept did not fit. The caller stores it, and
// what it buys is the difference between a knowledge base that has this
// problem and one that merely might: everything downstream of a
// truncated vector looks exactly like everything downstream of a whole
// one (design doc 0089).
func embeddingText(k *domain.Knowledge, max int) (text string, full bool) {
	parts := []string{k.ID, k.Title, k.Description, strings.Join(k.Tags, " ")}
	if q, ok := k.Attrs["question"].(string); ok {
		parts = append(parts, q)
	}
	parts = append(parts, k.Body)
	// The cap covers the joined text: a concept whose envelope is long
	// (a deep path, a paragraph of description) must not push the total
	// past the window just because the body fit on its own.
	joined := strings.TrimSpace(strings.Join(parts, "\n"))
	return truncateUTF8(joined, max), len(joined) <= max
}

// truncateUTF8 caps s at max bytes and then drops a trailing partial rune.
// A plain s[:max] lands mid-character two times in three on Japanese text
// (3 bytes per character), and half a character is not valid UTF-8.
func truncateUTF8(s string, max int) string {
	if len(s) > max {
		s = s[:max]
	}
	for len(s) > 0 {
		// DecodeLastRuneInString reports (RuneError, 1) for a byte that
		// cannot end a valid encoding; a real U+FFFD decodes with size 3.
		if r, size := utf8.DecodeLastRuneInString(s); r != utf8.RuneError || size > 1 {
			break
		}
		s = s[:len(s)-1]
	}
	return s
}
