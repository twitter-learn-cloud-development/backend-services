---
name: efficient-development
description: Delegate noisy or multi-command verification to the project test_runner. Use for long logs, broad test suites, race/static checks, or several independent checks. Do not use for docs-only or trivial changes.
---
1. The main agent owns analysis and implementation.
2. Run verification directly when it needs at most two short targeted commands.
3. Delegate to `test_runner` only when verification is noisy, long-running, or spans multiple independent checks.
4. Pass only:
   - changed files or packages;
   - required checks;
   - known environment constraints.
5. Request only commands, pass/fail summary, first actionable error, file/line, failure class, and suspected cause.
6. Allow at most one delegated rerun after a fix. Return control to the main agent after repeated failure.
