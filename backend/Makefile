.PHONY: test test-unit test-integration test-coverage test-e2e test-all

# Run all backend tests (unit + integration).
test:
	MARKUPMARKDOWN_ENV=test go test -count=1 -v ./...

# Run only short tests (unit tests that don't need a DB).
test-unit:
	MARKUPMARKDOWN_ENV=test go test -short -count=1 ./...

# Run only integration tests (require a test DB; pattern "Integration").
test-integration:
	MARKUPMARKDOWN_ENV=test go test -count=1 -v -run Integration ./...

# Generate coverage report. We exclude packages that are pure plumbing
# (cmd, testutil, embed) to keep the headline number meaningful.
test-coverage:
	MARKUPMARKDOWN_ENV=test go test -count=1 \
		-coverprofile=coverage.out \
		-covermode=atomic \
		-timeout=600s ./...
	@go tool cover -func=coverage.out | tail -1
	@go tool cover -html=coverage.out -o coverage.html
	@echo "Coverage report: backend/coverage.html"

# Run Playwright E2E tests (requires backend + frontend dev servers).
test-e2e:
	cd ../frontend && npx playwright test

# Run everything.
test-all: test test-e2e
