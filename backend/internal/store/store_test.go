package store

import (
	"encoding/json"
	"path/filepath"
	"testing"
)

func TestStoreGetNeverReturnsNilSlices(t *testing.T) {
	dir := t.TempDir()
	st, err := New(filepath.Join(dir, "state.json"), "runodoo123", "runodoo-pg")
	if err != nil {
		t.Fatalf("store.New failed: %v", err)
	}

	got := st.Get()
	for name, val := range map[string]any{
		"Instances":       got.Instances,
		"AddonRepos":      got.AddonRepos,
		"Backups":         got.Backups,
		"BackupSchedules": got.BackupSchedules,
	} {
		raw, err := json.Marshal(val)
		if err != nil {
			t.Fatalf("marshal %s failed: %v", name, err)
		}
		if string(raw) != "[]" {
			t.Fatalf("expected %s to marshal as [], got %s", name, string(raw))
		}
	}
}
