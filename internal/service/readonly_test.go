package service

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/na0fu3y/ochakai/internal/config"
	"github.com/na0fu3y/ochakai/internal/domain"
)

// TestReadOnlyRefusesEveryWrite is the guarantee design doc 0040 rests on:
// on a read-only deployment, nothing that changes knowledge succeeds.
//
// The list is derived by reflection rather than written out, so a new
// write method cannot be added without either guarding it or failing
// here. That is the point — the check lives in the service precisely so
// that a later endpoint is covered without its author knowing about it
// (0040 §2.2). Reads are excluded by name; anything else must refuse.
func TestReadOnlyRefusesEveryWrite(t *testing.T) {
	// Methods that read. Everything else on Service is expected to refuse
	// when the deployment is read-only.
	reads := map[string]bool{
		"Get": true, "Search": true, "SearchOrList": true, "Context": true,
		"Revisions": true, "Usage": true, "Browse": true,
		// Counting the review queues is a read of the same feeds
		// (design doc 0049): a read-only deployment still shows how much
		// work is waiting, it just cannot be the one to do it. Stats
		// reads the same ledgers one instance wide (design doc 0051),
		// and still says what it was asked for and could not answer.
		"Queues": true, "Stats": true,
		// The two derived files are reads of the same tree and ledger
		// (design doc 0046 §§3.7-3.8).
		"IndexDocument": true, "LogDocument": true,
		"Export": true, "Attachment": true, "AttachmentMeta": true,
		"GetFile":         true,
		"FillAttachments": true, "RefuseIfCurated": true,
		"RefuseIfRevivingCurated": true, "Close": true,
	}
	// Zero arguments of the right type for each parameter, so every method
	// can be called far enough to hit (or miss) the guard.
	arg := func(t reflect.Type) reflect.Value {
		switch t {
		case reflect.TypeOf((*context.Context)(nil)).Elem():
			return reflect.ValueOf(context.Background())
		case reflect.TypeOf(&domain.Knowledge{}):
			return reflect.ValueOf(&domain.Knowledge{Type: "Metric", ID: "x", Title: "x"})
		case reflect.TypeOf(&time.Time{}):
			return reflect.ValueOf((*time.Time)(nil))
		}
		return reflect.Zero(t)
	}

	svc := &Service{Config: &config.Config{ReadOnly: true}}
	rv := reflect.ValueOf(svc)
	checked := 0
	for i := 0; i < rv.NumMethod(); i++ {
		name := rv.Type().Method(i).Name
		if reads[name] {
			continue
		}
		m := rv.Method(i)
		mt := m.Type()
		in := make([]reflect.Value, mt.NumIn())
		for j := range in {
			in[j] = arg(mt.In(j))
		}
		// A write that is not guarded reaches a nil Store and panics; that
		// is a failure of this test's subject, not of the test.
		var out []reflect.Value
		func() {
			defer func() {
				if r := recover(); r != nil {
					t.Errorf("%s is not guarded by readOnly: it ran on to %v", name, r)
				}
			}()
			out = m.Call(in)
		}()
		if out == nil {
			continue
		}
		last := out[len(out)-1]
		if last.IsNil() || !errors.Is(last.Interface().(error), ErrReadOnly) {
			t.Errorf("%s on a read-only service returned %v, want ErrReadOnly", name, last.Interface())
			continue
		}
		checked++
	}
	// If the reflection ever stops finding methods, the test would pass
	// vacuously.
	if checked < 10 {
		t.Errorf("only %d write methods checked; the reflection is not finding them", checked)
	}
}

// A writable service must not refuse: the guard has to be off by default,
// or every existing deployment breaks on upgrade.
func TestWritableByDefault(t *testing.T) {
	for _, cfg := range []*config.Config{nil, {}, {ReadOnly: false}} {
		if err := (&Service{Config: cfg}).readOnly(); err != nil {
			t.Errorf("Config %+v: readOnly() = %v, want nil", cfg, err)
		}
	}
}

// The message has to say what happened and that it is the deployment's
// choice, not the caller's mistake — it reaches an agent as a tool error.
func TestReadOnlyErrorExplainsItself(t *testing.T) {
	msg := ErrReadOnly.Error()
	for _, want := range []string{"read-only", "does not change"} {
		if !strings.Contains(msg, want) {
			t.Errorf("ErrReadOnly = %q, missing %q", msg, want)
		}
	}
}
