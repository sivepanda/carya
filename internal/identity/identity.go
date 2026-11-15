// Package identity manages user identity for Carya's team collaboration features.
package identity

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"

	"carya/internal/config"
	"carya/internal/git"

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

	username := defaultUserIDSource()
	if username == "" {
		username = uuid.New().String()[:8]
	}

	identityText := buildDisplayName(username, configuredOrDefaultDeviceID(username))
	userID, err := u.generateUniqueHash(identityText)
	if err != nil {
		log.Printf("Failed to generate user ID hash: %v", err)
		return "", fmt.Errorf("failed to generate user ID hash: %w", err)
	}

	// Save the ID
	if err := u.Save(userID); err != nil {
		log.Printf("Failed to save user ID: %v", err)
		return "", fmt.Errorf("failed to save user ID: %w", err)
	}

	return userID, nil
}

// DefaultDeviceID returns a default device identifier in <user>@<device> form.
func DefaultDeviceID() string {
	username := defaultUserIDSource()
	if username == "" {
		username = uuid.New().String()[:8]
	}
	return configuredOrDefaultDeviceID(username)
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

func defaultUserIDSource() string {
	if name := gitUserName(); name != "" {
		return sanitizeUserID(name)
	}

	if username := os.Getenv("USER"); username != "" {
		return sanitizeUserID(username)
	}

	if username := os.Getenv("USERNAME"); username != "" {
		return sanitizeUserID(username)
	}

	return ""
}

func gitUserName() string {
	cmd := exec.Command("git", "config", "user.name")
	output, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(output))
}

func configuredOrDefaultDeviceID(userID string) string {
	cfg, err := config.LoadGlobalConfig()
	if err == nil {
		if configured := strings.TrimSpace(cfg.DeviceID); configured != "" {
			return configured
		}
	}

	host, err := os.Hostname()
	if err != nil {
		host = "device"
	}

	host = sanitizeUserID(host)
	if host == "" {
		host = "device"
	}

	return fmt.Sprintf("%s@%s", userID, host)
}

func buildDisplayName(userID, deviceID string) string {
	return fmt.Sprintf("%s (%s)", userID, deviceID)
}

func hashDisplayName(displayName string) string {
	sum := sha256.Sum256([]byte(displayName))
	return hex.EncodeToString(sum[:])[:16]
}

func (u *UserIdentity) generateUniqueHash(identityText string) (string, error) {
	base := hashDisplayName(identityText)
	if !u.userHashExists(base) {
		return base, nil
	}

	for i := 0; i < 12; i++ {
		salt, err := randomHex(4)
		if err != nil {
			return "", err
		}
		candidate := hashDisplayName(identityText + "#" + salt)
		if !u.userHashExists(candidate) {
			return candidate, nil
		}
	}

	return "", fmt.Errorf("could not create a collision-free user hash")
}

func (u *UserIdentity) userHashExists(userHash string) bool {
	if userHash == "" {
		return false
	}

	localRef := git.UserTreeRefPath(userHash)
	remoteRef := git.RemoteUserTreeRefPath("origin", userHash)

	return u.refExists(localRef) || u.refExists(remoteRef)
}

func (u *UserIdentity) refExists(ref string) bool {
	cmd := exec.Command("git", "show-ref", "--verify", "--quiet", ref)
	cmd.Dir = filepath.Dir(filepath.Dir(u.idPath))
	return cmd.Run() == nil
}

func randomHex(byteLen int) (string, error) {
	b := make([]byte, byteLen)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
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

	if len(id) > 64 {
		log.Printf("User ID too long (max 64 characters)")
		return fmt.Errorf("user ID too long (max 64 characters)")
	}

	hashed := regexp.MustCompile(`^[a-f0-9]{16}$`)
	if hashed.MatchString(id) {
		return nil
	}

	// Must match: lowercase alphanumeric with hyphens and underscores
	valid := regexp.MustCompile(`^[a-z0-9][a-z0-9\-_]*[a-z0-9]$|^[a-z0-9]$`)
	if !valid.MatchString(id) {
		log.Printf("User ID must be lowercase alphanumeric with optional hyphens/underscores")
		return fmt.Errorf("user ID must be lowercase alphanumeric with optional hyphens/underscores")
	}

	return nil
}
