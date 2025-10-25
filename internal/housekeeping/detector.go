package housekeeping

import (
	"os"
	"path/filepath"
)

// PackageType represents a detected package manager or build system
type PackageType struct {
	Name        string
	DetectFile  string
	Description string
}

// DetectedPackage contains information about a detected package system
type DetectedPackage struct {
	Type PackageType
	Path string
}

// Common package types and their detection files
var PackageTypes = []PackageType{
	{Name: "npm", DetectFile: "package.json", Description: "Node.js (npm)"},
	{Name: "yarn", DetectFile: "yarn.lock", Description: "Node.js (Yarn)"},
	{Name: "pnpm", DetectFile: "pnpm-lock.yaml", Description: "Node.js (pnpm)"},
	{Name: "go", DetectFile: "go.mod", Description: "Go Modules"},
	{Name: "python-pip", DetectFile: "requirements.txt", Description: "Python (pip)"},
	{Name: "python-poetry", DetectFile: "pyproject.toml", Description: "Python (Poetry)"},
	{Name: "python-pipenv", DetectFile: "Pipfile", Description: "Python (Pipenv)"},
	{Name: "rust", DetectFile: "Cargo.toml", Description: "Rust (Cargo)"},
	{Name: "ruby", DetectFile: "Gemfile", Description: "Ruby (Bundler)"},
	{Name: "php-composer", DetectFile: "composer.json", Description: "PHP (Composer)"},
	{Name: "java-maven", DetectFile: "pom.xml", Description: "Java (Maven)"},
	{Name: "java-gradle", DetectFile: "build.gradle", Description: "Java/Kotlin (Gradle)"},
	{Name: "dotnet", DetectFile: "*.csproj", Description: ".NET"},
	{Name: "elixir", DetectFile: "mix.exs", Description: "Elixir (Mix)"},
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
	templates := map[string]map[string][]Command{
		"npm": {
			"post-pull": {
				{Command: "npm install", WorkingDir: ".", Description: "Install npm dependencies"},
			},
			"post-checkout": {
				{Command: "npm install", WorkingDir: ".", Description: "Install npm dependencies"},
			},
		},
		"yarn": {
			"post-pull": {
				{Command: "yarn install", WorkingDir: ".", Description: "Install Yarn dependencies"},
			},
			"post-checkout": {
				{Command: "yarn install", WorkingDir: ".", Description: "Install Yarn dependencies"},
			},
		},
		"pnpm": {
			"post-pull": {
				{Command: "pnpm install", WorkingDir: ".", Description: "Install pnpm dependencies"},
			},
			"post-checkout": {
				{Command: "pnpm install", WorkingDir: ".", Description: "Install pnpm dependencies"},
			},
		},
		"go": {
			"post-pull": {
				{Command: "go mod download", WorkingDir: ".", Description: "Download Go dependencies"},
				{Command: "go mod tidy", WorkingDir: ".", Description: "Clean up Go dependencies"},
			},
			"post-checkout": {
				{Command: "go mod download", WorkingDir: ".", Description: "Download Go dependencies"},
			},
		},
		"python-pip": {
			"post-pull": {
				{Command: "pip install -r requirements.txt", WorkingDir: ".", Description: "Install Python dependencies"},
			},
			"post-checkout": {
				{Command: "pip install -r requirements.txt", WorkingDir: ".", Description: "Install Python dependencies"},
			},
		},
		"python-poetry": {
			"post-pull": {
				{Command: "poetry install", WorkingDir: ".", Description: "Install Poetry dependencies"},
			},
			"post-checkout": {
				{Command: "poetry install", WorkingDir: ".", Description: "Install Poetry dependencies"},
			},
		},
		"python-pipenv": {
			"post-pull": {
				{Command: "pipenv install", WorkingDir: ".", Description: "Install Pipenv dependencies"},
			},
			"post-checkout": {
				{Command: "pipenv install", WorkingDir: ".", Description: "Install Pipenv dependencies"},
			},
		},
		"rust": {
			"post-pull": {
				{Command: "cargo build", WorkingDir: ".", Description: "Build Rust project"},
			},
			"post-checkout": {
				{Command: "cargo build", WorkingDir: ".", Description: "Build Rust project"},
			},
		},
		"ruby": {
			"post-pull": {
				{Command: "bundle install", WorkingDir: ".", Description: "Install Ruby gems"},
			},
			"post-checkout": {
				{Command: "bundle install", WorkingDir: ".", Description: "Install Ruby gems"},
			},
		},
		"php-composer": {
			"post-pull": {
				{Command: "composer install", WorkingDir: ".", Description: "Install Composer dependencies"},
			},
			"post-checkout": {
				{Command: "composer install", WorkingDir: ".", Description: "Install Composer dependencies"},
			},
		},
		"java-maven": {
			"post-pull": {
				{Command: "mvn clean install", WorkingDir: ".", Description: "Build Maven project"},
			},
			"post-checkout": {
				{Command: "mvn clean install", WorkingDir: ".", Description: "Build Maven project"},
			},
		},
		"java-gradle": {
			"post-pull": {
				{Command: "./gradlew build", WorkingDir: ".", Description: "Build Gradle project"},
			},
			"post-checkout": {
				{Command: "./gradlew build", WorkingDir: ".", Description: "Build Gradle project"},
			},
		},
		"dotnet": {
			"post-pull": {
				{Command: "dotnet restore", WorkingDir: ".", Description: "Restore .NET dependencies"},
			},
			"post-checkout": {
				{Command: "dotnet restore", WorkingDir: ".", Description: "Restore .NET dependencies"},
			},
		},
		"elixir": {
			"post-pull": {
				{Command: "mix deps.get", WorkingDir: ".", Description: "Get Elixir dependencies"},
			},
			"post-checkout": {
				{Command: "mix deps.get", WorkingDir: ".", Description: "Get Elixir dependencies"},
			},
		},
	}

	if categoryCommands, exists := templates[pkgName]; exists {
		if commands, exists := categoryCommands[category]; exists {
			return commands
		}
	}

	return []Command{}
}
