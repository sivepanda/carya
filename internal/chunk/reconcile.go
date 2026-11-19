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
				out[i] = Retention{DecisionPrune, reasonNoLongerMatches}
			}
		}
	}
	return out
}

const reasonNoLongerMatches = "no longer matches"

// Tip is a file's content at the top of its chunk stack.
type Tip struct {
	Exists bool   // false when the stack leaves the file deleted or absent
	Blob   string // the file's blob hash, when it exists
}

// StackTip returns path as it stands once its kept chunks (the ones
// DecideRetentionAll keeps) are applied on top of HEAD in recording order.
// That is the version the next chunk of path has to be diffed against for it
// to stack on the existing ones. chunks may include other files' chunks,
// which are ignored. The tip is computed on a temporary index, so neither the
// repository's index nor HEAD is touched; its blob is written to the
// repository's object store.
//
// A file whose chunks have all landed in HEAD (or upstream) has HEAD's
// version as its tip. ok is false when there is no stack to build on: the
// file has chunks still to be committed but none of them applies any more
// (the stack is stale), it has a binary chunk, whose change cannot be
// applied, or git failed.
func StackTip(repoPath, path string, chunks []Chunk) (tip Tip, ok bool) {
	var stack []Chunk
	for _, c := range chunks {
		if c.FilePath != path {
			continue
		}
		if isBinaryPatch(strings.TrimSpace(c.Diff)) {
			return Tip{}, false
		}
		stack = append(stack, c)
	}
	sort.SliceStable(stack, func(a, b int) bool { return RecordedBefore(stack[a], stack[b]) })

	idx, err := newScratchIndex(repoPath, "HEAD")
	if err != nil {
		return Tip{}, false
	}
	defer idx.remove()

	kept, stale := 0, 0
	for k, r := range DecideRetentionAll(repoPath, stack) {
		switch {
		case r.Decision == DecisionKeep:
			if !idx.apply(stack[k].Diff, false) {
				return Tip{}, false
			}
			kept++
		case r.Reason == reasonNoLongerMatches:
			stale++
		}
	}
	if stale > 0 && kept == 0 {
		return Tip{}, false
	}

	blob, exists, err := idx.entry(path)
	if err != nil {
		return Tip{}, false
	}
	return Tip{Exists: exists, Blob: blob}, true
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

// entry returns path's blob hash in the index, and whether it is there.
func (x *scratchIndex) entry(path string) (blob string, exists bool, err error) {
	cmd := exec.Command("git", "ls-files", "--cached", "-s", "-z", "--", ":(top,literal)"+path)
	cmd.Dir = x.repoPath
	cmd.Env = x.env
	out, err := cmd.Output()
	if err != nil {
		return "", false, err
	}
	// format: <mode> <hash> <stage>\t<path>\0
	for record := range strings.SplitSeq(string(out), "\x00") {
		meta, entryPath, found := strings.Cut(record, "\t")
		if !found || entryPath != path {
			continue
		}
		if fields := strings.Fields(meta); len(fields) >= 2 {
			return fields[1], true, nil
		}
	}
	return "", false, nil
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
