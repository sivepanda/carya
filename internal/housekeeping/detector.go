package housekeeping

import (
	_ "embed"
	"encoding/json"
	"os"
	"path/filepath"
)

//go:embed autodetect.json
var autodetectJSON []byte

// PackageType represents a detected package manager or build system
type PackageType struct {
	Name        string                       `json:"name"`
	DetectFile  string                       `json:"detectFile"`
	Description string                       `json:"description"`
	Commands    map[string][]Command         `json:"commands"`
}

// DetectedPackage contains information about a detected package system
type DetectedPackage struct {
	Type PackageType
	Path string
}

// loadPackageTypes loads package types from embedded JSON
func loadPackageTypes() ([]PackageType, error) {
	var types []PackageType
	if err := json.Unmarshal(autodetectJSON, &types); err != nil {
		return nil, err
	}
	return types, nil
}

// PackageTypes returns all configured package types
var PackageTypes []PackageType

func init() {
	var err error
	PackageTypes, err = loadPackageTypes()
	if err != nil {
		// Fall back to empty slice on error
		PackageTypes = []PackageType{}
	}
}

// Detector scans the project directory for package managers
type Detector struct {
	rootDir string
}

// NewDetector creates a new package detector
func NewDetector(rootDir string) *Detector {
	if rootDir == "" {
		rootDir = "."
	}
	return &Detector{rootDir: rootDir}
}

// DetectPackages scans the directory for package management files
func (d *Detector) DetectPackages() ([]DetectedPackage, error) {
	var detected []DetectedPackage

	for _, pkgType := range PackageTypes {
		// Handle glob patterns (like *.csproj)
		if filepath.Base(pkgType.DetectFile) != pkgType.DetectFile &&
		   (pkgType.DetectFile[0] == '*' || pkgType.DetectFile == "*.csproj") {
			matches, err := filepath.Glob(filepath.Join(d.rootDir, pkgType.DetectFile))
			if err == nil && len(matches) > 0 {
				detected = append(detected, DetectedPackage{
					Type: pkgType,
					Path: matches[0],
				})
			}
			continue
		}

		// Regular file detection
		filePath := filepath.Join(d.rootDir, pkgType.DetectFile)
		if _, err := os.Stat(filePath); err == nil {
			detected = append(detected, DetectedPackage{
				Type: pkgType,
				Path: filePath,
			})
		}
	}

	return detected, nil
}

// GetSuggestedCommands returns suggested housekeeping commands based on detected packages
func (d *Detector) GetSuggestedCommands(category string) ([]Command, error) {
	detected, err := d.DetectPackages()
	if err != nil {
		return nil, err
	}

	var suggestions []Command

	for _, pkg := range detected {
		commands := getCommandsForPackage(pkg.Type.Name, category)
		suggestions = append(suggestions, commands...)
	}

	return suggestions, nil
}

// getCommandsForPackage returns housekeeping commands for a specific package type
func getCommandsForPackage(pkgName, category string) []Command {
	// Find the package type by name
	for _, pkgType := range PackageTypes {
		if pkgType.Name == pkgName {
			if commands, exists := pkgType.Commands[category]; exists {
				return commands
			}
			break
		}
	}
	return []Command{}
}
