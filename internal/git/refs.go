package git

import (
	"fmt"
	"log"
	"os"
	"os/exec"
	"strings"
)

// RefManager handles git ref operations for Carya user state sharing.
type RefManager struct {
	repoPath string
}

// UserRef is a user's published working state.
//
// A state is published as a commit whose tree is the working state and whose
// only parent is the commit that state was built on (the publisher's HEAD).
// Recording the base lets readers diff a state against its own base instead
// of their local HEAD, and lets git find the real merge base between two
// states. Refs written before this format point at a bare tree (local) or a
// parentless commit (pushed); those have no known base.
type UserRef struct {
	UserID     string
	Commit     string // state commit; empty for a bare-tree ref
	TreeHash   string
	BaseCommit string // commit the state was built on; empty when unknown
}

// HasBase reports whether the state records the commit it was built on.
func (u UserRef) HasBase() bool {
	return u.Commit != "" && u.BaseCommit != ""
}

// Base returns the commit the state was built on, or fallback when the state
// predates base recording.
func (u UserRef) Base(fallback string) string {
	if u.HasBase() {
		return u.BaseCommit
	}
	return fallback
}

// NewRefManager creates a new RefManager for the given repository.
func NewRefManager(repoPath string) *RefManager {
	return &RefManager{
		repoPath: repoPath,
	}
}

// PublishUserState records treeHash as userID's working state, built on the
// repository's current HEAD, at refs/carya/users/<userID>/tree. Publishing
// the same tree on the same base again leaves the ref untouched, so repeated
// publishes do not churn commits or pushes.
func (r *RefManager) PublishUserState(userID, treeHash string) (UserRef, error) {
	refPath := UserTreeRefPath(userID)

	base, err := r.headCommit()
	if err != nil {
		return UserRef{}, err
	}

	if current, err := r.readUserRef(userID, refPath); err == nil &&
		current.Commit != "" && current.TreeHash == treeHash && current.BaseCommit == base {
		return current, nil
	}

	args := []string{"commit-tree", treeHash, "-m", "carya state: " + userID}
	if base != "" {
		args = append(args, "-p", base)
	}
	commitCmd := exec.Command("git", args...)
	commitCmd.Dir = r.repoPath
	commitCmd.Env = caryaCommitEnv()
	out, err := commitCmd.CombinedOutput()
	if err != nil {
		log.Printf("Failed to create state commit for tree %s: %v, output: %s", treeHash, err, out)
		return UserRef{}, fmt.Errorf("failed to create state commit: %w\nOutput: %s", err, out)
	}
	commit := strings.TrimSpace(string(out))

	cmd := exec.Command("git", "update-ref", refPath, commit)
	cmd.Dir = r.repoPath
	if output, err := cmd.CombinedOutput(); err != nil {
		log.Printf("Failed to update user state ref: %v, output: %s", err, output)
		return UserRef{}, fmt.Errorf("failed to update user state ref: %w\nOutput: %s", err, output)
	}

	return UserRef{UserID: userID, Commit: commit, TreeHash: treeHash, BaseCommit: base}, nil
}

// CommitTree writes a commit for tree with the given parents, authored by
// carya, without touching any ref.
func (r *RefManager) CommitTree(tree, message string, parents ...string) (string, error) {
	args := []string{"commit-tree", tree, "-m", message}
	for _, p := range parents {
		args = append(args, "-p", p)
	}
	cmd := exec.Command("git", args...)
	cmd.Dir = r.repoPath
	cmd.Env = caryaCommitEnv()
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("commit-tree: %w\n%s", err, out)
	}
	return strings.TrimSpace(string(out)), nil
}

// UpdateRef points ref at commit.
func (r *RefManager) UpdateRef(ref, commit string) error {
	cmd := exec.Command("git", "update-ref", ref, commit)
	cmd.Dir = r.repoPath
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("update-ref %s: %w\n%s", ref, err, out)
	}
	return nil
}

// DefaultBranch returns the ref that work is integrated into: the remote's
// default branch when origin/HEAD is known, else a local main or master,
// else HEAD.
func (r *RefManager) DefaultBranch() string {
	cmd := exec.Command("git", "symbolic-ref", "--quiet", "--short", "refs/remotes/origin/HEAD")
	cmd.Dir = r.repoPath
	if out, err := cmd.Output(); err == nil {
		if ref := strings.TrimSpace(string(out)); ref != "" {
			return ref
		}
	}
	for _, candidate := range []string{"origin/main", "origin/master", "main", "master"} {
		check := exec.Command("git", "rev-parse", "--verify", "--quiet", candidate+"^{commit}")
		check.Dir = r.repoPath
		if check.Run() == nil {
			return candidate
		}
	}
	return "HEAD"
}

// headCommit returns the commit HEAD points at, or "" on an unborn branch.
func (r *RefManager) headCommit() (string, error) {
	cmd := exec.Command("git", "rev-parse", "--verify", "--quiet", "HEAD^{commit}")
	cmd.Dir = r.repoPath
	output, err := cmd.Output()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok && exitErr.ExitCode() == 1 {
			return "", nil
		}
		return "", fmt.Errorf("failed to resolve HEAD: %w", err)
	}
	return strings.TrimSpace(string(output)), nil
}

func caryaCommitEnv() []string {
	return append(os.Environ(),
		"GIT_AUTHOR_NAME=carya",
		"GIT_AUTHOR_EMAIL=carya@internal",
		"GIT_COMMITTER_NAME=carya",
		"GIT_COMMITTER_EMAIL=carya@internal",
	)
}

// readUserRef resolves a state ref, which may be a state commit or a bare
// tree written before states recorded their base.
func (r *RefManager) readUserRef(userID, refPath string) (UserRef, error) {
	typeCmd := exec.Command("git", "cat-file", "-t", refPath)
	typeCmd.Dir = r.repoPath
	typeOut, err := typeCmd.Output()
	if err != nil {
		return UserRef{}, err
	}

	switch strings.TrimSpace(string(typeOut)) {
	case "commit":
		cmd := exec.Command("git", "show", "-s", "--format=%H %T %P", refPath)
		cmd.Dir = r.repoPath
		out, err := cmd.Output()
		if err != nil {
			return UserRef{}, err
		}
		fields := strings.Fields(string(out))
		if len(fields) < 2 {
			return UserRef{}, fmt.Errorf("unexpected commit format for %s: %q", refPath, out)
		}
		ref := UserRef{UserID: userID, Commit: fields[0], TreeHash: fields[1]}
		// A state has exactly one parent: its base. Parentless commits are
		// legacy pushes; anything with more parents is not a carya state.
		if len(fields) == 3 {
			ref.BaseCommit = fields[2]
		}
		return ref, nil
	case "tree":
		cmd := exec.Command("git", "rev-parse", refPath+"^{tree}")
		cmd.Dir = r.repoPath
		out, err := cmd.Output()
		if err != nil {
			return UserRef{}, err
		}
		return UserRef{UserID: userID, TreeHash: strings.TrimSpace(string(out))}, nil
	default:
		return UserRef{}, fmt.Errorf("%s is not a commit or tree", refPath)
	}
}

// GetUserState retrieves a user's published state, preferring the local ref
// over the one fetched from the team remote.
func (r *RefManager) GetUserState(userID string) (UserRef, error) {
	if ref, err := r.readUserRef(userID, UserTreeRefPath(userID)); err == nil {
		return ref, nil
	}

	if ref, err := r.readUserRef(userID, RemoteUserTreeRefPath(defaultTeamRemote, userID)); err == nil {
		return ref, nil
	}

	log.Printf("User ref not found: %s", userID)
	return UserRef{}, fmt.Errorf("user ref not found: %s", userID)
}

// GetUserTreeRef retrieves the tree hash for a user's published state.
func (r *RefManager) GetUserTreeRef(userID string) (string, error) {
	ref, err := r.GetUserState(userID)
	if err != nil {
		return "", err
	}
	return ref.TreeHash, nil
}

// DeleteUserTreeRef removes a user's tree ref.
func (r *RefManager) DeleteUserTreeRef(userID string) error {
	refPath := UserTreeRefPath(userID)

	cmd := exec.Command("git", "update-ref", "-d", refPath)
	cmd.Dir = r.repoPath

	if output, err := cmd.CombinedOutput(); err != nil {
		log.Printf("Failed to delete user tree ref: %v, output: %s", err, output)
		return fmt.Errorf("failed to delete user tree ref: %w\nOutput: %s", err, output)
	}

	return nil
}

// StateRef is one ref holding a published state: a local ref, or the copy
// fetched from the team remote.
type StateRef struct {
	Ref    string
	Remote bool
	UserRef
}

// ListStateRefs lists every state ref, local and fetched, without merging a
// user's local and remote copies the way ListUserRefs does.
func (r *RefManager) ListStateRefs() ([]StateRef, error) {
	var out []StateRef
	for _, source := range []struct {
		prefix string
		remote bool
	}{
		{LocalUserRefsPrefix(), false},
		{RemoteUserRefsPrefix(defaultTeamRemote), true},
	} {
		cmd := exec.Command("git", "for-each-ref", "--format=%(refname)", source.prefix)
		cmd.Dir = r.repoPath
		output, err := cmd.Output()
		if err != nil {
			return nil, fmt.Errorf("list %s: %w", source.prefix, err)
		}
		for _, refPath := range strings.Split(strings.TrimSpace(string(output)), "\n") {
			userID := strings.TrimSuffix(strings.TrimPrefix(refPath, source.prefix), "/tree")
			if refPath == "" || userID == "" || strings.Contains(userID, "/") {
				continue
			}
			ref, err := r.readUserRef(userID, refPath)
			if err != nil {
				continue
			}
			out = append(out, StateRef{Ref: refPath, Remote: source.remote, UserRef: ref})
		}
	}
	return out, nil
}

// ListUserRefs returns all user refs in the repository.
func (r *RefManager) ListUserRefs() ([]UserRef, error) {
	type refSource struct {
		prefix string
		local  bool
	}

	sources := []refSource{
		{prefix: RemoteUserRefsPrefix(defaultTeamRemote), local: false},
		{prefix: LocalUserRefsPrefix(), local: true},
	}

	byUser := make(map[string]UserRef)
	for _, source := range sources {
		cmd := exec.Command("git", "for-each-ref", "--format=%(refname)", source.prefix)
		cmd.Dir = r.repoPath

		output, err := cmd.Output()
		if err != nil {
			continue
		}

		for _, refPath := range strings.Split(strings.TrimSpace(string(output)), "\n") {
			refPath = strings.TrimSpace(refPath)
			if refPath == "" {
				continue
			}
			userID := strings.TrimPrefix(refPath, source.prefix)
			userID = strings.TrimSuffix(userID, "/tree")
			if userID == "" || strings.Contains(userID, "/") {
				continue
			}

			if _, exists := byUser[userID]; exists && !source.local {
				continue
			}

			ref, err := r.readUserRef(userID, refPath)
			if err != nil {
				continue
			}
			byUser[userID] = ref
		}
	}

	if len(byUser) == 0 {
		return nil, nil
	}

	refs := make([]UserRef, 0, len(byUser))
	for _, ref := range byUser {
		refs = append(refs, ref)
	}

	return refs, nil
}

// SetBaseRef sets refs/carya/base to the current HEAD.
func (r *RefManager) SetBaseRef() error {
	// Get current HEAD
	cmd := exec.Command("git", "rev-parse", "HEAD")
	cmd.Dir = r.repoPath

	output, err := cmd.Output()
	if err != nil {
		log.Printf("Failed to get HEAD: %v", err)
		return fmt.Errorf("failed to get HEAD: %w", err)
	}

	headHash := strings.TrimSpace(string(output))

	// Update the base ref
	cmd = exec.Command("git", "update-ref", "refs/carya/base", headHash)
	cmd.Dir = r.repoPath

	if output, err := cmd.CombinedOutput(); err != nil {
		log.Printf("Failed to set base ref: %v, output: %s", err, output)
		return fmt.Errorf("failed to set base ref: %w\nOutput: %s", err, output)
	}

	return nil
}

// GetBaseRef retrieves the base ref hash.
func (r *RefManager) GetBaseRef() (string, error) {
	cmd := exec.Command("git", "show-ref", "--hash", "refs/carya/base")
	cmd.Dir = r.repoPath

	output, err := cmd.Output()
	if err != nil {
		log.Printf("Base ref not found")
		return "", fmt.Errorf("base ref not found")
	}

	return strings.TrimSpace(string(output)), nil
}

// HasRemote reports whether the repository has a remote with this name.
func (r *RefManager) HasRemote(remote string) bool {
	cmd := exec.Command("git", "remote", "get-url", remote)
	cmd.Dir = r.repoPath
	return cmd.Run() == nil
}

// FetchCaryaRefs fetches all carya refs from a remote.
func (r *RefManager) FetchCaryaRefs(remote string) error {
	// Verify the remote exists before attempting to fetch
	checkCmd := exec.Command("git", "remote", "get-url", remote)
	checkCmd.Dir = r.repoPath
	if _, err := checkCmd.Output(); err != nil {
		log.Printf("Remote '%s' not found", remote)
		return fmt.Errorf("remote '%s' not found", remote)
	}

	cmd := exec.Command("git", "fetch", "--prune", remote, "+refs/carya/*:refs/remotes/"+remote+"/carya/*")
	cmd.Dir = r.repoPath

	if output, err := cmd.CombinedOutput(); err != nil {
		outputStr := strings.TrimSpace(string(output))
		// "no match" just means no carya refs exist on the remote yet
		if strings.Contains(outputStr, "no match") {
			return nil
		}
		log.Printf("Failed to fetch carya refs: %v, output: %s", err, outputStr)
		return fmt.Errorf("failed to fetch carya refs: %w\nOutput: %s", err, outputStr)
	}

	return nil
}

// PushUserRef pushes a user's published state to a remote. The state commit
// carries its base, so the push also sends any base history the remote lacks.
func (r *RefManager) PushUserRef(remote, userID string) error {
	ref, err := r.readUserRef(userID, UserTreeRefPath(userID))
	if err != nil {
		return fmt.Errorf("failed to get user state ref: %w", err)
	}

	commitSHA := ref.Commit
	if commitSHA == "" {
		// A bare-tree ref from before states recorded their base: refs can
		// only be pushed as commits, and its base is unknown, so wrap it in a
		// parentless commit as older versions did.
		commitCmd := exec.Command("git", "commit-tree", ref.TreeHash, "-m", "state snapshot")
		commitCmd.Dir = r.repoPath
		commitCmd.Env = caryaCommitEnv()
		out, err := commitCmd.Output()
		if err != nil {
			log.Printf("Failed to create commit for tree %s: %v", ref.TreeHash, err)
			return fmt.Errorf("failed to create commit for tree: %w", err)
		}
		commitSHA = strings.TrimSpace(string(out))
	}

	// Push the commit to the user's ref on the remote
	cmd := exec.Command("git", "push", remote,
		fmt.Sprintf("%s:%s", commitSHA, UserTreeRefPath(userID)),
		"--force",
	)
	cmd.Dir = r.repoPath

	if output, err := cmd.CombinedOutput(); err != nil {
		log.Printf("Failed to push user ref: %v, output: %s", err, output)
		return fmt.Errorf("failed to push user ref: %w\nOutput: %s", err, output)
	}

	return nil
}

// GetHEADTreeHash returns the tree hash for the current HEAD commit.
func (r *RefManager) GetHEADTreeHash() (string, error) {
	cmd := exec.Command("git", "rev-parse", "HEAD^{tree}")
	cmd.Dir = r.repoPath

	output, err := cmd.Output()
	if err != nil {
		log.Printf("Failed to get HEAD tree: %v", err)
		return "", fmt.Errorf("failed to get HEAD tree: %w", err)
	}

	return strings.TrimSpace(string(output)), nil
}

// ResolveRef resolves a ref name to its hash.
func (r *RefManager) ResolveRef(refName string) (string, error) {
	cmd := exec.Command("git", "rev-parse", refName)
	cmd.Dir = r.repoPath

	output, err := cmd.Output()
	if err != nil {
		log.Printf("Failed to resolve ref %s: %v", refName, err)
		return "", fmt.Errorf("failed to resolve ref %s: %w", refName, err)
	}

	return strings.TrimSpace(string(output)), nil
}
