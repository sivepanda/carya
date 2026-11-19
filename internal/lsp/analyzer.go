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
	"carya/internal/peers"
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

	refs  *git.RefManager
	peers *peers.Checker

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
		peers:         peers.NewChecker(repoPath),
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

	locallyModified, err := a.modifiedFiles()
	if err != nil {
		return err
	}

	// Each teammate is measured against the base their state records, and
	// conflicts are predicted against your published state.
	mine, err := a.refs.GetUserState(a.userID)
	if err != nil {
		mine = git.UserRef{}
	}
	changes, err := a.peers.ForPaths(a.userID, mine, locallyModified)
	if err != nil {
		return err
	}

	reports := make(map[string]*FileReport)
	for _, ch := range changes {
		report := reports[ch.Path]
		if report == nil {
			report = &FileReport{}
			reports[ch.Path] = report
		}
		report.Teammates = append(report.Teammates, TeammateDiff{
			UserID:     ch.UserID,
			Patch:      ch.Patch,
			IsConflict: ch.Conflict,
		})
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

// modifiedFiles lists the files with staged or unstaged changes against HEAD.
func (a *Analyzer) modifiedFiles() ([]string, error) {
	cmd := exec.Command("git", "diff", "--name-only", "-z", "HEAD")
	cmd.Dir = a.repoPath
	out, err := cmd.Output()
	if err != nil {
		return nil, err
	}
	var files []string
	for _, name := range strings.Split(string(out), "\x00") {
		if name != "" {
			files = append(files, name)
		}
	}
	return files, nil
}
