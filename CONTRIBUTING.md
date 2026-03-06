# Contributing to Forja

Thank you for your interest in contributing to Forja! This document provides guidelines for contributing to this project.

## Getting Started

1. Fork the repository
2. Clone your fork: `git clone https://github.com/<your-username>/forja.git`
3. Create a branch for your change: `git checkout -b my-feature`
4. Make your changes
5. Run tests: `go test ./...`
6. Commit your changes: `git commit -m "Add my feature"`
7. Push to your fork: `git push origin my-feature`
8. Open a Pull Request

## Development

### Prerequisites

- Go 1.21+

### Building

```bash
go build -o forja ./cmd/forja
```

### Running Tests

```bash
go test ./...
```

## Pull Requests

- Keep PRs focused on a single change
- Include tests for new functionality
- Ensure all tests pass before submitting
- Write clear commit messages

## Reporting Issues

Open an issue on GitHub with:
- A clear description of the problem
- Steps to reproduce
- Expected vs actual behavior
- Relevant logs or error messages

## License

By contributing, you agree that your contributions will be licensed under the [MIT License](LICENSE).
