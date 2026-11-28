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

	gitURL = strings.TrimSpace(gitURL)
	if gitURL == "" || strings.HasPrefix(gitURL, "-") {
		return nil, fmt.Errorf("invalid git repository URL")
	}
	lowerURL := strings.ToLower(gitURL)
	if !strings.HasPrefix(lowerURL, "https://") &&
		!strings.HasPrefix(lowerURL, "http://") &&
		!strings.HasPrefix(lowerURL, "ssh://") &&
		!strings.HasPrefix(lowerURL, "git@") {
		return nil, fmt.Errorf("unsupported git repository URL scheme (use https://, http://, ssh://, or git@)")
	}

	branch = strings.TrimSpace(branch)
	if strings.HasPrefix(branch, "-") {
		return nil, fmt.Errorf("invalid git branch name")
	}

	cloneURL := gitURL
	if token != "" && strings.HasPrefix(gitURL, "https://") {
		cloneURL = strings.Replace(gitURL, "https://", "https://oauth2:"+token+"@", 1)
	}

	args := []string{"clone", "--depth", "1"}
	if branch != "" {
		args = append(args, "-b", branch)
	}
	args = append(args, "--", cloneURL, destDir)

	cmd := exec.CommandContext(ctx, "git", args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		_ = os.RemoveAll(destDir)
		return nil, fmt.Errorf("git clone failed: %s", strings.TrimSpace(string(out)))
	}
	ensureWorldReadable(destDir)

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
	ensureWorldReadable(target.LocalPath)

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

	tmpDir, err := os.MkdirTemp("", "runodoo-zip-extract-*")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(tmpDir)

	if err := extractZipSafe(zipPath, tmpDir); err != nil {
		return nil, err
	}

	// If __manifest__.py is directly at the root of the zip (user zipped module contents instead of folder),
	// wrap it inside a valid Python module folder name (underscores, no hyphens) so Odoo can import it!
	targetRoot := destDir
	if _, err := os.Stat(filepath.Join(tmpDir, "__manifest__.py")); err == nil {
		pyModName := sanitizePythonModuleName(cleanName)
		targetRoot = filepath.Join(destDir, pyModName)
	}

	if err := os.MkdirAll(targetRoot, 0o755); err != nil {
		return nil, err
	}
	if err := copyDirReadable(tmpDir, targetRoot); err != nil {
		_ = os.RemoveAll(destDir)
		return nil, err
	}
	normalizeModuleFolderNames(destDir)
	ensureWorldReadable(destDir)

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

// DeleteRepo removes an addon repository from disk and state, blocking deletion if still attached to any Odoo instance.
func (m *Manager) DeleteRepo(id string) error {
	var localPath string
	err := m.store.Update(func(st *models.State) error {
		idx := -1
		var repoName string
		for i, r := range st.AddonRepos {
			if r.ID == id {
				idx = i
				repoName = r.Name
				localPath = r.LocalPath
				break
			}
		}
		if idx == -1 {
			return fmt.Errorf("addon repository not found")
		}

		// Block deletion if attached to any Odoo instance
		var attachedTo []string
		for _, inst := range st.Instances {
			for _, rid := range inst.AddonRepoIDs {
				if rid == id {
					attachedTo = append(attachedTo, inst.Name)
					break
				}
			}
		}
		if len(attachedTo) > 0 {
			return fmt.Errorf("tidak bisa menghapus addon '%s' karena masih ter-attach ke instance Odoo: [%s] — silakan detach terlebih dahulu di halaman detail instance", repoName, strings.Join(attachedTo, ", "))
		}

		st.AddonRepos = append(st.AddonRepos[:idx], st.AddonRepos[idx+1:]...)
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

// ComputeAddonsPath calculates the container-side Odoo `addons_path` and host bind mounts
// for attached AddonRepo IDs.
func (m *Manager) ComputeAddonsPath(repoIDs []string) (addonsPath string, binds []string) {
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

	absDest, err := filepath.Abs(destDir)
	if err != nil {
		return err
	}
	destPrefix := absDest + string(os.PathSeparator)
	for _, f := range r.File {
		if strings.HasPrefix(f.Name, "__MACOSX/") {
			continue
		}
		if f.Name == "" || filepath.IsAbs(f.Name) {
			return fmt.Errorf("illegal file path in zip: %s", f.Name)
		}
		cleaned := filepath.Clean(filepath.FromSlash(strings.ReplaceAll(f.Name, "\\", "/")))
		if cleaned == "." {
			continue
		}
		if filepath.IsAbs(cleaned) || cleaned == ".." || strings.HasPrefix(cleaned, ".."+string(os.PathSeparator)) {
			return fmt.Errorf("illegal file path in zip: %s", f.Name)
		}
		target, err := filepath.Abs(filepath.Join(absDest, cleaned))
		if err != nil || !strings.HasPrefix(target, destPrefix) {
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
		out, err := os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o644)
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

var (
	reValidOdooModName = regexp.MustCompile(`^[a-zA-Z_][a-zA-Z0-9_]*$`)
	reGitHubBranchSfx  = regexp.MustCompile(`(?i)-(main|master|dev|develop|staging|prod|production|v?\d+(\.\d+)*)$`)
)

// normalizeModuleFolderNames renames any extracted Odoo module directory whose name is not
// a valid Python/Odoo identifier (e.g. GitHub ZIPs like "sintaksa_branding-main" -> "sintaksa_branding").
func normalizeModuleFolderNames(rootDir string) {
	var manifestDirs []string
	_ = filepath.WalkDir(rootDir, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() && (d.Name() == ".git" || d.Name() == "__pycache__") {
			return filepath.SkipDir
		}
		if !d.IsDir() && d.Name() == "__manifest__.py" {
			manifestDirs = append(manifestDirs, filepath.Dir(p))
		}
		return nil
	})

	for _, modDir := range manifestDirs {
		// Never rename rootDir itself
		if filepath.Clean(modDir) == filepath.Clean(rootDir) {
			continue
		}
		base := filepath.Base(modDir)
		if reValidOdooModName.MatchString(base) {
			continue
		}
		cleaned := reGitHubBranchSfx.ReplaceAllString(base, "")
		cleaned = sanitizePythonModuleName(cleaned)
		if cleaned == "" || cleaned == base {
			continue
		}
		newPath := filepath.Join(filepath.Dir(modDir), cleaned)
		if _, err := os.Stat(newPath); err == nil {
			continue
		}
		_ = os.Rename(modDir, newPath)
	}
}

func sanitizePythonModuleName(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	var b strings.Builder
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '_' {
			b.WriteRune(r)
		} else {
			b.WriteRune('_')
		}
	}
	res := strings.Trim(b.String(), "_")
	for strings.Contains(res, "__") {
		res = strings.ReplaceAll(res, "__", "_")
	}
	if res == "" || (res[0] >= '0' && res[0] <= '9') {
		res = "mod_" + res
	}
	return res
}

func ensureWorldReadable(root string) {
	_ = filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			_ = os.Chmod(p, 0o755)
		} else {
			_ = os.Chmod(p, 0o644)
		}
		return nil
	})
}

func copyDirReadable(src, dst string) error {
	return filepath.WalkDir(src, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, p)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		data, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		return os.WriteFile(target, data, 0o644)
	})
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
