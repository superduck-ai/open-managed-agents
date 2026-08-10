---
title: "Testing"
summary: "Run tests locally with Go and Bun, and execute end-to-end SDK tests against a running server."
topics: [testing, development]
sources:
  - id: agents-md
    type: file
    path: AGENTS.md
  - id: tests-dir
    type: file
    path: tests/
  - id: justfile
    type: file
    path: justfile
  - id: files-api-test
    type: file
    path: tests/files_api_test.go
  - id: skills-api-test
    type: file
    path: tests/skills_api_test.go
  - id: web-test
    type: file
    path: tests/js-test/README.md
---

Testing in Open Managed Agents covers unit tests, integration tests, and end-to-end SDK compatibility tests. The project uses Go's standard testing framework for backend code and Bun for frontend tests [@justfile].

## Running Go Tests

Run all Go tests with a single command [@justfile]:

```bash
go test ./... -count=1
```

The `-count=1` flag disables test caching, ensuring tests run fresh each time.

Individual test packages can be targeted:

```bash
go test ./tests -run TestFilesAPI -count=1
```

Test organization follows the pattern of writing failure scenarios before success scenarios, ensuring error handling is robust before validating happy paths [@agents-md].

## Test Structure

Tests reside in `tests/` at the repository root, organized by API resource [@tests-dir]:
- `files_api_test.go`: File upload and retrieval tests [@files-api-test]
- `skills_api_test.go`: Skill package and version tests [@skills-api-test]
- `sessions_api_test.go`: Session management tests
- `agents_api_test.go`: Agent search tests

Tests use the `testApp` helper to create a fresh application context with an isolated database connection for each test run [@files-api-test].

## Frontend Tests

Frontend tests use Bun and are located in `tests/js-test/` [@web-test].

Run all frontend tests [@justfile]:

```bash
bun test
```

## End-to-End Testing with Real SDKs

E2E tests verify compatibility with official Anthropic SDKs by running tests from the SDK repositories against a local server [@agents-md].

First, start the local server:

```bash
ADDR=127.0.0.1:18080 go run .
```

Then run SDK tests with the local base URL:

```bash
TEST_API_BASE_URL=http://127.0.0.1:18080 go test ./tests -run TestGoSDKFilesE2E -count=1 -v
```

Python SDK tests run from the official Python SDK virtualenv:

```bash
.venv/bin/pytest tests/api_resources/beta/test_files.py -q
```

TypeScript SDK tests run via Jest:

```bash
./node_modules/.bin/jest tests/api-resources/beta/files.test.ts --runInBand
```

Stop the local server after E2E testing completes [@agents-md].

## Test Coverage and CI

All tests must pass before merging code. The project runs tests automatically in CI for each pull request. When modifying database schema or handlers, run the full test suite locally first to catch regressions early [@agents-md].

SDK E2E tests provide the strongest guarantee that the API remains compatible with Anthropic's official clients, making them essential for any changes to request/response formats or HTTP behavior.
