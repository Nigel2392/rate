package rate_test

import (
	"context"
	"testing"

	"github.com/Nigel2392/rate"
)

// Define a local type to test the generic match type system
type testStringKey string

func init() {
	rate.RegisterMatchType[testStringKey](func(data testStringKey) []string {
		return []string{string(data)}
	})
}

func TestContextACL(t *testing.T) {
	acl := &rate.ContextACL[testStringKey]{}
	ctx := context.Background()

	// Unmarked context should not match
	match, err := acl.Match(ctx, "test")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if match {
		t.Error("expected unmarked context to return false")
	}

	// Marked context should match
	markedCtx := acl.Mark(ctx, true)
	match, err = acl.Match(markedCtx, "test")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !match {
		t.Error("expected marked context to return true")
	}
}

func TestListBasedACL(t *testing.T) {
	acl := rate.NewListACL[testStringKey]("allowed_1", "allowed_2")
	ctx := context.Background()

	match, err := acl.Match(ctx, "allowed_1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !match {
		t.Error("expected 'allowed_1' to match list ACL")
	}

	match, _ = acl.Match(ctx, "not_allowed")
	if match {
		t.Error("expected 'not_allowed' to NOT match list ACL")
	}

	// Test dynamically adding to the ACL
	acl.Add("allowed_3")
	match, _ = acl.Match(ctx, "allowed_3")
	if !match {
		t.Error("expected newly added 'allowed_3' to match")
	}
}

func TestFuncACL(t *testing.T) {
	var acl rate.FuncACL[testStringKey] = func(_ context.Context, d testStringKey) (bool, error) {
		return d == "magic_word", nil
	}

	match, _ := acl.Match(context.Background(), "magic_word")
	if !match {
		t.Error("expected FuncACL to match 'magic_word'")
	}

	match, _ = acl.Match(context.Background(), "normal")
	if match {
		t.Error("expected FuncACL to NOT match 'normal'")
	}
}

func TestSessionIDForType(t *testing.T) {
	// Test the custom registered type
	keys := rate.SessionIDForType(testStringKey("session_123"))
	if len(keys) == 0 || keys[0] != "session_123" {
		t.Errorf("expected session_123, got %v", keys)
	}

	// Test the built-in string Kind registration (registered in keys.go init)
	strKeys := rate.SessionIDForType("raw_string_key")
	if len(strKeys) == 0 || strKeys[0] != "raw_string_key" {
		t.Errorf("expected 'raw_string_key', got %v", strKeys)
	}

	// Test panic for an unregistered type (e.g., float64 wasn't registered in keys.go)
	defer func() {
		if r := recover(); r == nil {
			t.Errorf("expected SessionIDForType to panic on unregistered float64")
		}
	}()
	_ = rate.SessionIDForType(123.45)
}
