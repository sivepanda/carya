# CRUSH.md - Carya Project Guidelines

## Build & Test Commands
- Build: `go build ./cmd/carya`
- Run: `go run ./cmd/carya/main.go`
- Test: `go test ./...`
- Test single package: `go test ./internal/housekeeping`
- Test single function: `go test -run TestFunctionName ./internal/housekeeping`
- Lint: `golangci-lint run`
- Format: `gofmt -s -w .`

## Code Style Guidelines
- **Imports**: Group standard library, third-party, and internal imports with a blank line between groups
- **Formatting**: Use `gofmt` for consistent formatting
- **Error Handling**: Always check errors and return them with context using `fmt.Errorf("context: %w", err)`
- **Naming**:
  - Packages: Short, lowercase, no underscores
  - Functions/Variables: camelCase for unexported, PascalCase for exported
  - Constants: PascalCase
  - Interfaces: PascalCase, often ending with 'er' (e.g., Manager)
- **Comments**: All exported functions must have comments in godoc format
- **File Organization**: Group related functionality in packages under internal/
- **Testing**: Write tests for all public functions

## Project Structure
- cmd/: Application entry points
- internal/: Private application code
- .carya/: Project-specific configuration