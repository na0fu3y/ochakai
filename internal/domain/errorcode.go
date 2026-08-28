package domain

import "slices"

// An error response carries a machine-readable code beside its prose.
//
// The prose is for a person and is free to be reworded in any release
// (docs/compatibility.md); the code is the part a client may branch on.
// Without it, the only way to tell two failures apart was to match the
// sentence — and three of the conditions below answer 409, so a client
// that wanted to distinguish "this id is taken" from "there is no
// rejection to withdraw" had to match prose the policy says may move.
//
// Where the status already determines the condition the code repeats it,
// and that is deliberate: a code present on every error is one a client
// can switch on without first asking whether this particular failure has
// one.
//
// These are counted as vocabulary (docs/surface.md, VOCAB): a word a
// client holds onto is a word somebody has to learn, whichever surface
// teaches it.
const (
	// CodeInvalid — the request was understood and refused: 400, and the
	// auth middleware's 401, where what failed is likewise the request —
	// its credential — rather than any condition of the knowledge.
	CodeInvalid = "invalid"
	// CodeForbidden — this caller may not do this: 403. A delegation
	// header from a caller that is not an allowed delegator
	// (OCHAKAI_DELEGATING_CALLERS), or a rejected identity.
	CodeForbidden = "forbidden"
	// CodeReadOnly — the deployment declines every write: 403. Distinct
	// from CodeForbidden because the fix is the deployment's posture,
	// not the caller's identity.
	CodeReadOnly = "read_only"
	// CodeNotFound — no object at this address: 404.
	CodeNotFound = "not_found"
	// CodeMethodNotAllowed — the address exists, this method does not: 405.
	CodeMethodNotAllowed = "method_not_allowed"
	// CodeAlreadyExists — the id is taken: 409.
	CodeAlreadyExists = "already_exists"
	// CodeNotDeleted — purge was asked of a live concept: 409. Delete
	// first; destruction takes two steps (design doc 0031).
	CodeNotDeleted = "not_deleted"
	// CodePreconditionFailed — the If-Match precondition did not hold:
	// 412. The concept changed since it was read (design doc 0030).
	CodePreconditionFailed = "precondition_failed"
	// CodeTooLarge — the body is over the limit this route accepts: 413.
	CodeTooLarge = "too_large"
	// CodeUnsupported — this deployment cannot do this at all: 501,
	// e.g. a file write with no bucket configured (design doc 0013).
	CodeUnsupported = "unsupported"
	// CodeInternal — something failed that the caller cannot act on:
	// 500. The prose is deliberately uninformative; the detail is in the
	// server's log rather than on the wire.
	CodeInternal = "internal"
)

// ErrorCodes is every code an error response can carry, sorted. The
// contract declares the same list as an enum, and a code the product can
// say that the contract cannot is what the sync test between them
// catches.
var ErrorCodes = []string{
	CodeAlreadyExists,
	CodeForbidden,
	CodeInternal,
	CodeInvalid,
	CodeMethodNotAllowed,
	CodeNotDeleted,
	CodeNotFound,
	CodePreconditionFailed,
	CodeReadOnly,
	CodeTooLarge,
	CodeUnsupported,
}

// ValidErrorCode reports whether c is one of the codes above.
func ValidErrorCode(c string) bool { return slices.Contains(ErrorCodes, c) }
