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
	DetectFiles []string                     `json:"detectFiles,omitempty"` // Multiple files, all must exist
	Excludes    []string                     `json:"excludes,omitempty"`    // Package managers to exclude when this is detected
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
		// Check for multiple required files (all must exist)
		if len(pkgType.DetectFiles) > 0 {
			allExist := true
			var firstPath string
			for i, file := range pkgType.DetectFiles {
				filePath := filepath.Join(d.rootDir, file)
				if _, err := os.Stat(filePath); err != nil {
					allExist = false
					break
				}
				if i == 0 {
					firstPath = filePath
				}
			}
			if allExist {
				detected = append(detected, DetectedPackage{
					Type: pkgType,
					Path: firstPath,
				})
			}
			continue
		}

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

	// Apply exclusions
	detected = applyExclusions(detected)

	return detected, nil
}

// applyExclusions filters out packages based on exclusion rules
func applyExclusions(detected []DetectedPackage) []DetectedPackage {
	// Build a set of all detected package names
	detectedNames := make(map[string]bool)
	for _, pkg := range detected {
		detectedNames[pkg.Type.Name] = true
	}

	// Collect all packages that should be excluded
	excludedNames := make(map[string]bool)
	for _, pkg := range detected {
		for _, excludeName := range pkg.Type.Excludes {
			if detectedNames[excludeName] {
				excludedNames[excludeName] = true
			}
		}
	}

	// Filter out excluded packages
	var filtered []DetectedPackage
	for _, pkg := range detected {
		if !excludedNames[pkg.Type.Name] {
			filtered = append(filtered, pkg)
		}
	}

	return filtered
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
