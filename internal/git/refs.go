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

// UserRef represents a user's tree reference.
type UserRef struct {
	UserID   string
	TreeHash string
}

// NewRefManager creates a new RefManager for the given repository.
func NewRefManager(repoPath string) *RefManager {
	return &RefManager{
		repoPath: repoPath,
	}
}

// UpdateUserTreeRef updates or creates the ref for a user's working tree.
// Ref path: refs/carya/users/<userID>/tree
func (r *RefManager) UpdateUserTreeRef(userID, treeHash string) error {
	refPath := UserTreeRefPath(userID)

	cmd := exec.Command("git", "update-ref", refPath, treeHash)
	cmd.Dir = r.repoPath

	if output, err := cmd.CombinedOutput(); err != nil {
		log.Printf("Failed to update user tree ref: %v, output: %s", err, output)
		return fmt.Errorf("failed to update user tree ref: %w\nOutput: %s", err, output)
	}

	return nil
}

func (r *RefManager) getTreeHashForRef(refPath string) (string, error) {
	cmd := exec.Command("git", "rev-parse", refPath+"^{tree}")
	cmd.Dir = r.repoPath

	output, err := cmd.Output()
	if err != nil {
		return "", err
	}

	return strings.TrimSpace(string(output)), nil
}

func (r *RefManager) getLocalUserTreeRef(userID string) (string, error) {
	return r.getTreeHashForRef(UserTreeRefPath(userID))
}

func (r *RefManager) getRemoteUserTreeRef(remote, userID string) (string, error) {
	return r.getTreeHashForRef(RemoteUserTreeRefPath(remote, userID))
}

// GetUserTreeRef retrieves the tree hash for a user's ref.
func (r *RefManager) GetUserTreeRef(userID string) (string, error) {
	if hash, err := r.getLocalUserTreeRef(userID); err == nil {
		return hash, nil
	}

	if hash, err := r.getRemoteUserTreeRef(defaultTeamRemote, userID); err == nil {
		return hash, nil
	}

	log.Printf("User ref not found: %s", userID)
	return "", fmt.Errorf("user ref not found: %s", userID)
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

			hash, err := r.getTreeHashForRef(refPath)
			if err != nil {
				continue
			}

			if _, exists := byUser[userID]; exists && !source.local {
				continue
			}
			byUser[userID] = UserRef{UserID: userID, TreeHash: hash}
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

// FetchCaryaRefs fetches all carya refs from a remote.
func (r *RefManager) FetchCaryaRefs(remote string) error {
	// Verify the remote exists before attempting to fetch
	checkCmd := exec.Command("git", "remote", "get-url", remote)
	checkCmd.Dir = r.repoPath
	if _, err := checkCmd.Output(); err != nil {
		log.Printf("Remote '%s' not found", remote)
		return fmt.Errorf("remote '%s' not found", remote)
	}

	cmd := exec.Command("git", "fetch", remote, "+refs/carya/*:refs/remotes/"+remote+"/carya/*")
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

// PushUserRef pushes a user's tree ref to a remote.
func (r *RefManager) PushUserRef(remote, userID string) error {
	// Get the user's current tree hash
	treeHash, err := r.getLocalUserTreeRef(userID)
	if err != nil {
		return fmt.Errorf("failed to get user tree ref: %w", err)
	}

	// Create a commit object pointing to the tree so it can be pushed
	commitCmd := exec.Command("git", "commit-tree", treeHash, "-m", "state snapshot")
	commitCmd.Dir = r.repoPath
	commitCmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=carya",
		"GIT_AUTHOR_EMAIL=carya@internal",
		"GIT_COMMITTER_NAME=carya",
		"GIT_COMMITTER_EMAIL=carya@internal",
	)
	out, err := commitCmd.Output()
	if err != nil {
		log.Printf("Failed to create commit for tree %s: %v", treeHash, err)
		return fmt.Errorf("failed to create commit for tree: %w", err)
	}
	commitSHA := strings.TrimSpace(string(out))

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
