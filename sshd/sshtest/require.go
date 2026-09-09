package sshtest

import (
	"fmt"
	"testing"
)

// Require is a helper for common assertions that fail the test.
type Require struct {
	t testing.TB
}

// Env returns a Require helper for assertions.
func (e *Environment) Require() *Require {
	return &Require{t: e.t}
}

// NoError fails the test if err is not nil.
func (r *Require) NoError(err error, msgAndArgs ...interface{}) {
	r.t.Helper()
	if err != nil {
		if len(msgAndArgs) > 0 {
			r.t.Fatalf("%s: %v", fmt.Sprint(msgAndArgs...), err)
		} else {
			r.t.Fatalf("unexpected error: %v", err)
		}
	}
}

// Equal fails the test if expected != actual.
func (r *Require) Equal(expected, actual interface{}, msgAndArgs ...interface{}) {
	r.t.Helper()
	if expected != actual {
		if len(msgAndArgs) > 0 {
			r.t.Fatalf("%s: expected %v, got %v", fmt.Sprint(msgAndArgs...), expected, actual)
		} else {
			r.t.Fatalf("expected %v, got %v", expected, actual)
		}
	}
}

// Contains fails the test if s does not contain substr.
func (r *Require) Contains(s, substr string, msgAndArgs ...interface{}) {
	r.t.Helper()
	if len(s) == 0 || len(substr) == 0 {
		r.t.Fatalf("contains check failed: s=%q, substr=%q", s, substr)
		return
	}
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return
		}
	}
	if len(msgAndArgs) > 0 {
		r.t.Fatalf("%s: %q does not contain %q", fmt.Sprint(msgAndArgs...), s, substr)
	} else {
		r.t.Fatalf("%q does not contain %q", s, substr)
	}
}

// True fails the test if condition is false.
func (r *Require) True(condition bool, msgAndArgs ...interface{}) {
	r.t.Helper()
	if !condition {
		if len(msgAndArgs) > 0 {
			r.t.Fatalf("%s: expected true", fmt.Sprint(msgAndArgs...))
		} else {
			r.t.Fatal("expected true")
		}
	}
}
