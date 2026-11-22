package addons

import (
	"archive/zip"
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/runodoo/runodoo/internal/config"
	"github.com/runodoo/runodoo/internal/models"
	"github.com/runodoo/runodoo/internal/store"
)

type Manager struct {
	cfg   *config.Config
	store *store.Store
}

func NewManager(cfg *config.Config, st *store.Store) *Manager {
	return &Manager{cfg: cfg, store: st}
}

// CloneGitRepo clones an addons Git repository into the managed addons directory and scans modules.
func (m *Manager) CloneGitRepo(ctx context.Context, name, gitURL, branch, token string) (*models.AddonRepo, error) {
	cleanName := sanitizeName(name)
	if cleanName == "" {
		return nil, fmt.Errorf("invalid addon repository name")
	}
	destDir := filepath.Join(m.cfg.AddonsDir(), cleanName)
	if _, err := os.Stat(destDir); err == nil {
		return nil, fmt.Errorf("addon repository '%s' already exists", cleanName)
	}

	cloneURL := gitURL
	if token != "" && strings.HasPrefix(gitURL, "https://") {
		cloneURL = strings.Replace(gitURL, "https://", "https://oauth2:"+token+"@", 1)
	}

	args := []string{"clone", "--depth", "1"}
	if branch != "" {
		args = append(args, "-b", branch)
	}
	args = append(args, cloneURL, destDir)

	cmd := exec.CommandContext(ctx, "git", args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		_ = os.RemoveAll(destDir)
		return nil, fmt.Errorf("git clone failed: %s", strings.TrimSpace(string(out)))
	}

	modules := ScanModules(destDir)
	now := time.Now()
	repo := models.AddonRepo{
		ID:         randomID(),
		Name:       cleanName,
		SourceType: "git",
		GitURL:     gitURL,
		GitBranch:  branch,
		LocalPath:  destDir,
		Modules:    modules,
		CreatedAt:  now,
		UpdatedAt:  now,
	}

	if err := m.store.Update(func(st *models.State) error {
		st.AddonRepos = append(st.AddonRepos, repo)
		return nil
	}); err != nil {
		return nil, err
	}
	return &repo, nil
}

// PullGitRepo runs `git pull` on an existing git addon repository and re-scans modules.
func (m *Manager) PullGitRepo(ctx context.Context, id string) (*models.AddonRepo, error) {
	st := m.store.Get()
	var target *models.AddonRepo
	for i := range st.AddonRepos {
		if st.AddonRepos[i].ID == id {
			target = &st.AddonRepos[i]
			break
		}
	}
	if target == nil {
		return nil, fmt.Errorf("addon repository not found")
	}
	if target.SourceType != "git" {
		return nil, fmt.Errorf("addon repository '%s' is not a git repository", target.Name)
	}

	cmd := exec.CommandContext(ctx, "git", "-C", target.LocalPath, "pull", "--ff-only")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("git pull failed: %s", strings.TrimSpace(string(out)))
	}

	modules := ScanModules(target.LocalPath)
	now := time.Now()
	var updated models.AddonRepo
	err = m.store.Update(func(st *models.State) error {
		for i := range st.AddonRepos {
			if st.AddonRepos[i].ID == id {
				st.AddonRepos[i].Modules = modules
				st.AddonRepos[i].UpdatedAt = now
				updated = st.AddonRepos[i]
				return nil
			}
		}
		return fmt.Errorf("addon repository not found")
	})
	if err != nil {
		return nil, err
	}
	return &updated, nil
}

// UploadZip extracts an uploaded .zip archive into the managed addons directory and scans modules.
func (m *Manager) UploadZip(name string, zipPath string) (*models.AddonRepo, error) {
	cleanName := sanitizeName(name)
	if cleanName == "" {
		return nil, fmt.Errorf("invalid addon name")
	}
	destDir := filepath.Join(m.cfg.AddonsDir(), cleanName)
	if _, err := os.Stat(destDir); err == nil {
		return nil, fmt.Errorf("addon repository '%s' already exists", cleanName)
	}
	if err := os.MkdirAll(destDir, 0o755); err != nil {
		return nil, err
	}

	if err := extractZipSafe(zipPath, destDir); err != nil {
		_ = os.RemoveAll(destDir)
		return nil, err
	}

	modules := ScanModules(destDir)
	now := time.Now()
	repo := models.AddonRepo{
		ID:         randomID(),
		Name:       cleanName,
		SourceType: "zip",
		LocalPath:  destDir,
		Modules:    modules,
		CreatedAt:  now,
		UpdatedAt:  now,
	}
	if err := m.store.Update(func(st *models.State) error {
		st.AddonRepos = append(st.AddonRepos, repo)
		return nil
	}); err != nil {
		return nil, err
	}
	return &repo, nil
}

// RescanRepo refreshes the discovered __manifest__.py modules for an addon repo.
func (m *Manager) RescanRepo(id string) (*models.AddonRepo, error) {
	var updated models.AddonRepo
	err := m.store.Update(func(st *models.State) error {
		for i := range st.AddonRepos {
			if st.AddonRepos[i].ID == id {
				st.AddonRepos[i].Modules = ScanModules(st.AddonRepos[i].LocalPath)
				st.AddonRepos[i].UpdatedAt = time.Now()
				updated = st.AddonRepos[i]
				return nil
			}
		}
		return fmt.Errorf("addon repository not found")
	})
	if err != nil {
		return nil, err
	}
	return &updated, nil
}

// DeleteRepo removes an addon repository from disk and state.
func (m *Manager) DeleteRepo(id string) error {
	var localPath string
	err := m.store.Update(func(st *models.State) error {
		idx := -1
		for i, r := range st.AddonRepos {
			if r.ID == id {
				idx = i
				localPath = r.LocalPath
				break
			}
		}
		if idx == -1 {
			return fmt.Errorf("addon repository not found")
		}
		st.AddonRepos = append(st.AddonRepos[:idx], st.AddonRepos[idx+1:]...)
		// Detach from any instances referencing this repo
		for i := range st.Instances {
			filtered := make([]string, 0, len(st.Instances[i].AddonRepoIDs))
			for _, rid := range st.Instances[i].AddonRepoIDs {
				if rid != id {
					filtered = append(filtered, rid)
				}
			}
			st.Instances[i].AddonRepoIDs = filtered
		}
		return nil
	})
	if err != nil {
		return err
	}
	if localPath != "" && strings.HasPrefix(localPath, m.cfg.AddonsDir()) {
		_ = os.RemoveAll(localPath)
	}
	return nil
}

// ListRecommendedAddons scans the built-in recommended addons directory (e.g. repo's addons/)
// and returns all discovered Odoo modules that users can selectively install per instance.
func (m *Manager) ListRecommendedAddons() []models.AddonModule {
	mods := ScanModules(m.cfg.RecommendedAddonsDir)
	if mods == nil {
		return []models.AddonModule{}
	}
	return mods
}

// ComputeAddonsPath calculates the container-side Odoo `addons_path` and host bind mounts
// for attached AddonRepo IDs AND selectively chosen recommended addons.
func (m *Manager) ComputeAddonsPath(repoIDs []string, recommendedSelected []string) (addonsPath string, binds []string) {
	st := m.store.Get()
	repoMap := make(map[string]models.AddonRepo, len(st.AddonRepos))
	for _, r := range st.AddonRepos {
		repoMap[r.ID] = r
	}

	pathsSet := map[string]bool{
		"/usr/lib/python3/dist-packages/odoo/addons": true,
	}

	for _, id := range repoIDs {
		repo, ok := repoMap[id]
		if !ok {
			continue
		}
		containerRepoRoot := "/mnt/extra-addons/" + repo.Name
		hostRepoPath := filepath.Join(m.cfg.HostAddonsDir(), repo.Name)
		binds = append(binds, fmt.Sprintf("%s:%s:ro", hostRepoPath, containerRepoRoot))

		// Scan __manifest__.py parent directories
		_ = filepath.WalkDir(repo.LocalPath, func(p string, d os.DirEntry, err error) error {
			if err != nil {
				return nil
			}
			if d.IsDir() && (d.Name() == ".git" || d.Name() == "__pycache__") {
				return filepath.SkipDir
			}
			if !d.IsDir() && d.Name() == "__manifest__.py" {
				modDir := filepath.Dir(p)
				parentDir := filepath.Dir(modDir)
				rel, err := filepath.Rel(repo.LocalPath, parentDir)
				if err == nil {
					if rel == ".." {
						// Single-module repo: __manifest__.py is directly inside repo root
						pathsSet["/mnt/extra-addons"] = true
					} else if rel == "." || rel == "" {
						pathsSet[containerRepoRoot] = true
					} else {
						pathsSet[containerRepoRoot+"/"+filepath.ToSlash(rel)] = true
					}
				}
			}
			return nil
		})
	}

	// Mount ONLY the specifically selected recommended addons into /mnt/recommended-addons/<technicalName>
	if len(recommendedSelected) > 0 {
		selectedSet := make(map[string]bool, len(recommendedSelected))
		for _, sel := range recommendedSelected {
			selectedSet[strings.TrimSpace(sel)] = true
		}
		available := m.ListRecommendedAddons()
		mountedAny := false
		for _, mod := range available {
			if selectedSet[mod.TechnicalName] || selectedSet[mod.RelPath] {
				hostModPath := filepath.Join(m.cfg.HostRecommendedAddonsDir, filepath.FromSlash(mod.RelPath))
				containerModPath := "/mnt/recommended-addons/" + mod.TechnicalName
				binds = append(binds, fmt.Sprintf("%s:%s:ro", hostModPath, containerModPath))
				mountedAny = true
			}
		}
		if mountedAny {
			pathsSet["/mnt/recommended-addons"] = true
		}
	}

	ordered := make([]string, 0, len(pathsSet))
	for p := range pathsSet {
		ordered = append(ordered, p)
	}
	sort.Strings(ordered)
	return strings.Join(ordered, ","), binds
}

// ScanModules walks a directory and extracts metadata from every __manifest__.py found.
func ScanModules(rootDir string) []models.AddonModule {
	modules := []models.AddonModule{}
	_ = filepath.WalkDir(rootDir, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() && (d.Name() == ".git" || d.Name() == "__pycache__") {
			return filepath.SkipDir
		}
		if !d.IsDir() && d.Name() == "__manifest__.py" {
			modDir := filepath.Dir(p)
			techName := filepath.Base(modDir)
			rel, _ := filepath.Rel(rootDir, modDir)
			raw, err := os.ReadFile(p)
			if err != nil {
				return nil
			}
			mod := parseManifest(techName, filepath.ToSlash(rel), string(raw))
			modules = append(modules, mod)
		}
		return nil
	})
	sort.Slice(modules, func(i, j int) bool {
		return modules[i].TechnicalName < modules[j].TechnicalName
	})
	return modules
}

var (
	reName    = regexp.MustCompile(`['"]name['"]\s*:\s*['"]([^'"]+)['"]`)
	reVersion = regexp.MustCompile(`['"]version['"]\s*:\s*['"]([^'"]+)['"]`)
	reSummary = regexp.MustCompile(`['"]summary['"]\s*:\s*['"]([^'"]+)['"]`)
	reAuthor  = regexp.MustCompile(`['"]author['"]\s*:\s*['"]([^'"]+)['"]`)
	reDepends = regexp.MustCompile(`(?s)['"]depends['"]\s*:\s*\[([^\]]*)\]`)
	reQuoted  = regexp.MustCompile(`['"]([^'"]+)['"]`)
)

func parseManifest(techName, relPath, content string) models.AddonModule {
	mod := models.AddonModule{
		TechnicalName: techName,
		Name:          techName,
		RelPath:       relPath,
		Depends:       []string{},
	}
	if m := reName.FindStringSubmatch(content); len(m) > 1 {
		mod.Name = m[1]
	}
	if m := reVersion.FindStringSubmatch(content); len(m) > 1 {
		mod.Version = m[1]
	}
	if m := reSummary.FindStringSubmatch(content); len(m) > 1 {
		mod.Summary = m[1]
	}
	if m := reAuthor.FindStringSubmatch(content); len(m) > 1 {
		mod.Author = m[1]
	}
	if m := reDepends.FindStringSubmatch(content); len(m) > 1 {
		for _, dm := range reQuoted.FindAllStringSubmatch(m[1], -1) {
			if len(dm) > 1 {
				mod.Depends = append(mod.Depends, dm[1])
			}
		}
	}
	return mod
}

func extractZipSafe(zipPath, destDir string) error {
	r, err := zip.OpenReader(zipPath)
	if err != nil {
		return err
	}
	defer r.Close()

	cleanDest := filepath.Clean(destDir) + string(os.PathSeparator)
	for _, f := range r.File {
		if strings.HasPrefix(f.Name, "__MACOSX/") {
			continue
		}
		target := filepath.Join(destDir, f.Name)
		if !strings.HasPrefix(filepath.Clean(target)+string(os.PathSeparator), cleanDest) && filepath.Clean(target) != filepath.Clean(destDir) {
			return fmt.Errorf("illegal file path in zip: %s", f.Name)
		}
		if f.FileInfo().IsDir() {
			if err := os.MkdirAll(target, 0o755); err != nil {
				return err
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		rc, err := f.Open()
		if err != nil {
			return err
		}
		out, err := os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, f.Mode())
		if err != nil {
			rc.Close()
			return err
		}
		_, err = io.Copy(out, rc)
		rc.Close()
		out.Close()
		if err != nil {
			return err
		}
	}
	return nil
}

func sanitizeName(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	var b strings.Builder
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' || r == '_' {
			b.WriteRune(r)
		} else if r == ' ' || r == '.' {
			b.WriteRune('-')
		}
	}
	return strings.Trim(b.String(), "-_")
}

func randomID() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
