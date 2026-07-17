name: git-workflow-guardrail
description: Active safety gatekeeper triggered whenever evaluating workspace state, generating git commit recommendations, or running long processes.

## Goal
Enforce write boundaries on internal repository histories and ensure compliance with team lifecycle signaling conventions.

## Instructions
1. **Read-Only Inspection:** Use git commands exclusively to look at the codebase (`git status`, `git log`, `git diff`, `git show`, `git ls-files`).
2. **Commit Proposal:** Format all proposed repository modifications strictly following the Conventional Commits framework (`feat:`, `fix:`, `refactor:`, `chore:`, `test:`, `docs:`). Present these to the user as clear options for manual execution.
3. **Execution Heartbeat:** Track long-lived asynchronous evaluations. If a workspace analytical compilation or system validation pipeline takes longer than 5 minutes to conclude, invoke the completion tracker script `./done.sh` immediately.

## Constraints
* **Strict State Protection:** Never invoke history-altering mutations or staging operations (`git add`, `git commit`, `git checkout`, `git reset`, `git push`).
