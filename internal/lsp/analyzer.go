package lsp

import (
	"fmt"
	"log"
	"os/exec"
	"strings"
	"sync"
	"time"

	"carya/internal/git"
	"carya/internal/identity"
)

type TeammateDiff struct {
	UserID     string
	Patch      string
	IsConflict bool
}

type FileReport struct {
	Teammates []TeammateDiff
}

type Analyzer struct {
	repoPath  string
	caryaPath string
	userID    string

	refs      *git.RefManager
	conflicts *git.ConflictPredictor

	mu      sync.RWMutex
	reports map[string]*FileReport

	lastFetch     time.Time
	fetchInterval time.Duration
}

func NewAnalyzer(repoPath, caryaPath string) (*Analyzer, error) {
	uid, err := identity.NewUserIdentity(caryaPath).Get()
	if err != nil {
		log.Printf("User identity not found: %v", err)
		return nil, fmt.Errorf("user identity not found: %w", err)
	}

	a := &Analyzer{
		repoPath:      repoPath,
		caryaPath:     caryaPath,
		userID:        uid,
		refs:          git.NewRefManager(repoPath),
		conflicts:     git.NewConflictPredictor(repoPath),
		reports:       make(map[string]*FileReport),
		fetchInterval: 2 * time.Minute,
	}

	if err := a.refs.FetchCaryaRefs("origin"); err != nil {
		log.Printf("fetch team refs on startup: %v", err)
	}
	a.lastFetch = time.Now()

	return a, nil
}

func (a *Analyzer) Refresh() error {
	if time.Since(a.lastFetch) >= a.fetchInterval {
		if err := a.refs.FetchCaryaRefs("origin"); err != nil {
			log.Printf("fetch team refs: %v", err)
		}
		a.lastFetch = time.Now()
	}

	baseTree, err := a.refs.GetHEADTreeHash()
	if err != nil {
		return err
	}

	locallyModified, err := a.modifiedFiles()
	if err != nil {
		return err
	}

	localSet := make(map[string]bool, len(locallyModified))
	for _, f := range locallyModified {
		localSet[f] = true
	}

	myTree, _ := a.refs.GetUserTreeRef(a.userID)

	teammates, err := a.refs.ListUserRefs()
	if err != nil {
		return err
	}

	reports := make(map[string]*FileReport)

	for _, teammate := range teammates {
		if teammate.UserID == a.userID {
			continue
		}

		theirChanges, err := a.conflicts.DiffTrees(baseTree, teammate.TreeHash)
		if err != nil {
			continue
		}

		conflictedFiles := a.predictConflictedFiles(baseTree, myTree, teammate.TreeHash)

		for _, change := range theirChanges {
			if !localSet[change.Path] {
				continue
			}

			patch, _ := a.filePatch(baseTree, teammate.TreeHash, change.Path)

			report := reports[change.Path]
			if report == nil {
				report = &FileReport{}
				reports[change.Path] = report
			}

			report.Teammates = append(report.Teammates, TeammateDiff{
				UserID:     teammate.UserID,
				Patch:      patch,
				IsConflict: conflictedFiles[change.Path],
			})
		}
	}

	a.mu.Lock()
	a.reports = reports
	a.mu.Unlock()

	return nil
}

func (a *Analyzer) ReportForFile(relativePath string) *FileReport {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.reports[relativePath]
}

func (a *Analyzer) predictConflictedFiles(baseTree, myTree, theirTree string) map[string]bool {
	if myTree == "" {
		return nil
	}

	report, err := a.conflicts.PredictConflicts(baseTree, myTree, theirTree)
	if err != nil || !report.HasConflicts {
		return nil
	}

	files := make(map[string]bool, len(report.ConflictedFiles))
	for _, cf := range report.ConflictedFiles {
		files[cf.Path] = true
	}
	return files
}

func (a *Analyzer) modifiedFiles() ([]string, error) {
	unstaged := exec.Command("git", "diff", "--name-only", "HEAD")
	unstaged.Dir = a.repoPath
	unstagedOutput, err := unstaged.Output()
	if err != nil {
		return nil, err
	}

	staged := exec.Command("git", "diff", "--name-only", "--staged", "HEAD")
	staged.Dir = a.repoPath
	stagedOutput, _ := staged.Output()

	seen := make(map[string]bool)
	var files []string

	for _, raw := range [][]byte{unstagedOutput, stagedOutput} {
		for _, name := range strings.Split(strings.TrimSpace(string(raw)), "\n") {
			name = strings.TrimSpace(name)
			if name != "" && !seen[name] {
				seen[name] = true
				files = append(files, name)
			}
		}
	}

	return files, nil
}

func (a *Analyzer) filePatch(baseTree, theirTree, path string) (string, error) {
	cmd := exec.Command("git", "diff-tree", "-p", baseTree, theirTree, "--", path)
	cmd.Dir = a.repoPath
	output, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return string(output), nil
}
