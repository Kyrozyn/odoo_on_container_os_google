package auth

import (
	"encoding/base32"
	"path/filepath"
	"testing"
	"time"

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
	if _, err := mgr2.ValidateToken(tok); err == nil {
		t.Fatal("expected old token to be revoked after ChangePassword")
	}
	if err := mgr2.ChangePassword("short"); err == nil {
		t.Fatal("expected password shorter than 10 chars to fail")
	}
}

func TestTOTPAndRateLimiter(t *testing.T) {
	secret, uri, err := GenerateTOTPSecret("RunOdoo", "admin")
	if err != nil || secret == "" || uri == "" {
		t.Fatalf("GenerateTOTPSecret failed: %v", err)
	}
	key, err := base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(secret)
	if err != nil {
		t.Fatalf("decode secret failed: %v", err)
	}
	now := time.Now()
	validCode := totpAtCounter(key, uint64(now.Unix()/30))
	if !verifyTOTPAt(secret, validCode, now) {
		t.Fatal("expected valid TOTP code to verify")
	}
	if verifyTOTPAt(secret, "000000", now) && validCode != "000000" {
		t.Fatal("expected invalid TOTP code to fail")
	}

	rl := NewLoginRateLimiter()
	for i := 0; i < 3; i++ {
		if ok, _ := rl.Check("1.2.3.4"); !ok {
			t.Fatalf("expected attempt %d to be allowed", i)
		}
		rl.RecordFailure("1.2.3.4", 3, time.Minute)
	}
	if ok, retry := rl.Check("1.2.3.4"); ok || retry <= 0 {
		t.Fatalf("expected IP to be locked out after 3 failures, got ok=%v retry=%d", ok, retry)
	}
	rl.RecordSuccess("1.2.3.4")
	if ok, _ := rl.Check("1.2.3.4"); !ok {
		t.Fatal("expected IP to be unlocked after RecordSuccess")
	}
}
