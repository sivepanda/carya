// Package identity manages user identity for Carya's team collaboration features.
package identity

import (
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/google/uuid"
)

// UserIdentity manages the user's identity for carya operations.
type UserIdentity struct {
	idPath string // Path to .carya/user-id file
}

// NewUserIdentity creates a new UserIdentity manager.
func NewUserIdentity(caryaPath string) *UserIdentity {
	return &UserIdentity{
		idPath: filepath.Join(caryaPath, "user-id"),
	}
}

// GetOrCreate returns the user ID, creating one if it doesn't exist.
// Priority:
// 1. Existing user-id file
// 2. Git config user.name (sanitized)
// 3. System username (sanitized)
// 4. Generated UUID
func (u *UserIdentity) GetOrCreate() (string, error) {
	// Try existing file first
	if data, err := os.ReadFile(u.idPath); err == nil {
		id := strings.TrimSpace(string(data))
		if id != "" {
			return id, nil
		}
	}

	// Try to get a user ID from various sources
	var userID string

	// Try git config user.name
	if name := u.getGitUserName(); name != "" {
		userID = sanitizeUserID(name)
	}

	// Try system username
	if userID == "" {
		if username := os.Getenv("USER"); username != "" {
			userID = sanitizeUserID(username)
		} else if username := os.Getenv("USERNAME"); username != "" {
			userID = sanitizeUserID(username)
		}
	}

	// Fall back to UUID
	if userID == "" {
		userID = uuid.New().String()[:8]
	}

	// Save the ID
	if err := u.Save(userID); err != nil {
		log.Printf("Failed to save user ID: %v", err)
		return "", fmt.Errorf("failed to save user ID: %w", err)
	}

	return userID, nil
}

// Get returns the current user ID, or an error if not set.
func (u *UserIdentity) Get() (string, error) {
	data, err := os.ReadFile(u.idPath)
	if err != nil {
		if os.IsNotExist(err) {
			log.Printf("User ID not set, run 'carya init' first")
			return "", fmt.Errorf("user ID not set, run 'carya init' first")
		}
		log.Printf("Failed to read user ID: %v", err)
		return "", fmt.Errorf("failed to read user ID: %w", err)
	}

	id := strings.TrimSpace(string(data))
	if id == "" {
		log.Printf("User ID file is empty")
		return "", fmt.Errorf("user ID file is empty")
	}

	return id, nil
}

// Save writes the user ID to the file.
func (u *UserIdentity) Save(userID string) error {
	// Ensure parent directory exists
	if err := os.MkdirAll(filepath.Dir(u.idPath), 0755); err != nil {
		log.Printf("Failed to create directory: %v", err)
		return fmt.Errorf("failed to create directory: %w", err)
	}

	if err := os.WriteFile(u.idPath, []byte(userID+"\n"), 0644); err != nil {
		log.Printf("Failed to write user ID: %v", err)
		return fmt.Errorf("failed to write user ID: %w", err)
	}

	return nil
}

// Exists checks if a user ID has been set.
func (u *UserIdentity) Exists() bool {
	_, err := os.Stat(u.idPath)
	return err == nil
}

// getGitUserName attempts to get the user's name from git config.
func (u *UserIdentity) getGitUserName() string {
	cmd := exec.Command("git", "config", "user.name")
	output, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(output))
}

// sanitizeUserID converts a string to a valid user ID.
// Valid IDs are lowercase alphanumeric with hyphens and underscores.
func sanitizeUserID(name string) string {
	// Convert to lowercase
	name = strings.ToLower(name)

	// Replace spaces and special characters with hyphens
	name = strings.ReplaceAll(name, " ", "-")

	// Remove any characters that aren't alphanumeric, hyphen, or underscore
	reg := regexp.MustCompile(`[^a-z0-9\-_]`)
	name = reg.ReplaceAllString(name, "")

	// Remove consecutive hyphens
	for strings.Contains(name, "--") {
		name = strings.ReplaceAll(name, "--", "-")
	}

	// Trim leading/trailing hyphens
	name = strings.Trim(name, "-")

	// Truncate to reasonable length
	if len(name) > 32 {
		name = name[:32]
	}

	return name
}

// ValidateUserID checks if a user ID is valid.
func ValidateUserID(id string) error {
	if id == "" {
		log.Printf("User ID cannot be empty")
		return fmt.Errorf("user ID cannot be empty")
	}

	if len(id) > 32 {
		log.Printf("User ID too long (max 32 characters)")
		return fmt.Errorf("user ID too long (max 32 characters)")
	}

	// Must match: lowercase alphanumeric with hyphens and underscores
	valid := regexp.MustCompile(`^[a-z0-9][a-z0-9\-_]*[a-z0-9]$|^[a-z0-9]$`)
	if !valid.MatchString(id) {
		log.Printf("User ID must be lowercase alphanumeric with optional hyphens/underscores")
		return fmt.Errorf("user ID must be lowercase alphanumeric with optional hyphens/underscores")
	}

	return nil
}
