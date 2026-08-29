# Destructive Operations

Code that deletes or overwrites — `os.RemoveAll`, file overwrite, `rm`, a
force-push, a DB drop — is held to a different standard than code that reads.

## Never mutation-test a destructive function in place

**On 2026-08-29 this destroyed a developer's home directory.**

`ynd export --clean` and `ynd marketplace --clean` called `os.RemoveAll` on the
path given to `-o`, with no prompt and no guard. A guard (`refuseToClean`) was
written, plus tests that pass it the filesystem root, `$HOME`, the working
directory and its parent with confirmation disabled — safe only while the guard
exists, because the test asserts the deletion never happens.

The guard was then deliberately broken to prove the test bites, and the test
suite re-run. `os.RemoveAll` executed against those real paths. Lost: the shell
config, the login keychain, the Desktop, the agent memory directory, an entire
working copy.

The house rule "a check that finds nothing is not a check that found nothing
wrong" — break every new guard once to prove it bites — **does not apply to
destructive code**. That is the one exception, and it is not optional.

## Structure destructive code so the dangerous paths never reach the delete

- The decision is a **pure predicate**: `refuseToClean(path) string` returns why
  a path must not be deleted, and touches nothing. Dangerous paths are only ever
  handed to the function that *decides*, never to the one that *deletes*.
- The deleting function is called in tests **only** with paths under
  `t.TempDir()`. Use `t.Chdir()` into a temp directory to fake "the current
  directory" rather than passing the real one.
- Test helpers should hard-assert that any path reaching the deleting function
  is inside the test's temp directory.

Before any mutation run, ask: *can the code under test delete or overwrite
anything?* If yes, reason about the guard or mutate against a fake root. Never
in place.

## Two layers on any destructive flag

- **Hard refusal** for paths that are never a legitimate target — filesystem
  root, `$HOME`, the current directory, any ancestor of it, a git working copy.
  These ignore `-y`: skipping a question is not a licence to delete a home
  directory.
- **Confirmation** for anything else that exists and is non-empty, skippable via
  `-y` / `YNH_YES` / `CI`, matching `ynd compress` and `ynd inspect`.

## Order the prompt so the safe answer is the default

`promptAction` returns `choices[0]` on empty input **or EOF**. A destructive
prompt must therefore list the refusing answer first:

```go
promptAction("Delete it? [y/N] ", "n", "y")   // correct
promptAction("Delete it? [y/N] ", "y", "n")   // deletes whenever stdin is a pipe
```

A prompt labelled `[y/N]` that returns `y` on EOF is a lie to the operator.
