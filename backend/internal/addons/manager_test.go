package addons

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/runodoo/runodoo/internal/config"
	"github.com/runodoo/runodoo/internal/store"
)

func TestScanModules(t *testing.T) {
	dir := t.TempDir()
	modDir := filepath.Join(dir, "custom_pack", "my_sale_ext")
	if err := os.MkdirAll(modDir, 0o755); err != nil {
		t.Fatal(err)
	}
	manifest := `{
    'name': 'My Sales Extension',
    'version': '18.0.1.0.0',
    'summary': 'Extends sales workflow',
    'author': 'RunOdoo',
    'depends': ['base', 'sale'],
}`
	if err := os.WriteFile(filepath.Join(modDir, "__manifest__.py"), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}

	mods := ScanModules(dir)
	if len(mods) != 1 {
		t.Fatalf("expected 1 module, got %d", len(mods))
	}
	if mods[0].TechnicalName != "my_sale_ext" || mods[0].Name != "My Sales Extension" || mods[0].Version != "18.0.1.0.0" {
		t.Fatalf("unexpected module parsed: %+v", mods[0])
	}
	if len(mods[0].Depends) != 2 || mods[0].Depends[0] != "base" || mods[0].Depends[1] != "sale" {
		t.Fatalf("unexpected depends: %+v", mods[0].Depends)
	}
}

func TestRecommendedAddonsSelectiveMount(t *testing.T) {
	dir := t.TempDir()
	recDir := filepath.Join(dir, "recommended-addons")
	for _, mod := range []string{"om_theme_lite", "execute_sql"} {
		p := filepath.Join(recDir, mod)
		if err := os.MkdirAll(p, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(p, "__manifest__.py"), []byte("{'name': '"+mod+"'}"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	cfg := &config.Config{
		DataDir:                  dir,
		HostDataDir:              dir,
		RecommendedAddonsDir:     recDir,
		HostRecommendedAddonsDir: recDir,
	}
	st, err := store.New(filepath.Join(dir, "state.json"), "pass123", "pg")
	if err != nil {
		t.Fatal(err)
	}
	mgr := NewManager(cfg, st)

	list := mgr.ListRecommendedAddons()
	if len(list) != 2 {
		t.Fatalf("expected 2 recommended addons, got %d", len(list))
	}

	// Select only om_theme_lite
	pathStr, binds := mgr.ComputeAddonsPath(nil, []string{"om_theme_lite"})
	if !strings.Contains(pathStr, "/mnt/recommended-addons") {
		t.Fatalf("expected /mnt/recommended-addons in addons_path, got %s", pathStr)
	}
	if len(binds) != 1 || !strings.HasSuffix(binds[0], ":/mnt/recommended-addons/om_theme_lite:ro") {
		t.Fatalf("expected single selective bind for om_theme_lite, got %+v", binds)
	}
}
