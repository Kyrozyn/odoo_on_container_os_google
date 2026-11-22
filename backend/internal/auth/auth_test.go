package auth

import (
	"path/filepath"
	"testing"

	"github.com/runodoo/runodoo/internal/store"
)

func TestAuthPasswordAndJWT(t *testing.T) {
	dir := t.TempDir()
	st, err := store.New(filepath.Join(dir, "state.json"), "runodoo123", "runodoo-pg-shared")
	if err != nil {
		t.Fatalf("store.New failed: %v", err)
	}

	mgr := NewManager(st)
	if !mgr.VerifyPassword("runodoo123") {
		t.Fatal("expected default password to verify")
	}
	if mgr.VerifyPassword("wrong") {
		t.Fatal("expected wrong password to fail")
	}

	tok, err := mgr.GenerateToken()
	if err != nil {
		t.Fatalf("GenerateToken failed: %v", err)
	}
	claims, err := mgr.ValidateToken(tok)
	if err != nil {
		t.Fatalf("ValidateToken failed: %v", err)
	}
	if claims.Sub != "admin" {
		t.Fatalf("expected sub=admin, got %s", claims.Sub)
	}

	if err := mgr.ChangePassword("newSecret123"); err != nil {
		t.Fatalf("ChangePassword failed: %v", err)
	}
	if !mgr.VerifyPassword("newSecret123") {
		t.Fatal("expected new password to verify")
	}

	// Verify persistence across reload
	st2, err := store.New(filepath.Join(dir, "state.json"), "runodoo123", "runodoo-pg-shared")
	if err != nil {
		t.Fatalf("store reload failed: %v", err)
	}
	mgr2 := NewManager(st2)
	if !mgr2.VerifyPassword("newSecret123") {
		t.Fatal("expected reloaded store to retain new password")
	}
}
