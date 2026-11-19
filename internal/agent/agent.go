// Package agent runs Carya for coding agents: no TUI, no daemon. Harness
// hooks call into it to set a checkout up, publish what the agent has on
// disk, and ask whether peers are changing the files it is about to edit.
package agent

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"carya/internal/config"
	"carya/internal/git"
	"carya/internal/identity"
	"carya/internal/peers"
	"carya/internal/repository"
)

// Session is Carya set up in one checkout, under one agent identity.
type Session struct {
	Repo   *repository.Repository
	UserID string
}

// Bootstrap prepares the checkout containing dir for an agent and returns its
// session. It is idempotent and cheap enough to run from every hook: it
// creates .carya (ignored through info/exclude, so no tracked file changes),
// the checkout's identity named after harness, default team settings, and
// the shadow repository other Carya commands expect.
func Bootstrap(dir, harness string) (*Session, error) {
	repo, err := repository.NewAt(dir)
	if err != nil {
		return nil, err
	}
	if err := repo.EnsureExists(); err != nil {
		return nil, err
	}
	if err := repo.EnsureLocallyIgnored(); err != nil {
		return nil, err
	}

	userID, err := identity.NewUserIdentity(repo.CaryaPath()).GetOrCreateAgent(harness)
	if err != nil {
		return nil, err
	}

	if _, err := os.Stat(filepath.Join(repo.CaryaPath(), "team.json")); os.IsNotExist(err) {
		// A linked worktree follows the main checkout's settings, so turning
		// sharing off there also covers every agent worktree made from it.
		cfg := config.TeamConfig{AutoPublish: true, AutoFetch: true, AutoPush: true}
		if main := repo.MainWorktreeRoot(); main != repo.RootPath() {
			if _, err := os.Stat(filepath.Join(main, ".carya", "team.json")); err == nil {
				cfg = config.LoadTeamConfig(filepath.Join(main, ".carya"))
			}
		}
		if err := config.SaveTeamConfig(repo.CaryaPath(), cfg); err != nil {
			return nil, err
		}
	}

	if _, err := os.Stat(repo.ShadowPath()); os.IsNotExist(err) {
		// Seed only on creation: reseeding would discard a running daemon's
		// shadow index.
		shadow := git.NewShadowRepo(repo.CaryaPath(), repo.RootPath())
		if err := shadow.Initialize(); err != nil {
			return nil, err
		}
		_ = shadow.SeedIndexFromMainHEAD()
	}

	return &Session{Repo: repo, UserID: userID}, nil
}

// Open returns the session for an already set-up checkout without creating
// anything.
func Open(dir string) (*Session, error) {
	repo, err := repository.NewAt(dir)
	if err != nil {
		return nil, err
	}
	if !repo.Exists() {
		return nil, fmt.Errorf("carya is not set up in %s", repo.RootPath())
	}
	userID, err := identity.NewUserIdentity(repo.CaryaPath()).Get()
	if err != nil {
		return nil, err
	}
	return &Session{Repo: repo, UserID: userID}, nil
}

// Snapshot publishes the checkout's working tree, including edits made by
// shell commands, as this agent's state. Only the local ref changes; Sync
// shares it.
func (s *Session) Snapshot() (git.UserRef, error) {
	tree, err := git.WorkingStateTree(s.Repo.RootPath())
	if err != nil {
		return git.UserRef{}, err
	}
	return git.NewRefManager(s.Repo.RootPath()).PublishUserState(s.UserID, tree)
}

// Check publishes the current state, then reports peers' unfinished changes
// to paths. Paths may be absolute or relative to the checkout root; paths
// outside the checkout are ignored.
func (s *Session) Check(paths []string) ([]peers.Change, error) {
	rel := s.RelPaths(paths)
	if len(rel) == 0 {
		return nil, nil
	}
	mine, err := s.Snapshot()
	if err != nil {
		mine = git.UserRef{}
	}
	return peers.NewChecker(s.Repo.RootPath()).ForPaths(s.UserID, mine, rel)
}

// RelPaths converts paths to checkout-relative slash paths, dropping any
// outside the checkout.
func (s *Session) RelPaths(paths []string) []string {
	root := evalOrSelf(s.Repo.RootPath())
	var rel []string
	for _, p := range paths {
		if p == "" {
			continue
		}
		if !filepath.IsAbs(p) {
			p = filepath.Join(root, p)
		}
		r, err := filepath.Rel(root, evalOrSelf(p))
		if err != nil || r == "." || r == ".." || strings.HasPrefix(r, ".."+string(filepath.Separator)) {
			continue
		}
		rel = append(rel, filepath.ToSlash(r))
	}
	return rel
}

// evalOrSelf resolves symlinks in p, or in its nearest existing parent for a
// file that does not exist yet (such as one a Write is about to create).
func evalOrSelf(p string) string {
	if resolved, err := filepath.EvalSymlinks(p); err == nil {
		return resolved
	}
	dir, base := filepath.Split(filepath.Clean(p))
	if dir == "" || dir == p {
		return p
	}
	return filepath.Join(evalOrSelf(filepath.Clean(dir)), base)
}

// Sharing reports whether states are pushed and fetched from this checkout,
// and why not when they are not.
func (s *Session) Sharing() (bool, string) {
	if os.Getenv("CLAUDE_CODE_REMOTE") == "true" {
		return false, "cloud sessions can only push branches, so Carya does not share state from them"
	}
	cfg := config.LoadTeamConfig(s.Repo.CaryaPath())
	if !cfg.AutoPush && !cfg.AutoFetch {
		return false, "pushing and fetching are turned off in .carya/team.json"
	}
	if !git.NewRefManager(s.Repo.RootPath()).HasRemote("origin") {
		return false, "there is no origin remote"
	}
	return true, ""
}

// ClaimSync reports whether a push/fetch is due (none in the last interval)
// and, if so, records that one is starting, so hooks firing in quick
// succession start at most one.
func (s *Session) ClaimSync(interval time.Duration) bool {
	if ok, _ := s.Sharing(); !ok {
		return false
	}
	stamp := filepath.Join(s.Repo.CaryaPath(), "last-state-sync")
	if info, err := os.Stat(stamp); err == nil && time.Since(info.ModTime()) < interval {
		return false
	}
	return os.WriteFile(stamp, []byte(time.Now().Format(time.RFC3339)+"\n"), 0644) == nil
}

// FetchStates fetches everyone's published states from origin without
// pushing anything.
func (s *Session) FetchStates() error {
	if ok, why := s.Sharing(); !ok {
		return fmt.Errorf("not sharing state: %s", why)
	}
	return git.NewRefManager(s.Repo.RootPath()).FetchCaryaRefs("origin")
}

// Sync pushes this agent's state and fetches everyone else's.
func (s *Session) Sync() error {
	if ok, why := s.Sharing(); !ok {
		return fmt.Errorf("not sharing state: %s", why)
	}
	cfg := config.LoadTeamConfig(s.Repo.CaryaPath())
	refs := git.NewRefManager(s.Repo.RootPath())
	var errs []string
	if cfg.AutoPush {
		if _, err := s.Snapshot(); err != nil {
			errs = append(errs, err.Error())
		} else if err := refs.PushUserRef("origin", s.UserID); err != nil {
			errs = append(errs, err.Error())
		}
	}
	if cfg.AutoFetch {
		if err := refs.FetchCaryaRefs("origin"); err != nil {
			errs = append(errs, err.Error())
		}
	}
	if len(errs) > 0 {
		return fmt.Errorf("%s", strings.Join(errs, "; "))
	}
	return nil
}
