package draft

import (
	"fmt"
	"os"
	"path/filepath"
)

// DataDirEnv is the environment variable naming the private data root.
const DataDirEnv = "DRAFTROOM_DATA_DIR"

// DefaultDataDir is where the private data repo lives relative to a
// checkout of the public one.
const DefaultDataDir = "../fantasy_sports_data"

// DataRoot is the private directory holding proprietary source data.
//
// The public repo carries the extractors, the league's own rulings, and the
// name aliases; it does not carry vendor data. Ciely's projections, the
// FantasyPoints and Athletic articles, and the Subvertadown exports are
// subscriber content, and redistributing them through a public GitHub repo
// is not ours to do. They live in a separate private repo that this type
// locates.
type DataRoot string

// Raw returns the path to a source's immutable export directory.
func (d DataRoot) Raw(parts ...string) string {
	return filepath.Join(append([]string{string(d), "raw"}, parts...)...)
}

// Normalized returns the path to a generated CSV.
func (d DataRoot) Normalized(name string) string {
	return filepath.Join(string(d), "normalized", name)
}

// Manifest returns the path to the provenance manifest.
func (d DataRoot) Manifest() string {
	return filepath.Join(string(d), "manifest.json")
}

// ConfigDirEnv names the directory holding the hand-maintained files that
// ship with this repo: keeper rulings, name aliases, and personal leans.
const ConfigDirEnv = "DRAFTROOM_CONFIG_DIR"

// configDirFromRoot is where those files live inside a checkout.
const configDirFromRoot = "league_home/app/internal/draft/data"

// ResolveConfigDir finds the tracked config directory.
//
// Resolved against the repo root rather than the working directory so the
// binary works from anywhere — installed on PATH, run from a draft-night
// directory, or invoked by a launch agent. Reading "internal/draft/data"
// relative to the cwd silently produced an empty rulings file whenever the
// command ran from outside the module.
func ResolveConfigDir(flagValue string) (string, error) {
	for _, c := range []dataDirCandidate{
		{flagValue, "-config flag"},
		{os.Getenv(ConfigDirEnv), ConfigDirEnv},
	} {
		if c.path == "" {
			continue
		}
		abs, err := filepath.Abs(c.path)
		if err != nil {
			return "", fmt.Errorf("draft: resolving config dir from %s: %w", c.from, err)
		}
		if info, err := os.Stat(abs); err == nil && info.IsDir() {
			return abs, nil
		}
		return "", fmt.Errorf("draft: config dir from %s does not exist: %s", c.from, abs)
	}

	if root, ok := repoRoot(); ok {
		candidate := filepath.Join(root, configDirFromRoot)
		if info, err := os.Stat(candidate); err == nil && info.IsDir() {
			return candidate, nil
		}
	}
	// Last resort: a checkout-relative path, which works when run from
	// league_home/app itself.
	abs, err := filepath.Abs("internal/draft/data")
	if err != nil {
		return "", err
	}
	if info, err := os.Stat(abs); err == nil && info.IsDir() {
		return abs, nil
	}
	return "", fmt.Errorf(
		"draft: cannot find the config directory (rulings, aliases, my-guys)\n"+
			"Expected %s inside the repo, or set %s", configDirFromRoot, ConfigDirEnv)
}

type dataDirCandidate struct{ path, from string }

// absolute resolves a candidate to an absolute path.
//
// The default location is deliberately resolved relative to the repository
// root rather than the working directory. Commands and tests run from
// several directories inside the module, and anchoring "../" to whatever
// happens to be the cwd would point at a different place each time.
func (c dataDirCandidate) absolute() (string, error) {
	if c.from == "default location" {
		return defaultDataDirPath(), nil
	}
	return filepath.Abs(c.path)
}

// defaultDataDirPath returns the conventional sibling of the repo root,
// falling back to a cwd-relative path when no repo root can be found.
func defaultDataDirPath() string {
	if root, ok := repoRoot(); ok {
		return filepath.Join(filepath.Dir(root), filepath.Base(DefaultDataDir))
	}
	abs, _ := filepath.Abs(DefaultDataDir)
	return abs
}

// repoRoot walks up from the working directory looking for the checkout's
// .git, so the data directory can be found from anywhere in the module.
func repoRoot() (string, bool) {
	dir, err := os.Getwd()
	if err != nil {
		return "", false
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, ".git")); err == nil {
			return dir, true
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", false
		}
		dir = parent
	}
}

// ResolveDataRoot finds the private data directory, preferring an explicit
// flag, then the environment, then the conventional sibling location.
//
// A missing directory is a hard error with the exact commands to fix it,
// rather than a silent fallback to an empty board — a draft board that
// quietly loses a source is worse than one that refuses to start.
func ResolveDataRoot(flagValue string) (DataRoot, error) {
	candidates := []dataDirCandidate{
		{flagValue, "-data flag"},
		{os.Getenv(DataDirEnv), DataDirEnv},
		{DefaultDataDir, "default location"},
	}

	for _, c := range candidates {
		if c.path == "" {
			continue
		}
		abs, err := c.absolute()
		if err != nil {
			return "", fmt.Errorf("draft: resolving data dir from %s: %w", c.from, err)
		}
		info, err := os.Stat(abs)
		if err == nil && info.IsDir() {
			return DataRoot(abs), nil
		}
		// An explicitly requested path that isn't there is an error, not
		// a reason to silently try the next candidate.
		if c.from != "default location" {
			return "", fmt.Errorf("draft: data dir from %s does not exist: %s", c.from, abs)
		}
	}

	abs := defaultDataDirPath()
	return "", fmt.Errorf(`draft: no private data directory found

Proprietary source data lives outside this public repo. Expected it at:
  %s

Create it with:
  mkdir -p %s && cd %s && git init

Or point at an existing one:
  export %s=/path/to/fantasy_sports_data`, abs, abs, abs, DataDirEnv)
}
