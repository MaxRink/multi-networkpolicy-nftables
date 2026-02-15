# Contributing to Multi-NetworkPolicy nftables

Thank you for your interest in contributing to Multi-NetworkPolicy nftables! This document provides guidelines and contribution requirements.

## Code of Conduct

This project adheres to the [Contributor Covenant Code of Conduct](CODE_OF_CONDUCT.md). By participating, you are expected to uphold this code.

## Contribution Requirements

### 1. Coding Standards

- **Go**: Use standard Go formatting (gofmt/goimports) and follow Go idioms
- Use standard library constants (e.g., `http.MethodGet` instead of string literals)
- See [.golangci.yaml](.golangci.yaml) for Go linting rules

### 2. Testing Policy (Required)

All new features and significant changes **must include automated tests**:

- Add `*_test.go` files colocated with source code
- Cover success cases, error cases, and edge cases
- Aim for >70% code coverage for new code
- Run `make test` before opening PRs

If testing is impractical, document why in the PR description.

### 3. Documentation Updates (Required)

Documentation must be updated for every user-facing change:

- **Configuration**: Update relevant documentation
- Update README.md if applicable

### 4. Quality Standards

- Maintain or improve test coverage (monitored via CI)
- Run linters locally: `make lint`
- Fix all linter errors before submitting

### 5. Security and Privacy

- Never commit secrets, credentials, or PII
- Report security issues privately per [SECURITY.md](.github/SECURITY.md)
- Consider security implications in PR descriptions

## Workflow

### 1. Find or Create an Issue

- Check existing issues to avoid duplicates
- For features, describe the use case, alternatives, and security impact
- Link your PR to the issue when ready

### 2. Develop Your Changes

```bash
# Create a feature branch
git checkout -b feature/your-feature-name

# Make changes
go build ./...
```

### 3. Test Locally

```bash
# Run tests and linting
make test
make lint
```

### 4. Open a Pull Request

- Provide a clear description and link related issues
- Note test coverage and any limitations
- Describe security implications if relevant

## Code Review Requirements

All changes require pull request review before merge:

- ✅ At least one approving review
- ✅ All CI checks passing (tests, linting, security scans)
- ✅ Up-to-date with base branch
- ✅ No direct pushes to main branch
- ✅ Stale approvals dismissed on new commits

Any exceptions must be documented in the PR with justification.

## Development Setup

### Prerequisites

- Go 1.25 or later
- Docker (for building images)
- Linux with nftables support (for testing)
- make (for build automation)

### Running Locally

```bash
# Run tests
make test

# Build the binary
make build
```

## Commit Messages

Follow conventional commit format:

```
type(scope): short description

Longer description if needed.

Fixes #123
```

Types: `feat`, `fix`, `docs`, `style`, `refactor`, `test`, `chore`, `ci`

## Questions or Help

Open an issue or discussion with context. See [SECURITY.md](.github/SECURITY.md) for reporting security vulnerabilities.

Thank you for contributing! 🎉
