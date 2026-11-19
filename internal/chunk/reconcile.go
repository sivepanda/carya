package chunk

import (
	"os"
	"os/exec"
	"sort"
	"strings"
)

type ReconcileDecision string

const (
	DecisionKeep  ReconcileDecision = "keep"
	DecisionPrune ReconcileDecision = "prune"
)

// Retention is the decision reached for one chunk, with the reason for it.
type Retention struct {
	Decision ReconcileDecision
	Reason   string
}

// DecideRetention determines whether a single chunk should be kept based on the
// current git state. Use DecideRetentionAll when a file may have several
// chunks, since a chunk recorded on top of another only applies after it.
func DecideRetention(repoPath string, c Chunk) (ReconcileDecision, string) {
	r := DecideRetentionAll(repoPath, []Chunk{c})[0]
	return r.Decision, r.Reason
}

// DecideRetentionAll decides, for each chunk, whether it should be kept based
// on the current git state. The result is in the same order as chunks.
//
// The daemon diffs each new chunk against the shadow index as the previous
// chunk for that file left it, not against HEAD, so a file's chunks form a
// stack: a later chunk's context may only exist once the earlier ones are
// applied. Each file's chunks are therefore judged together, in the order they
// were recorded:
//   - a chunk is pruned as already applied/committed when HEAD contains it,
//     or as already in upstream when the upstream branch does;
//   - otherwise it is kept when it applies on top of HEAD plus the earlier
//     chunks that are kept, and pruned as no longer matching when it does not.
//
// Binary chunks cannot be applied, so they are kept while their path still
// differs from HEAD, as before, and take no part in the stack.
func DecideRetentionAll(repoPath string, chunks []Chunk) []Retention {
	out := make([]Retention, len(chunks))
	var text []int
	for i, c := range chunks {
		patch := strings.TrimSpace(c.Diff)
		switch {
		case patch == "":
			out[i] = Retention{DecisionPrune, "empty diff"}
		case isBinaryPatch(patch):
			out[i] = binaryRetention(repoPath, c)
		default:
			text = append(text, i)
		}
	}
	if len(text) == 0 {
		return out
	}

	upstreamRef := currentUpstreamRef(repoPath)
	for _, stack := range stacksByFile(chunks, text) {
		patches := stackPatches(chunks, stack)
		committed := containedIn(repoPath, "HEAD", patches)
		upstream := make([]bool, len(stack))
		if upstreamRef != "" {
			upstream = containedIn(repoPath, upstreamRef, patches)
		}

		var live []int // positions in stack still to be tried on top of HEAD
		for j, i := range stack {
			switch {
			case committed[j]:
				out[i] = Retention{DecisionPrune, "already applied/committed"}
			case upstream[j]:
				out[i] = Retention{DecisionPrune, "already in upstream"}
			default:
				live = append(live, j)
			}
		}

		livePatches := make([]string, len(live))
		for k, j := range live {
			livePatches[k] = patches[j]
		}
		for k, applied := range applyInOrder(repoPath, livePatches) {
			i := stack[live[k]]
			if applied {
				out[i] = Retention{DecisionKeep, "still applicable"}
			} else {
				out[i] = Retention{DecisionPrune, "no longer matches"}
			}
		}
	}
	return out
}

// AppliedAtHEAD reports whether HEAD already contains a single chunk's change.
// Use AppliedAtHEADAll when a file may have several chunks.
func AppliedAtHEAD(repoPath string, c Chunk) bool {
	return AppliedAtHEADAll(repoPath, []Chunk{c})[0]
}

// AppliedAtHEADAll reports, for each chunk, whether HEAD already contains its
// change. A file's chunks are judged as a stack, as in DecideRetentionAll, so
// an earlier chunk whose lines a later committed chunk rewrote still counts.
func AppliedAtHEADAll(repoPath string, chunks []Chunk) []bool {
	out := make([]bool, len(chunks))
	var text []int
	for i, c := range chunks {
		patch := strings.TrimSpace(c.Diff)
		switch {
		case patch == "":
		case isBinaryPatch(patch):
			modified, err := isPathModified(repoPath, c.FilePath)
			out[i] = err == nil && !modified
		default:
			text = append(text, i)
		}
	}
	for _, stack := range stacksByFile(chunks, text) {
		for j, applied := range containedIn(repoPath, "HEAD", stackPatches(chunks, stack)) {
			out[stack[j]] = applied
		}
	}
	return out
}

func isBinaryPatch(trimmedPatch string) bool {
	return strings.HasPrefix(trimmedPatch, "Binary file ")
}

func binaryRetention(repoPath string, c Chunk) Retention {
	modified, err := isPathModified(repoPath, c.FilePath)
	if err != nil {
		return Retention{DecisionPrune, "binary diff check failed"}
	}
	if modified {
		return Retention{DecisionKeep, "binary path still modified"}
	}
	return Retention{DecisionPrune, "binary path matches HEAD"}
}

// stacksByFile groups the chunk indices in idx by file and orders each group
// by when its chunks were recorded (see RecordedBefore).
func stacksByFile(chunks []Chunk, idx []int) [][]int {
	byFile := make(map[string][]int)
	var files []string
	for _, i := range idx {
		path := chunks[i].FilePath
		if _, seen := byFile[path]; !seen {
			files = append(files, path)
		}
		byFile[path] = append(byFile[path], i)
	}

	stacks := make([][]int, 0, len(files))
	for _, path := range files {
		stack := byFile[path]
		sort.SliceStable(stack, func(a, b int) bool {
			return RecordedBefore(chunks[stack[a]], chunks[stack[b]])
		})
		stacks = append(stacks, stack)
	}
	return stacks
}

func stackPatches(chunks []Chunk, stack []int) []string {
	patches := make([]string, len(stack))
	for j, i := range stack {
		patches[j] = chunks[i].Diff
	}
	return patches
}

// containedIn reports, for each patch of a stack (oldest first), whether ref
// already contains it. The stack is peeled off ref from the top down: a patch
// is contained when it reverse-applies once the later patches ref contains
// have been reversed, so a chunk is still found when a later chunk in the
// same commit rewrote its lines.
func containedIn(repoPath, ref string, patches []string) []bool {
	found := make([]bool, len(patches))
	idx, err := newScratchIndex(repoPath, ref)
	if err != nil {
		return found
	}
	defer idx.remove()

	for j := len(patches) - 1; j >= 0; j-- {
		found[j] = idx.apply(patches[j], true)
	}
	return found
}

// applyInOrder applies patches one after another on top of HEAD, leaving out
// any that do not apply, and reports which ones did.
func applyInOrder(repoPath string, patches []string) []bool {
	applied := make([]bool, len(patches))
	if len(patches) == 0 {
		return applied
	}
	idx, err := newScratchIndex(repoPath, "HEAD")
	if err != nil {
		return applied
	}
	defer idx.remove()

	for j, patch := range patches {
		applied[j] = idx.apply(patch, false)
	}
	return applied
}

func currentUpstreamRef(repoPath string) string {
	cmd := exec.Command("git", "rev-parse", "--abbrev-ref", "--symbolic-full-name", "@{upstream}")
	cmd.Dir = repoPath
	out, err := cmd.Output()
	if err != nil {
		return ""
	}

	return strings.TrimSpace(string(out))
}

// scratchIndex is a temporary index file seeded from a ref, used to try
// patches without touching the repository's real index.
type scratchIndex struct {
	repoPath string
	path     string
	env      []string
}

func newScratchIndex(repoPath, ref string) (*scratchIndex, error) {
	f, err := os.CreateTemp("", "carya-reconcile-index-*")
	if err != nil {
		return nil, err
	}
	idx := &scratchIndex{repoPath: repoPath, path: f.Name()}
	_ = f.Close()
	idx.env = append(os.Environ(), "GIT_INDEX_FILE="+idx.path)

	readTree := exec.Command("git", "read-tree", ref)
	readTree.Dir = repoPath
	readTree.Env = idx.env
	if err := readTree.Run(); err != nil {
		idx.remove()
		return nil, err
	}
	return idx, nil
}

// apply applies patch to the index, in reverse if asked. git apply is atomic,
// so a patch that does not apply cleanly leaves the index unchanged.
func (x *scratchIndex) apply(patch string, reverse bool) bool {
	args := []string{"apply", "--cached"}
	if reverse {
		args = append(args, "--reverse")
	}
	args = append(args, "-")
	cmd := exec.Command("git", args...)
	cmd.Dir = x.repoPath
	cmd.Env = x.env
	cmd.Stdin = strings.NewReader(patch)
	return cmd.Run() == nil
}

func (x *scratchIndex) remove() {
	_ = os.Remove(x.path)
}

func isPathModified(repoPath, path string) (bool, error) {
	cmd := exec.Command("git", "diff", "--quiet", "HEAD", "--", path)
	cmd.Dir = repoPath
	err := cmd.Run()
	if err == nil {
		return false, nil
	}
	if exitErr, ok := err.(*exec.ExitError); ok && exitErr.ExitCode() == 1 {
		return true, nil
	}
	return false, err
}
