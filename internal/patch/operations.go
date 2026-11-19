package patch

import (
	"bytes"
	"carya/internal/chunk"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

type Result struct {
	Patch    string
	Warnings []string
}

// CreateFromChunks joins the selected chunks' diffs into one patch, in the
// order the chunks were recorded: a file's later chunks are diffed on top of
// its earlier ones, and git apply takes a file's diffs in sequence.
func CreateFromChunks(chunks []chunk.Chunk, selectedIndices map[int]bool) Result {
	var patches []string
	var warnings []string

	var order []int
	for i := range chunks {
		if selectedIndices[i] {
			order = append(order, i)
		}
	}
	sort.SliceStable(order, func(a, b int) bool {
		return chunk.RecordedBefore(chunks[order[a]], chunks[order[b]])
	})

	for _, i := range order {
		c := chunks[i]
		cleanDiff, diffWarnings := CleanupDiffForGit(c)
		if len(diffWarnings) > 0 {
			for _, w := range diffWarnings {
				warnings = append(warnings, fmt.Sprintf("%s: %s", c.FilePath, w))
			}
		}
		if cleanDiff != "" {
			patches = append(patches, cleanDiff)
		}
	}

	return Result{
		Patch:    strings.Join(patches, ""),
		Warnings: warnings,
	}
}

func CleanupDiffForGit(c chunk.Chunk) (string, []string) {
	diff := c.Diff
	var warnings []string

	if strings.HasPrefix(diff, "Binary file ") {
		return "", nil
	}

	if !strings.HasSuffix(diff, "\n") {
		diff = diff + "\n"
	}

	if strings.Contains(diff, "\x00") {
		warning := "Diff contains null bytes (file may be binary)"
		warnings = append(warnings, warning)
		return "", warnings
	}

	diff = strings.ReplaceAll(diff, "\r\n", "\n")

	return diff, warnings
}

func CheckApply(repoPath, patch string) error {
	return withIndexAt(repoPath, headCommit(repoPath), func(env []string) error {
		return runApply(repoPath, env, []string{"--check", "--cached", "-"}, patch)
	})
}

// runApply applies patch with git apply exactly as it is: apply.whitespace=fix
// would otherwise change what is committed, and =error refuse it.
func runApply(repoPath string, env, args []string, patch string) error {
	applyArgs := append([]string{"apply", "--whitespace=nowarn"}, args...)
	applyCmd := exec.Command("git", applyArgs...)
	applyCmd.Dir = repoPath
	applyCmd.Env = env
	applyCmd.Stdin = strings.NewReader(patch)

	if output, err := applyCmd.CombinedOutput(); err != nil {
		log.Printf("Failed to apply patch: %v, output: %s", err, output)
		return fmt.Errorf("failed to apply patch: %w\n%s", err, output)
	}

	return nil
}

// ApplyAndCommit commits HEAD (or nothing, on an unborn branch) with patch
// applied, with message. It builds the commit in a temporary index, so other
// changes, staged or not, stay as they are, and then brings the index up to
// date for the paths it committed (see updateIndex). It runs `git commit`,
// so the repository's commit hooks and the user's identity apply. It returns
// git commit's output.
func ApplyAndCommit(repoPath, patch, message string) (string, error) {
	if err := CheckNoOperation(repoPath); err != nil {
		return "", err
	}
	oldHead := headCommit(repoPath)
	var result string
	err := withIndexAt(repoPath, oldHead, func(env []string) error {
		if err := runApply(repoPath, env, []string{"--cached", "-"}, patch); err != nil {
			return err
		}
		commitCmd := exec.Command("git", "commit", "-m", message)
		commitCmd.Dir = repoPath
		commitCmd.Env = env
		output, err := commitCmd.CombinedOutput()
		result = string(output)
		if err != nil {
			log.Printf("Failed to create commit: %v, output: %s", err, output)
			return fmt.Errorf("failed to create commit: %w\n%s", err, output)
		}
		return nil
	})
	if err != nil {
		return result, err
	}
	if err := updateIndex(repoPath, oldHead); err != nil {
		return result, fmt.Errorf("committed, but could not update the index to match: %w", err)
	}
	return result, nil
}

// CheckNoOperation refuses to commit while git is in the middle of a merge,
// cherry-pick or revert: the commit would become that operation's, recording
// its other parent or taking its author, with only some of its changes.
func CheckNoOperation(repoPath string) error {
	cmd := exec.Command("git", "rev-parse", "--git-path", "MERGE_HEAD", "--git-path", "CHERRY_PICK_HEAD", "--git-path", "REVERT_HEAD")
	cmd.Dir = repoPath
	out, err := cmd.Output()
	if err != nil {
		return fmt.Errorf("find git's operation state: %w", err)
	}
	paths := strings.Split(strings.TrimSpace(string(out)), "\n")
	for i, op := range []string{"merge", "cherry-pick", "revert"} {
		if i >= len(paths) {
			break
		}
		p := paths[i]
		if !filepath.IsAbs(p) {
			p = filepath.Join(repoPath, p)
		}
		if _, err := os.Stat(p); err == nil {
			return fmt.Errorf("a %s is in progress; finish or abort it before committing", op)
		}
	}
	return nil
}

// entry is a path's mode and object in a tree or the index; an absent path
// has mode "000000".
type entry struct{ mode, oid string }

func (e entry) regular() bool { return e.mode == "100644" || e.mode == "100755" }

// updateIndex brings the checkout's index up to date with the commit just
// made on top of oldHead ("" for none) from a temporary index, for the paths
// it changed. A path the user had nothing staged for gets the committed
// version. A path they had staged keeps their version with the committed
// change merged in, or as it is when the two overlap, so the index never
// shows the commit being undone. It reads and writes index entries directly:
// paths are never pathspecs, so a file named "*.go" stands for itself.
func updateIndex(repoPath, oldHead string) error {
	base := oldHead
	if base == "" {
		var err error
		if base, err = emptyTree(repoPath); err != nil {
			return err
		}
	}
	raw, err := runGit(repoPath, nil, "", "diff-tree", "-r", "-z", "--no-renames", "--no-abbrev", base, "HEAD")
	if err != nil {
		return fmt.Errorf("list committed paths: %w", err)
	}
	staged, err := runGit(repoPath, nil, "", "ls-files", "--stage", "-z")
	if err != nil {
		return fmt.Errorf("read index: %w", err)
	}
	index := map[string]entry{}
	conflicted := map[string]bool{}
	for _, rec := range strings.Split(staged, "\x00") {
		meta, p, ok := strings.Cut(rec, "\t")
		f := strings.Fields(meta)
		if !ok || len(f) != 3 {
			continue
		}
		if f[2] != "0" {
			conflicted[p] = true
		}
		index[p] = entry{f[0], f[1]}
	}

	var info strings.Builder
	fields := strings.Split(raw, "\x00")
	for i := 0; i+1 < len(fields); i += 2 {
		f := strings.Fields(strings.TrimPrefix(fields[i], ":"))
		if len(f) < 5 {
			return fmt.Errorf("unexpected diff-tree output %q", fields[i])
		}
		p, old, committed := fields[i+1], entry{f[0], f[2]}, entry{f[1], f[3]}
		if conflicted[p] {
			continue
		}
		cur, ok := index[p]
		if !ok {
			cur = entry{"000000", strings.Repeat("0", len(old.oid))}
		}
		switch {
		case cur == committed:
		case cur == old:
			writeEntry(&info, committed, p)
		default:
			if merged, ok := mergeStaged(repoPath, old, cur, committed); ok {
				writeEntry(&info, merged, p)
			}
		}
	}
	if info.Len() == 0 {
		return nil
	}
	if _, err := runGit(repoPath, nil, info.String(), "update-index", "-z", "--index-info"); err != nil {
		return err
	}
	// Entries written that way have no stat data; refresh it so plumbing
	// such as diff-files does not report the files as changed.
	_, _ = runGit(repoPath, nil, "", "update-index", "-q", "--refresh")
	return nil
}

func writeEntry(b *strings.Builder, e entry, path string) {
	if e.mode == "000000" {
		fmt.Fprintf(b, "0 %s\t%s\x00", e.oid, path) // mode 0 removes the path
		return
	}
	fmt.Fprintf(b, "%s %s\t%s\x00", e.mode, e.oid, path)
}

// mergeStaged merges the committed change to a file (old to committed) into
// the version the user has staged. It reports false unless all three are
// regular files and they merge cleanly.
func mergeStaged(repoPath string, old, staged, committed entry) (entry, bool) {
	if !old.regular() || !staged.regular() || !committed.regular() {
		return entry{}, false
	}
	dir, err := os.MkdirTemp("", "carya-commit-merge-*")
	if err != nil {
		return entry{}, false
	}
	defer os.RemoveAll(dir)
	files := []string{filepath.Join(dir, "staged"), filepath.Join(dir, "old"), filepath.Join(dir, "committed")}
	for i, e := range []entry{staged, old, committed} {
		content, err := runGit(repoPath, nil, "", "cat-file", "blob", e.oid)
		if err != nil || os.WriteFile(files[i], []byte(content), 0o600) != nil {
			return entry{}, false
		}
	}
	merged, err := runGit(repoPath, nil, "", "merge-file", "-p", "--quiet", files[0], files[1], files[2])
	if err != nil { // conflicts, or a binary file
		return entry{}, false
	}
	oid, err := runGit(repoPath, nil, merged, "hash-object", "-w", "--stdin")
	if err != nil {
		return entry{}, false
	}
	mode := staged.mode
	if staged.mode == old.mode {
		mode = committed.mode
	}
	return entry{mode, strings.TrimSpace(oid)}, true
}

// withIndexAt runs fn with an environment whose index is a temporary one
// holding rev's tree, or nothing when rev is "".
func withIndexAt(repoPath, rev string, fn func([]string) error) error {
	dir, err := os.MkdirTemp("", "carya-commit-index-*")
	if err != nil {
		return fmt.Errorf("create temporary index: %w", err)
	}
	defer os.RemoveAll(dir)

	env := append(os.Environ(), "GIT_INDEX_FILE="+filepath.Join(dir, "index"))
	seed := []string{"read-tree", "--empty"}
	if rev != "" {
		seed = []string{"read-tree", rev}
	}
	if _, err := runGit(repoPath, env, "", seed...); err != nil {
		return fmt.Errorf("seed temporary index: %w", err)
	}
	return fn(env)
}

// headCommit returns the commit HEAD points to, or "" on an unborn branch.
func headCommit(repoPath string) string {
	out, err := runGit(repoPath, nil, "", "rev-parse", "--verify", "--quiet", "HEAD^{commit}")
	if err != nil {
		return ""
	}
	return strings.TrimSpace(out)
}

// emptyTree returns the ID of the empty tree, which differs between SHA-1
// and SHA-256 repositories.
func emptyTree(repoPath string) (string, error) {
	out, err := runGit(repoPath, nil, "", "hash-object", "-t", "tree", "--stdin")
	return strings.TrimSpace(out), err
}

// runGit runs git in dir, with env (nil for this process's) and stdin, and
// returns its standard output; an error carries git's standard error.
func runGit(dir string, env []string, stdin string, args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = env
	cmd.Stdin = strings.NewReader(stdin)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		if msg := strings.TrimSpace(stderr.String()); msg != "" {
			err = fmt.Errorf("%w: %s", err, msg)
		}
		return string(out), fmt.Errorf("git %s: %w", args[0], err)
	}
	return string(out), nil
}
