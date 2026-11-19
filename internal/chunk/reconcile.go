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
// differs from HEAD. A binary chunk does replace the whole file, though, so it
// resets the stack: the text chunks recorded after it were diffed against the
// content it left (the blob the daemon recorded as its hash, or no file for a
// deletion), and are applied on top of that content instead of HEAD and the
// chunks before it. Until HEAD has that content compose cannot apply them, so
// they are kept with ReasonOnBinary. If the content is unknown (its blob is
// gone) they are kept with that reason for as long as the binary chunk is.
// Once the binary chunk is pruned the path matches HEAD again, and the chunks
// after it are applied on top of HEAD.
func DecideRetentionAll(repoPath string, chunks []Chunk) []Retention {
	out := make([]Retention, len(chunks))
	var stacked []int
	text := false
	for i, c := range chunks {
		patch := strings.TrimSpace(c.Diff)
		if patch == "" {
			out[i] = Retention{DecisionPrune, "empty diff"}
			continue
		}
		text = text || !isBinaryPatch(patch)
		stacked = append(stacked, i)
	}

	upstreamRef := ""
	if text {
		upstreamRef = currentUpstreamRef(repoPath)
	}
	for _, stack := range stacksByFile(chunks, stacked) {
		judgeStack(repoPath, chunks[stack[0]].FilePath, upstreamRef, chunks, stack, out, false)
	}
	return out
}

const (
	reasonKept            = "still applicable"
	reasonNoLongerMatches = "no longer matches"

	// ReasonOnBinary is why a text chunk recorded after a binary chunk of its
	// file is kept while HEAD lacks the content the binary chunk left: it
	// applies on top of that content, but compose cannot apply the binary
	// chunk, so it cannot commit this one yet either.
	ReasonOnBinary = "waits on an uncommitted binary change"
)

// judgeStack decides, into out, the retention of path's chunks (stack:
// indices into chunks, oldest first, none of them empty), as described for
// DecideRetentionAll. With wantTip it also returns path's content once the
// kept chunks are applied in order; ok is false when that cannot be told.
func judgeStack(repoPath, path, upstreamRef string, chunks []Chunk, stack []int, out []Retention, wantTip bool) (tip Tip, ok bool) {
	var text []int
	for _, i := range stack {
		if !isBinaryPatch(strings.TrimSpace(chunks[i].Diff)) {
			text = append(text, i)
		}
	}
	landed := make(map[int]Retention)
	if len(text) > 0 {
		patches := stackPatches(chunks, text)
		committed := containedIn(repoPath, "HEAD", patches)
		upstream := make([]bool, len(text))
		if upstreamRef != "" {
			upstream = containedIn(repoPath, upstreamRef, patches)
		}
		for k, i := range text {
			switch {
			case committed[k]:
				landed[i] = Retention{DecisionPrune, "already applied/committed"}
			case upstream[k]:
				landed[i] = Retention{DecisionPrune, "already in upstream"}
			}
		}
	}

	// The remaining text chunks are applied in runs: the first on top of
	// HEAD, each later one on top of what the binary chunk before it left.
	runs := []stackRun{{}}
	for _, i := range stack {
		c := chunks[i]
		if r, done := landed[i]; done {
			out[i] = r
			continue
		}
		if !isBinaryPatch(strings.TrimSpace(c.Diff)) {
			last := &runs[len(runs)-1]
			last.chunks = append(last.chunks, i)
			continue
		}
		out[i] = binaryRetention(repoPath, c)
		next := stackRun{}
		if out[i].Decision == DecisionKeep {
			next.base = binaryResult(repoPath, c)
		}
		runs = append(runs, next)
	}

	for k, run := range runs {
		last := k == len(runs)-1
		runTip, runOK := run.apply(repoPath, path, chunks, out, wantTip && last)
		if last {
			tip, ok = runTip, runOK
		}
	}
	return tip, ok
}

// stackRun is a run of a file's text chunks (indices into chunks, oldest
// first) recorded on top of base.
type stackRun struct {
	base   runBase
	chunks []int
}

// runBase is the content a run of text chunks was recorded on top of: HEAD's
// version of the file, or the content a binary chunk left it with.
type runBase struct {
	binary bool // left by a binary chunk; HEAD's version otherwise
	known  bool // whether the binary chunk's content is known
	tip    Tip  // the binary chunk's content, when known
}

// apply decides each of the run's chunks into out: kept when it applies on
// top of the run's base and its earlier kept chunks, pruned as no longer
// matching when it does not. With wantTip it also returns path's content at
// the top of the run.
func (r stackRun) apply(repoPath, path string, chunks []Chunk, out []Retention, wantTip bool) (Tip, bool) {
	// Chunks on top of a binary change that cannot be checked are kept
	// along with the binary chunk, which is kept while the path still
	// differs from HEAD.
	onBinary := func() (Tip, bool) {
		for _, i := range r.chunks {
			out[i] = Retention{DecisionKeep, ReasonOnBinary}
		}
		return Tip{}, false
	}
	if r.base.binary && !r.base.known {
		return onBinary()
	}
	if len(r.chunks) == 0 && !wantTip {
		return Tip{}, false
	}

	idx, err := newScratchIndex(repoPath, "HEAD")
	if err != nil {
		if r.base.binary {
			return onBinary()
		}
		for _, i := range r.chunks {
			out[i] = Retention{DecisionPrune, reasonNoLongerMatches}
		}
		return Tip{}, false
	}
	defer idx.remove()

	keep := Retention{DecisionKeep, reasonKept}
	if r.base.binary {
		head, err := idx.tip(path)
		if err == nil && head != r.base.tip {
			keep.Reason = ReasonOnBinary
			err = idx.set(path, r.base.tip)
		}
		if err != nil {
			return onBinary()
		}
	}

	for _, i := range r.chunks {
		if idx.apply(chunks[i].Diff, false) {
			out[i] = keep
		} else {
			out[i] = Retention{DecisionPrune, reasonNoLongerMatches}
		}
	}
	if !wantTip {
		return Tip{}, false
	}
	tip, err := idx.tip(path)
	return tip, err == nil
}

// binaryResult returns what a kept binary chunk left its file with. The
// daemon records the blob of the file's latest content as a chunk's hash, or
// a "deleted-" marker when the chunk ends with the file gone. The content is
// unknown when the hash names no blob in the repository: it was never written
// there, or has been garbage collected since.
func binaryResult(repoPath string, c Chunk) runBase {
	hash := string(c.Hash)
	if strings.HasPrefix(hash, "deleted-") {
		return runBase{binary: true, known: true}
	}
	if !isObjectID(hash) {
		return runBase{binary: true}
	}
	cmd := exec.Command("git", "cat-file", "-t", hash)
	cmd.Dir = repoPath
	out, err := cmd.Output()
	if err != nil || strings.TrimSpace(string(out)) != "blob" {
		return runBase{binary: true}
	}
	return runBase{binary: true, known: true, tip: Tip{Exists: true, Blob: hash}}
}

// isObjectID reports whether s is a full SHA-1 or SHA-256 object name.
func isObjectID(s string) bool {
	if len(s) != 40 && len(s) != 64 {
		return false
	}
	for _, r := range s {
		if !strings.ContainsRune("0123456789abcdef", r) {
			return false
		}
	}
	return true
}

// Tip is a file's content at the top of its chunk stack.
type Tip struct {
	Exists bool   // false when the stack leaves the file deleted or absent
	Blob   string // the file's blob hash, when it exists
}

// StackTip returns path as it stands once its kept chunks (the ones
// DecideRetentionAll keeps) are applied on top of HEAD in recording order,
// a binary chunk resetting it to the content that chunk left. That is the
// version the next chunk of path has to be diffed against for it to stack on
// the existing ones. chunks may include other files' chunks, which are
// ignored. The tip is computed on a temporary index, so neither the
// repository's index nor HEAD is touched; its blob is written to the
// repository's object store.
//
// A file whose chunks have all landed in HEAD (or upstream) has HEAD's
// version as its tip. ok is false when there is no stack to build on: the
// file has chunks still to be committed but none of them applies any more
// (the stack is stale), the content a binary chunk left is unknown, or git
// failed.
func StackTip(repoPath, path string, chunks []Chunk) (tip Tip, ok bool) {
	var mine []int
	for i, c := range chunks {
		if c.FilePath == path && strings.TrimSpace(c.Diff) != "" {
			mine = append(mine, i)
		}
	}
	var stack []int
	if len(mine) > 0 {
		stack = stacksByFile(chunks, mine)[0]
	}

	out := make([]Retention, len(chunks))
	if tip, ok = judgeStack(repoPath, path, currentUpstreamRef(repoPath), chunks, stack, out, true); !ok {
		return Tip{}, false
	}

	kept, stale := 0, 0
	for _, i := range stack {
		switch {
		case out[i].Decision == DecisionKeep:
			kept++
		case out[i].Reason == reasonNoLongerMatches:
			stale++
		}
	}
	if stale > 0 && kept == 0 {
		return Tip{}, false
	}
	return tip, true
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
// so a patch that does not apply cleanly leaves the index unchanged. It
// applies the patch as it is, whatever apply.whitespace says: =error would
// refuse a patch that adds (or, in reverse, removes) trailing blanks, and
// =fix would leave the index different from what was committed.
func (x *scratchIndex) apply(patch string, reverse bool) bool {
	args := []string{"apply", "--cached", "--whitespace=nowarn"}
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

// tip returns path's content in the index.
func (x *scratchIndex) tip(path string) (Tip, error) {
	cmd := exec.Command("git", "ls-files", "--cached", "-s", "-z", "--", ":(top,literal)"+path)
	cmd.Dir = x.repoPath
	cmd.Env = x.env
	out, err := cmd.Output()
	if err != nil {
		return Tip{}, err
	}
	// format: <mode> <hash> <stage>\t<path>\0
	for record := range strings.SplitSeq(string(out), "\x00") {
		meta, entryPath, found := strings.Cut(record, "\t")
		if !found || entryPath != path {
			continue
		}
		if fields := strings.Fields(meta); len(fields) >= 2 {
			return Tip{Exists: true, Blob: fields[1]}, nil
		}
	}
	return Tip{}, nil
}

// set makes path hold tip's content in the index, or removes it when tip has
// no file.
func (x *scratchIndex) set(path string, tip Tip) error {
	args := []string{"update-index", "--force-remove", "--", path}
	if tip.Exists {
		args = []string{"update-index", "--add", "--cacheinfo", "100644", tip.Blob, path}
	}
	cmd := exec.Command("git", args...)
	cmd.Dir = x.repoPath
	cmd.Env = x.env
	return cmd.Run()
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
