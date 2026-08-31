---
name: add-ynd-command
description: Add a new command to the ynd developer CLI end to end — dispatch, flag parsing, output formats, tests and docs. Use when adding a ynd subcommand, the sibling of add-vendor-adapter for the other half of the product.
---

# Add a `ynd` command

`ynd` is half the product — 13 commands against `ynh`'s 31 — and it had no
authoring guide while `add-vendor-adapter` covered the other half.

Work the checklist in order. Each step is a place a command has actually been
shipped incomplete.

## Before you start

Read an existing command of the same shape rather than starting from a blank
file. `cmd/ynd/compose.go` is the best model: it parses flags by hand, resolves
its source from flag then env then positional, and supports `--format
text|json` with a split renderer.

## 1. Decide the shape

| Shape | Model to copy | Notes |
|---|---|---|
| Reads a harness, prints a report | `compose.go` | source resolution + `--format` |
| Reads a harness, writes files | `preview.go`, `export.go` | needs `-o` and a destination guard |
| Operates on loose files | `lint.go`, `fmt.go` | takes multiple positional paths |
| Interactive | `inspect.go`, `compress.go` | needs prompt indirection — see step 6 |

**Destructive commands have their own rules.** If the command deletes or
overwrites anything, read `.claude/rules/destructive-operations.md` first. It is
written from an incident and it is not optional.

## 2. Dispatch

`cmd/ynd/main.go` — add a `case` to the switch:

```go
case "mycommand":
    err = cmdMyCommand(os.Args[2:])
```

Keep it alphabetical only if the neighbours are; match the file, do not
reorganise it in the same change.

## 3. The command function, in two parts

Split it so the output is testable without capturing process stdout:

```go
func cmdMyCommand(args []string) error {
    return cmdMyCommandTo(args, os.Stdout, os.Stderr)
}

func cmdMyCommandTo(args []string, stdout, stderr io.Writer) error {
    // ...
}
```

Every command in `cmd/ynd/` that has tests worth having does this. It is the
difference between a test that asserts on a buffer and one that cannot assert at
all.

## 4. Flag parsing

Hand-rolled, no framework — the project has zero external dependencies and that
is deliberate.

```go
for i := 0; i < len(args); i++ {
    switch args[i] {
    case "--format":
        if i+1 >= len(args) {
            return fmt.Errorf("--format requires a value")
        }
        i++
        format = args[i]
    case "-h", "--help":
        return errHelp
    default:
        if strings.HasPrefix(args[i], "-") {
            return fmt.Errorf("unknown flag: %s", args[i])
        }
        source = args[i]
    }
}
```

Three things that are easy to get wrong:

- **Return `errHelp`, do not print.** `main.go` catches it and prints usage.
- **Reject unknown flags.** Silently ignoring one means a typo'd flag does
  nothing and the user never learns why.
- **Collect every positional you accept.** `ynd lint` shipped taking only the
  first and discarding the rest silently — green output over unchecked files
  (ynh#251, fixed in #261). If you take one path, error on a second rather than
  dropping it.

## 5. Source resolution, if it takes a harness

The established order is **flag > `YNH_HARNESS` env > positional > cwd**:

```go
if source == "" {
    source = resolveHarnessEnv()
}
if source == "" {
    source = "."
}
```

Use `resolveHarnessEnv()` rather than reading the variable yourself, so the
precedence stays in one place.

## 6. Interactive commands need indirection

If it prompts or calls an LLM, route through a replaceable function variable so
tests can drive it:

- `queryLLMFunc` — `cmd/ynd/llm.go`
- `promptActionFunc` / `promptInputFunc` — `cmd/ynd/inspect.go`

A command that calls `fmt.Scanln` directly cannot be tested and will not be.

Also honour the skip-confirm chain: `-y` / `--yes` flag, then `YNH_YES`, then
`CI`. Matching `compress` and `inspect` means one mental model, not three.

**A destructive prompt must default to refusing.** `promptAction` returns
`choices[0]` on empty input *and on EOF*, so `promptAction("Delete? [y/N] ",
"n", "y")` is correct and reversing the arguments deletes whenever stdin is a
pipe.

## 7. Output

If it supports `--format`:

```go
func printMyCommandText(w io.Writer, out myOutput) error { ... }
func printMyCommandJSON(w io.Writer, out myOutput) error { ... }
```

Validate the value and name the alternatives:

```go
return fmt.Errorf("invalid --format value %q (want text or json)", format)
```

**Schemas are currently a `ynh` concept.** `docs/schema/cli/` holds schemas for
`ynh` commands, and `ynd validate-output` checks against those. If a new `ynd`
command emits structured JSON that anything else consumes, raise whether it
needs one rather than assuming it does not — the gap is real, not a decision.

## 8. Help text

Add the command to `printUsage()` in `cmd/ynd/main.go`, in both the command
list and the examples block if it takes non-obvious arguments.

Then add an entry to `commandHelp` in `cmd/ynd/help.go`, keyed by the command
name, opening with a line that names the command. Two tests fail if you skip
it: `TestEveryDispatchedCommandHasHelp` and
`TestEveryDispatchedCommandAppearsInUsage`.

Do not handle `-h`/`--help` inside your command. `main` intercepts it before
dispatch, which is why asking for documentation cannot run an action.

## 9. Tests

Standard library only, no frameworks.

```go
func TestCmdMyCommandJSONBasic(t *testing.T) {
    dir := t.TempDir()
    var stdout bytes.Buffer
    if err := cmdMyCommandTo([]string{dir, "--format", "json"}, &stdout, io.Discard); err != nil {
        t.Fatalf("unexpected error: %v", err)
    }
    // assert on stdout.String()
}
```

Cover, at minimum, what the existing suites cover:

- happy path in each `--format`
- no arguments
- unknown flag
- invalid `--format` value
- empty collections — `compose_test.go` has a dedicated case, because an empty
  array serialising as `null` is a wire-format break

Use `t.TempDir()` for isolation and `t.Setenv()` for anything environmental.
`errcheck` is strict: handle every returned error, including in tests.

## 10. Docs

- `docs/ynd.md` — the command reference
- `.claude/CLAUDE.md` — the command list at the top names every `ynd` command
- A tutorial under `docs/tutorial/` if it introduces a concept rather than a
  variation. **Touching `docs/tutorial/` triggers the evals gate.**

## 11. Gates

```bash
make check          # includes check-artifacts
make test FILE=./cmd/ynd
```

`/evals` is required if you touched `cmd/`, `internal/` or `docs/tutorial/` —
which, for a new command, you did.

## The check nobody remembers

Run the command with no arguments, with `--help`, and with a deliberately wrong
flag. All three should say something useful and change nothing.

`ynh prune --help` used to run `prune`. That class of bug is only ever found by
trying it.
