# Carya Development Guidelines

## Build Commands
- Build: `go build -o carya ./cmd/carya`
- Run: `./carya`
- Format code: `go fmt ./...`
- Lint: `go vet ./...`
- Test all: `go test ./...`
- Test single package: `go test carya/internal/chunk`
- Test single function: `go test carya/internal/chunk -run TestFunctionName`
- Test with verbose output: `go test -v ./...`
- Test with coverage: `go test -cover ./...`

## Code Style Guidelines
- **Imports**: Group standard library, external packages, and internal packages
- **Formatting**: Follow Go standard formatting with `go fmt`
- **Error Handling**: Always check errors and provide context
- **Naming**:
  - Use CamelCase for exported names, camelCase for unexported
  - Prefer descriptive names over abbreviations
  - Interface names should end with 'er' (e.g., Manager, Watcher)
- **Package Structure**: Keep packages focused on a single responsibility
- **Comments**: Document exported functions, types, and packages
- **Dependencies**: Use dependency injection for testability
- **Testing**: Write tests for all exported functions

## Project Structure
- `cmd/`: Application entry points
- `internal/`: Private application code
- `.carya/`: Application data directory



lorem ipsim
