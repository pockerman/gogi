# Contributing to gogi[AI]

Thank you for your interest in contributing to gogi. This document outlines the process for contributing to the project.

## Table of Contents

- [Getting Started](#getting-started)
- [Branching Strategy](#branching-strategy)
- [Commit Messages](#commit-messages)
- [Pull Requests](#pull-requests)
- [Code Style](#code-style)
- [Running Tests](#running-tests)

## Getting Started

1. Fork the repository and clone it locally.
2. Follow the installation steps in [README.md](./README.md) to set up your local environment.
3. Create a new branch from `main` for your changes (see [Branching Strategy](#branching-strategy)).

## Branching Strategy

Create branches from `main` using a descriptive name that reflects the work being done:

```
<type>/<short-description>
```

Examples:

```
feat/add-model-service-endpoint
fix/session-timeout-handling
docs/update-contributing-guide
```

## Commit Messages

This project follows the [Conventional Commits](https://www.conventionalcommits.org/) specification. Every commit message must have the following structure:

```
<type>(<optional scope>): <description>

[optional body]

[optional footer(s)]
```

### Types

| Type | When to use |
|------|-------------|
| `feat` | A new feature |
| `fix` | A bug fix |
| `docs` | Documentation changes only |
| `style` | Formatting changes that do not affect logic |
| `refactor` | Code change that is neither a bug fix nor a new feature |
| `test` | Adding or updating tests |
| `ci` | Changes to CI/CD configuration |
| `chore` | Maintenance tasks (dependency updates, build scripts, etc.) |
| `perf` | Performance improvements |

### Examples

```
feat(model-service): add support for streaming responses

fix(session): resolve race condition on concurrent requests

docs: update kubernetes deployment instructions

ci: add gofmt formatting step to build workflow
```

### Breaking Changes

Breaking changes must include `BREAKING CHANGE:` in the commit footer, or append `!` after the type:

```
feat!: remove deprecated v1 API endpoints

BREAKING CHANGE: The /v1/models endpoint has been removed. Use /v2/models instead.
```

## Pull Requests

- PR titles must follow the Conventional Commits format (enforced by CI).
- Keep PRs focused on a single concern. Avoid bundling unrelated changes.
- Ensure all CI checks pass before requesting a review.
- Link any relevant issues in the PR description.

## Code Style

Go code is automatically formatted using `gofmt` as part of the CI pipeline. Before pushing, you can format your code locally with:

```
gofmt -w .
```

## Running Tests

```
go test ./...
```

Ensure all tests pass before opening a pull request.
