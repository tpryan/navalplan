name: integrated-test-eval
description: Triggered when writing backend unit tests, testing frontend features, configuring Playwright E2E validation, or writing ADK agent evaluation suites.

## Goal
Validate backend components, UI paths, and AI agent performance using mock-driven, environment-isolated tests and ADK metrics.

## Instructions
1. **Mocking & Isolation:** Write table-driven tests in Go. Isolate backend unit tests by injecting datastore interfaces paired with mock utilities like `pgxmock` to eliminate side effects.
2. **Frontend Testing:** Validate vanilla JS single-page app behavior using jasmine or Vitest.
3. **E2E Validation:** Ensure functional application flow correctness using Playwright.
4. **Agent Evaluation:** Evaluate AI service metrics using the `google-adk` CLI toolbelt triggered via `make eval-*` targets.
5. **Gomock Isolation:** When using `gomock` with `t.Run`, always create a new controller and mock instance inside each subtest to prevent expectation pollution between tests.

## Constraints
* **No Live DB in Unit Tests:** Never connect to a live database during unit tests. Use mocks to ensure tests are deterministic.
