# Agent Loop

`ynh agent run` drives a coding agent in a loop: it sends a task, runs the
harness's declared [sensors](sensors.md) between turns, feeds the results back,
and stops when the sensors agree the work is done or a budget is spent.

It is the one place ynh runs an agent rather than assembling a harness for one.
Everything it decides comes from declarations already in the manifest — sensors,
tolerances, focuses, profiles. Between turns it runs
[`ynh check`](sensors.md#ynh-check) itself rather than re-implementing it, so
the loop and a human at a terminal cannot reach different conclusions about the
same manifest.

> Sensors are what the loop iterates against. If a harness declares none, the
> loop has nothing to verify and cannot converge.

## Usage

```bash
ynh agent run --harness <id|path> --task "<what to do>" [flags]
ynh agent run --resume <session-dir> [flags]
```

## Flags

| Flag | Meaning |
|---|---|
| `--harness <id\|path>` | Harness whose sensors, artifacts and hooks drive the run. Takes an installed id (`local/demo`) or a path to a harness directory (`.`, `./my-harness`, an absolute path), exactly as `ynh check` does. A path needs no prior install, which matters while you are still authoring the harness |
| `--task "<text>"` | What the agent is being asked to do |
| `--focus <name>` | Use a declared focus for the task and its profile |
| `--profile <name>` | Apply a profile overlay |
| `--backend <name>` | Backend to drive: `claude`, `codex` or `cursor` (default: `claude`; on `--resume`, the session's own) |
| `--model <name>` | Model override passed to the backend |
| `--effort <low\|medium\|high>` | Reasoning effort, mapped to the backend's own setting; overrides the harness's `agent.effort`. See [Effort](#effort) |
| `--convergence-sensor <name>` | Sensor consulted once all blocking sensors pass |
| `--sensor-overlay <json>` | Per-run sensor overrides |
| `--worktree <dir>` | Directory the agent works in and sensors run against |
| `--format <text\|json>` | `json` prints the run result as one object when the run ends |
| `--sandbox <mode>` | Sandbox mode passed to the backend |
| `--auto-approve <edits\|all>` | Approve the worker's file edits, or everything, without prompting. Off by default. See [Permissions](#permissions-and-auto-approve) |
| `--auto-commit` | Commit after each converged turn |
| `--interactive` | Pause for approval at turn boundaries |
| `--no-plan` | Skip the plan phase and act immediately |
| `--max-turns <n>` | Cap on act-phase turns |
| `--max-tokens <n>` | Cap on total tokens |
| `--max-wall <dur>` | Wall-clock cap, e.g. `30m` |
| `--max-plan-iterations <n>` | Cap on plan revisions before acting |
| `--emit-jsonl <path>` | Write the trajectory; `-` for stdout |
| `--resume <dir>` | Continue a previous session from its directory (the `--emit-jsonl` file's folder) |
| `--telemetry-relay` | Start `ynr relay` for the run and send the vendor's own telemetry into the spool (claude only). Also `YNH_TELEMETRY_RELAY`, or `"telemetry_relay": true` in `config.json`. See [Telemetry](telemetry.md#vendor-telemetry-through-the-relay) |

### Budgets

Every run is bounded. Caps resolve in order — **flag, then harness manifest,
then built-in default** — so there is no unlimited state to fall into:

| Cap | Default |
|---|---|
| `--max-turns` | 25 |
| `--max-tokens` | 2,000,000 |
| `--max-wall` | 60m |

A harness can carry its own envelope rather than every caller passing flags:

```json
"agent": { "max_turns": 40, "max_tokens": 4000000, "max_wall": "90m" }
```

The `session_start` trajectory event records the caps in force **and where each
came from** (`flag`, `manifest`, `default`). Aggregating a batch of runs, a cap
nobody chose that fires is noise in the result and a chosen cap that fires is a
finding — they have to be told apart.

A cap is a cap on what the worker is asked to do, not only on what the loop
records. The worker is **never sent a turn beyond a cap**: when the turn, token
or wall-clock cap is spent, or the run is interrupted, the loop ends the run
*before* it sends the next turn's feedback, so the tree is as the last counted
turn left it and `feedback_sent` appears only for turns that were taken. The
feedback it would have sent is kept in `checkpoint.json` as the pending message,
so `--resume` with room left sends it. On any early exit the worker process is
stopped rather than waited for: a worker with a turn in flight gets SIGTERM
(then SIGKILL after a few seconds), so it cannot finish work the run has
already given up on.

## What the agent can see

The worker receives only the environment variables the harness declares in
`env_passthrough`, plus the process minimum (`PATH`, `HOME`, `USER`, `SHELL`,
`TMPDIR`, `TZ`, `LANG`, `TERM`, `LC_*`) — not the operator's environment.

Those few are process mechanics rather than configuration: without `PATH` the
vendor binary cannot be found and without `HOME` it cannot locate its own
credentials. Anything that is policy stays out, **proxy settings included** — a
proxy URL can carry credentials, and inheriting one silently is the same
default this removes. A harness behind a proxy declares it.

Every run emits a `worker_env` trajectory event naming what was passed, what
was declared, and which declared variables were **not set** — names only, never
values. A worker that starts and cannot authenticate is otherwise
indistinguishable from one that is simply failing.

When [telemetry](telemetry.md) is on, the worker also receives `TRACEPARENT`
(and `TRACESTATE`, if any) naming the run's span, and `YNR_SPOOL` naming the
spool folder, so the vendor's own spans can join the run's trace. These are
ynh's own variables, like `YNH_AGENT_SESSION`, not the operator's. With
telemetry off it receives none of them.

With the [telemetry relay](telemetry.md#vendor-telemetry-through-the-relay)
on, a `claude` worker also receives Claude Code's telemetry settings pointing
at the run's relay, with every content switch forced off, and the same
settings as `--settings`. Those settings replace any of the same family the
harness passes through.

## Redaction

Trajectories are redacted **by value**. At startup ynh takes the values of
every environment variable whose name looks like a credential — `TOKEN`,
`SECRET`, `PASSWORD`, `API_KEY`, `PAT`, `CREDENTIAL`, `AUTH`, `PRIVATE` — and
replaces every occurrence of those exact strings in the trajectory with
`[redacted:NAME]`.

By value rather than by pattern, because pattern-matching for secrets misses
bespoke formats and mangles innocent text, whereas the values a run was given
are known exactly. The realistic leak is a sensor echoing one: a `curl` that
fails and prints its own `Authorization` header lands in the trajectory
otherwise.

**This is not a general secret scanner.** A credential ynh was never given —
one the agent generated, or read from a file — is not covered. The label names
the variable rather than blanking the text, so a reader can tell "the run's
GitHub token appeared here" from "some unknown string was removed"; the first
is a finding about the harness, the second is noise. An agent that inherits the
parent process environment holds every credential the operator holds, which is
not a default anyone chose. See [MCP credentials](mcp.md) for the declaration
and how a profile narrows it.

`--sandbox srt` is honoured by the `claude` backend only. Requesting it with
`codex` or `cursor` is an **error**, not a warning — a containment control that
silently does not apply is worse than an absent one, because it gets relied
upon. ynh does not provide isolation; it runs inside one you configured.

### What `--sandbox srt` allows

[srt](https://github.com/anthropic-experimental/sandbox-runtime) (`npm install
-g @anthropic-ai/sandbox-runtime`) reads its rules from a settings file, so ynh
writes one for each run and starts the worker as
`srt --settings <file> -- claude ...`. The file is `srt-settings.json` in the
session directory (beside the `--emit-jsonl` file), mode 0600. A run with no
session directory gets it in a private directory under `$YNH_HOME/run/`,
removed when the worker exits. If the file cannot be written the run stops
before the worker starts; srt itself refuses to run when a `--settings` file is
missing or invalid. Neither falls back to running unsandboxed.

| | Allowed | Everything else |
|---|---|---|
| Network | `api.anthropic.com`, `claude.ai`, `platform.claude.com`: the API, and signing in and refreshing a claude.ai or Console login | refused by srt's proxy, including Claude Code's optional hosts (operational telemetry, error reports, updates, plugin downloads, claude.ai connectors) and loopback |
| Writes | the worktree; Claude Code's own state: `~/.claude/`, `~/.claude.json` and its `.backup` and `.lock` beside it, or `$CLAUDE_CONFIG_DIR` when the harness passes it through; srt's own temporary directory (`TMPDIR` is set to `/tmp/claude`) | refused; so is the settings file itself, and srt's always-protected paths inside the worktree (`.git/hooks`, `.git/config`, shell rc files, `.claude/commands`, `.claude/agents` and others) |
| Reads | everywhere, srt's default | |

The network list is the worker's only way out: a task that needs `github.com`
or a package registry fails under `--sandbox srt`. The telemetry relay is not
reachable from inside srt and is skipped; see
[telemetry](telemetry.md#turning-it-on).

## Permissions and `--auto-approve`

By default ynh passes **no permission flag** to the worker. It gets whatever
the vendor CLI and the project grant it, and nothing more. On a machine where
the CLI denies edits, the loop runs to its cap with the agent reporting blocked
writes.

`--auto-approve` grants more, for one run:

| Level | Approves | claude | codex | cursor |
|---|---|---|---|---|
| `edits` | file edits; commands still need approval | `--permission-mode acceptEdits` | error | error |
| `all` | everything the worker asks to do | `--permission-mode bypassPermissions` | `--dangerously-bypass-approvals-and-sandbox` | `--force` |

It exists for runs inside containment you own: a container, an egress policy,
a diff gate, human review of what lands. Outside that, it hands an unattended
agent your credentials.

- **Levels are exact or refused.** codex and cursor have no mode that approves
  edits while still withholding commands, so `edits` is an error on them rather
  than something wider. Any other backend, or an unknown level, fails before
  the run starts.
- **An `edits` worker cannot run commands**, so it cannot run `go build` or
  `go test` itself. The sensors run them between turns and feed the results
  back, so `edits` suits simple lanes and `all` harder ones.
- **The project's choice wins.** If the working directory sets its own
  permission mode, the run is refused and the reason names the file and the
  setting: `permissions.defaultMode` in `.claude/settings.json` or
  `.claude/settings.local.json` for claude, a top-level `approval_policy` or
  `sandbox_mode` in `.codex/config.toml` for codex. cursor has no project-level
  mode: its `.cursor/cli.json` holds only allow and deny lists, which `--force`
  still respects. User-level settings are not consulted.
- **A vendor refusal is a worker error.** Claude Code refuses
  `bypassPermissions` as root, and a managed setting can disable a mode. Either
  way the run ends with exit 20 and the vendor's message. Claude does not fail
  when a setting disables the mode, it starts in another one; the loop reads
  the mode the session actually started in and stops if it is not the one
  asked for. ynh's agent image runs as uid 1001, not root.
- **Run-time only.** The harness manifest cannot set it, there is no
  environment variable for it, and a resume does not restore it: pass it again
  each time.

The level is recorded as `auto_approve` on the trajectory's `session_start`
(and `session_resumed`) event and in the run result. Absent means none.

## Convergence

After each turn the loop runs `ynh check` over the harness's sensors and stops
when its verdict is `pass`.

- Only **blocking** sensors gate. `advisory` and `report` sensors are reported
  and fed back, but never hold convergence open — the same rule `ynh check`
  applies, from the same `tolerance` declaration.
- **Failures already in the [baseline](sensors.md#baseline-inheriting-a-repo-that-already-fails)
  do not gate.** A sensor whose every failure is recorded reports `known`, and
  the loop treats it as debt the run inherited rather than work it owes. Only
  the lines a turn actually introduced are fed back, so the agent is not asked
  to clean a repository it was pointed at.
- When the gate is green and a convergence verifier is declared (a sensor with
  `role: convergence-verifier`, or one named by `--convergence-sensor`), that
  sensor is consulted as the final say, through a direct `ynh sensors run`, and
  the run converges only on `pass`.
- **A verifier that can never pass is refused before the run starts.** A
  `focus` sensor needs an agent runtime to resolve, so ynh reports it
  `deferred`; a `files` sensor reports freshness, which is `reported`. Neither
  is ever `pass`, so either would spend the whole budget and end at the turn
  cap. `ynh agent run` exits 1 before any worker starts, saying the
  verifier requires a command source, and `ynd validate` reports the same
  sensor. See [`convergence-verifier` needs a source that can decide](sensors.md#convergence-verifier-needs-a-source-that-can-decide).
- **A run that expected verification and produced no sensor results does not
  converge.** This matters on resume: a session whose harness cannot be restored
  has no sensors, and a verdict with no evidence behind it is worse than no
  verdict. The loop reports why and exits non-zero. The same applies when
  nothing that ran could ever block — only a blocking *command* sensor can
  produce a blocked verdict, so a harness whose only blocking sensor is a file
  glob verifies nothing and is told so.

A run started with no `--harness` is a plain agent runner — nothing was asked to
verify it, so the worker finishing is the only available signal and the loop
converges on it.

## Stuckness

Two detectors stop a loop that is not going anywhere:

- **No progress** — the sensor picture is unchanged for several turns. The
  comparison covers each sensor's *output*, not just its status, so fixing some
  findings while a sensor still fails counts as progress. File positions and
  durations are normalised, so a finding moving down a file does not, and
  neither does the same test failing with a different timing
  (`--- FAIL: TestX (0.03s)`). Where a baseline is in
  play it compares the *new* failures only: churn among findings that were
  already forgiven is not progress either.
- **Edit loop** — the agent repeats itself across turns.

## Resume

A run can be resumed only if it wrote its trajectory to a file. The session
directory is the folder that holds the `--emit-jsonl` file. A run with no
`--emit-jsonl`, or with `--emit-jsonl -`, has no session directory, writes no
checkpoint, and cannot be resumed.

```bash
mkdir -p runs/tidy
ynh agent run --harness local/demo --task "..." --emit-jsonl runs/tidy/trajectory.jsonl
```

The folder must already exist. While the run goes, it holds:

| File | Contents |
|---|---|
| the `--emit-jsonl` file | The trajectory |
| `checkpoint.json` | Where the run got to, rewritten after every completed turn and at each phase boundary |
| `gate-write-attempts.jsonl` | Only if the agent tried `ynh check --update-baseline` |

The run result reports this folder as `session_dir`. At most one turn of work
is lost: an interrupted turn is redone on resume.

To continue, pass the folder to `--resume`:

```bash
ynh agent run --resume runs/tidy
```

On resume the trajectory is appended to `<dir>/trajectory.jsonl`. Name the
file `trajectory.jsonl` in the first place, as above, or pass the same
`--emit-jsonl` again. Otherwise the resumed events land in a second file.

Give each run its own folder. Two runs that emit into the same folder write the
same `checkpoint.json`, and the last one to write wins.

The checkpoint records the run's identity (backend, task or focus, harness,
profile, convergence sensor, and the turn and token caps) as well as its
counters, so a resume restores the run it is actually resuming. For the
harness, profile, convergence sensor and caps, flags passed on the resume take
precedence and anything omitted comes from the checkpoint.

The backend, task and focus cannot be changed on a resume, only repeated:

- **Backend.** A resume drives the backend the run started on. The resume token
  belongs to that backend (a codex thread id, a claude or cursor session id) and
  means nothing to another, so `--backend` naming a different one is refused
  with exit 21. A checkpoint that records no backend predates recording it and
  resumes on `claude`.
- **Task and focus.** Given neither `--task` nor `--focus`, a resume takes the
  checkpoint's: its focus by name, so the focus's profile applies again, or else
  its task. Given either, it must match what the session was started with, or
  the resume is refused with exit 21. A conversation carrying on under a
  different task is a new run, not a resume.

Some settings are not restored. Pass them again if the original run used them:
`--model`, `--effort`, `--worktree`, `--max-wall`, `--sandbox`, `--auto-approve`,
`--auto-commit` and `--interactive`. `--auto-approve` stays out deliberately, as
`--sandbox` does: a grant to skip permission checks is made by the operator each
time, not inherited from a file.

A run interrupted after planning picks up from the checkpoint's pending message.
A run interrupted during planning starts the plan again from the checkpoint's
task. If the checkpoint records no task, the resume is refused until `--task`
supplies one.

Budgets carry across: consumption is restored alongside the caps, so resuming
does not hand the run a fresh allowance. The wall-clock time already spent
counts against `--max-wall`, but the cap itself comes from the flag, the
harness or the default.

A harness given as a path is checkpointed as its absolute path, so `--resume`
finds the same harness from any directory. In the trajectory and the JSON
result the harness is named by its manifest name rather than the path, and a
path never reaches telemetry.

If a checkpoint predates those fields and no `--harness` is given, the loop
warns and continues, but cannot converge. It has no sensors to converge on.

## Exit codes

| Code | Meaning |
|---|---|
| 0 | converged |
| 1 | refused before any worker started |
| 10 | turn cap reached |
| 11 | token budget exceeded |
| 12 | wall-clock limit reached |
| 13 | stuck |
| 14 | tamper detected — the baseline moved during the run |
| 15 | plan-iteration cap reached |
| 20 | worker error |
| 21 | resume error |
| 22 | gate error — `ynh check` could not run |
| 30 | aborted by user |
| 31 | interrupted |

Anything non-zero means the loop stopped without the sensors agreeing the work
was done. Code 1 is a run that never started, 10–12 are budgets, 13–15 are the
loop deciding to stop, 20–22 are failures to run, and 30–31 are external
interruption.

Code 1 is the code every ynh command exits with on a user or configuration
error. Here it means `ynh agent run` refused before starting a worker: an
unknown backend, a `--sandbox`, `--auto-approve` or `--effort` setting the
backend cannot honour (including the harness's `agent.effort`), a project that
[chooses its own permission mode](#permissions-and-auto-approve), a harness,
focus or profile that cannot be loaded, or a [convergence verifier that can
never pass](#convergence). ynh failing to write the trajectory also exits 1. A
refused resume is code 21 instead. Nothing ran, so fix the invocation or the
harness rather than retrying: every run with the same flags is refused the same
way.

Earlier releases exited 1 on such a refusal while the `--format json` result
said `exit_code: 20`. The two now agree, and 20 always means ynh got as far as
starting a worker (a vendor CLI missing from `PATH` included).

Code 14 is the one a pipeline must **escalate rather than retry**. It means the
gate's own reference point moved while the run was in progress: the
[baseline](sensors.md#baseline-inheriting-a-repo-that-already-fails) changed,
or stopped being readable. `ynh check --update-baseline` refuses inside an
agent session, but that only closes the front door — nothing stops a worker
editing the baseline files directly, and an agent that cannot converge has
every incentive to. The loop checks before consulting the gate, not after, so a
widened baseline never gets the chance to forgive the failures that turn
introduced.

Code 22 mirrors `ynh check`'s own exit 2 and is an operator fault, not the
agent's: the harness is missing, a sensor command cannot be executed, or the
gate crashed. It is distinct from 20 so a batch of runs can tell "this agent
failed" from "this harness is broken and every run will hit it". The loop stops
at the first occurrence rather than continuing against no signal — spending a
whole budget on turns nothing could verify, and then reporting the exhaustion as
the agent's failure, hides the real fault.

Code 20 includes a worker that cannot authenticate with or reach its model.
The run ends on the **first** turn, with the vendor's own message as the
reason:

```
worker error: claude: Not logged in · Please run /login (authentication failed: does the harness env_passthrough pass the vendor's credentials?)
```

The usual cause is a harness whose `env_passthrough` does not list the
vendor's API key, so the worker [never receives it](#what-the-agent-can-see).
Each backend reads the failure from the vendor's structured output: an error
`result` from Claude Code or Cursor, a `turn.failed` event from Codex. As a
vendor-neutral safety net, a turn that answers without consuming a single
token is also a worker error, because a model cannot respond without consuming
tokens. That rule applies only when the backend reported usage for the turn:
for one that reports none, zero is not a measurement. Without these checks the
same message came back every turn and the run ended as stuck (13), pointing at
the agent rather than its environment.

A vendor CLI that refuses to start, or exits non-zero without answering, is a
worker error too, and its stderr is the reason:

```
worker error: claude: --dangerously-skip-permissions cannot be used with root/sudo privileges for security reasons
```

## Run result

`--format json` prints one object when the run ends, on **every** path —
converged or not. A run that did not converge is the one worth investigating,
so it still reports what it consumed and what it touched. A run refused before
a worker started gets one too, with `exit_code` 1 (or 21 for a resume) and the
refusal as its `reason`. The result's `exit_code` is always the code the
process exits with.

The one exception is a command line that cannot be parsed at all: an unknown
flag, a flag missing its value, a value of the wrong type, `--task` with
`--focus`, or neither of them. That is rejected before a run exists, so it
exits 1 and produces no result. With `--format json`, wherever it appears on
the command line, the error is the
[error envelope](cli-structured.md#error-envelope) on stderr, with
`invalid_input` as its code (`io_error` when a `--task @file` cannot be read),
and stdout is empty:

```json
{"error":{"code":"invalid_input","message":"unknown flag: --bogus"}}
```

```bash
ynh agent run --harness demo --task "..." --format json
```

It carries the exit code and reason, the caps in force **and where each came
from**, what was actually consumed and **which cap bound the run**, the
convergence verifier's last word, the final gate result, and the files the run
changed relative to the commit it started from.

`--emit-jsonl` remains an event *stream*. Reconstructing a result by tailing
NDJSON and inferring across events is exactly the bespoke tooling this exists to
remove. When `--emit-jsonl -` is streaming the trajectory to stdout, the result
goes to stderr so the NDJSON stays clean.

Two fields earn their place in a batch of a hundred runs:

- **`bound_by`** names the cap that ended the run. Read with `budget_sources`,
  it separates *a cap nobody chose fired* from *the cap you set fired* — the
  first is noise, the second is a finding.
- **`changed_files`** includes new files, not just tracked edits. A converged
  run that changed nothing, and a run that rewrote forty files nobody asked
  about, are both findings.

### Cost, the token split and effort

Comparing runs (does this lane need a bigger model, or less effort, and what
does each cost?) needs more than the token total. Each of these is **absent
when the backend did not report it**, never zero, because a zero cost would
read as "free":

| Field | What it holds |
|---|---|
| `consumed.tokens` | Input plus output tokens. Cache reads and writes are not included. |
| `consumed.input_tokens`, `consumed.output_tokens` | The split behind `tokens`. |
| `consumed.cache_read_tokens` | Tokens read from the prompt cache, beside the total rather than in it. |
| `consumed.cache_creation_tokens` | Tokens written to the prompt cache, also beside the total. Their cost is already inside `cost_usd`. |
| `consumed.cost_usd` | The cost the backend reported, summed over the run. ynh never prices tokens itself. |
| `effort` | The reasoning effort the worker reported it ran with. Never inferred from the model name. |

All of them count plan-phase turns, as `tokens` does, and carry across
`--resume` through the checkpoint. A count the backend reported as zero is
present as zero.

Cache writes come from Claude's `cache_creation_input_tokens`, summed per turn
the same way as cache reads. Claude's usage record also splits that total by
cache lifetime in a `cache_creation` object; ynh takes the total only, so the
writes are never counted twice.

**Claude token counts were double before this release.** ynh added each
turn's usage from the assistant events to the same turn's usage on the result
event, and counted an API call again for every content block Claude Code
emits it in. So `consumed.tokens` for the `claude` backend came out at least
twice the real figure, and more on turns with tool calls. Each turn now counts
once, from the result event's per-turn usage, so expect claude token totals
of roughly half what earlier runs reported. The field's meaning is unchanged;
its values were wrong. Compare claude runs across this change with that in
mind. `--max-tokens` and the 2,000,000 default cap are measured against the
corrected total, so a claude run now reaches them about twice as late as
before, which is the cap meaning what it says.

What each backend reports today:

| Backend | Split | Cache reads | Cache writes | Cost | Effort |
|---|---|---|---|---|---|
| `claude` | yes | yes | yes | yes | yes |
| `codex` | yes | yes | when its usage carries `cache_write_input_tokens` | no | no |
| `cursor` | when its output carries usage | when its output carries usage | when its usage carries `cache_creation_input_tokens` | no | no |

Every backend reports the fields in the same meaning. `input_tokens` excludes
cache reads and cache writes, `cache_read_tokens` is the cache reads,
`cache_creation_tokens` is the cache writes, and `tokens` is `input_tokens`
plus `output_tokens`. Codex counts differently, the way the OpenAI API does:
its `input_tokens` includes its `cached_input_tokens` and its
`cache_write_input_tokens`, and its `output_tokens` includes its
`reasoning_output_tokens`. So for `codex`:

| ynh field | From Codex's `turn.completed` usage |
|---|---|
| `consumed.input_tokens` | `input_tokens` minus `cached_input_tokens` minus `cache_write_input_tokens` |
| `consumed.cache_read_tokens` | `cached_input_tokens` |
| `consumed.cache_creation_tokens` | `cache_write_input_tokens`; absent from a Codex too old to report it |
| `consumed.output_tokens` | `output_tokens`, reasoning included and not added again |
| `consumed.tokens` | non-cached, non-written input plus output |
| `consumed.cost_usd`, `effort` | absent: Codex reports neither. An effort ynh asked for is in `effort_requested` |

Codex reports these as running totals for the thread, and a resumed thread
continues from the totals it saved. ynh counts each turn as the difference from
the totals already counted. On `--resume` it starts from what the checkpoint
recorded; a checkpoint with no usage record (one written before ynh read Codex
usage) leaves the first resumed turn's share unknown, so that one turn reports
no usage rather than counting the earlier turns twice.

Before this release ynh never read Codex's usage at all: a `codex` run reported
no tokens, the token cap never bound it, and the zero-token rule above did not
apply to it. It also could not drive a turn: `codex exec` reads its prompt
from stdin to the end and runs a single turn, so ynh now runs one `codex exec`
per turn and continues the thread with `codex exec resume <thread_id>`, as it
does with cursor.

Claude Code reports cost as a running total for the worker process, which a
resumed session may continue from where its transcript left off. ynh takes
each turn's cost as the difference, so nothing is counted twice across a
resume. The effort is the one Claude Code says it applies at runtime, after
flags, settings and environment. It is reported whether or not ynh asked for
one; what ynh asked for is `effort_requested` (see [Effort](#effort)).

### The model

Two fields keep what a run asked for apart from what ran:

| Field | What it holds |
|---|---|
| `model` | The model the worker reported it ran on, in the backend's own words. An alias is resolved: `--model sonnet` on Claude Code reports `claude-sonnet-5-5`. Absent when the backend reported none. |
| `model_requested` | What `--model` asked for, as given. Absent when no model was pinned and the backend chose its default. |

`model` is never filled in from `model_requested`. A run on the backend's
default has only `model`; a backend that reports nothing has only
`model_requested`, or neither. Before this release `model` held what
`--model` asked for, which was empty for every run on a default model and an
alias for a pinned one. That value is now `model_requested`.

What each backend reports:

| Backend | `model` comes from |
|---|---|
| `claude` | The `system` `init` event, then each main-thread assistant message's `message.model` |
| `codex` | Nothing: no `codex exec --json` event names the model, so `model` is absent |
| `cursor` | The `system` `init` event, which gives a display name such as `Claude 4 Sonnet` |

For `claude`, the init event names the model the session starts on, and every
assistant message names the model that wrote it. These agree unless Claude
Code switches model mid-run, as when a fallback model takes over; then the
latest main-thread message wins, since it is what actually answered. A
subagent's messages are ignored: it may run on a smaller model while the
session's stays the run's. The model Claude Code names on an answer it wrote
itself, such as "Not logged in", is `<synthetic>`, and is never reported.

On `--resume`, `model` carries over from the checkpoint until the relaunched
worker reports one, and is then that process's: when the model changes across
a resume, the result names the latest. `--model` is not restored (see
[Resume](#resume)), so `model_requested` is what the resume
itself passed.

The trajectory's `session_start` carries `model_requested`, which with the
harness SHA and base commit reproduces the request. It cannot carry the model
that ran: the header is written before the worker has said anything. A
`worker_model` event records that instead, when each worker process first
reports a model and again whenever it reports a different one.
`session_start` still writes `model` too, as a copy of `model_requested`, so
no field disappears from the header. It is deprecated and goes in a release
that bumps the capabilities version; read `model_requested` instead.

### Effort

`--effort low|medium|high` asks the worker for a reasoning effort. A harness
can carry its own in the `agent` block, and the flag wins over it:

```json
"agent": { "max_turns": 40, "effort": "low" }
```

The three levels are ynh's, and each backend maps them to its own setting.
The backends offer more levels than these (Claude Code's `xhigh` and `max`,
Codex's `minimal`, `xhigh` and above), which ynh does not expose, so a level
means the same thing whichever backend runs it.

| ynh | `claude` | `codex` | `cursor` |
|---|---|---|---|
| `low` | `--effort low` | `-c model_reasoning_effort="low"` | refused |
| `medium` | `--effort medium` | `-c model_reasoning_effort="medium"` | refused |
| `high` | `--effort high` | `-c model_reasoning_effort="high"` | refused |

Sources:
[Claude Code CLI reference](https://code.claude.com/docs/en/cli-reference)
(`--effort`: low, medium, high, xhigh, max; "Available levels depend on the
model"); the Codex
[configuration reference](https://learn.chatgpt.com/docs/config-file/config-reference)
(`model_reasoning_effort`) and its `-c key=value` override, which `codex exec`
accepts (`codex-rs/utils/cli/src/config_override.rs`); and `cursor-agent
--help` (2026.10.01), which has no effort flag. Some Cursor models take an
effort inside the model name (`'<model>[effort=high]'`), which is per model,
so it is yours to pass with `--model`, not something ynh can map.

A level the backend cannot honour, or any level on `cursor`, is **refused
before the run starts**, not ignored, whether it came from the flag or the
harness. A harness that sets `agent.effort` therefore cannot run on `cursor`;
`ynd validate` rejects a level other than the three.

The result keeps the request apart from what ran, as it does for the model:

| Field | What it holds |
|---|---|
| `effort` | The effort the worker reported it ran with, in the backend's own words. Today only Claude Code reports one. |
| `effort_requested` | What `--effort` or `agent.effort` asked for. Absent when nothing was asked for. |

A backend that applied a different level than the one asked for (a model that
does not support it, a managed setting that overrides it) shows as the two
disagreeing. The trajectory's `session_start` and `session_resumed` carry
`effort_requested`. `--effort` is not restored on `--resume` (see
[Resume](#resume)): a resume asks for what its own flag, or the harness, says.

### Pinning a run to a toolchain

`harness.sha` is the resolved commit the harness was installed from. `version`
is an author-declared string that can be reused across different content; the
SHA is what actually pins a run to one set of sensors.

`image_digest` records the container image the run executed in. **A run cannot
observe this for itself** — a digest is computed *after* a build, so it cannot
be stamped into the image it identifies, and a process inside a container has
no portable way to learn its own image. The launcher passes it in:

```bash
docker run -e YNH_IMAGE_DIGEST="$(docker inspect --format '{{index .RepoDigests 0}}' my-harness)" …
```

Absent means **not recorded**, not "not containerised". Guessing would be worse
than silence: a wrong digest in a graded corpus is indistinguishable from a
right one until someone tries to reproduce the run and cannot.

Both fields also appear on the trajectory's `session_start` event.

Shape: [`docs/schema/cli/agent-run.schema.json`](https://github.com/eyelock/ynh/blob/main/docs/schema/cli/agent-run.schema.json).

## Trajectory

`--emit-jsonl` writes one JSON object per line, which is how a consumer follows
a run without parsing terminal output.

| Event | Emitted when |
|---|---|
| `session_start` | Run begins. Carries the model requested (`model_requested`, and the deprecated `model` copy of it), the effort requested (`effort_requested`), ynh version, harness version, base commit, the `--auto-approve` level, and the resolved budgets with their sources |
| `session_resumed` | Resumed run begins, before the first new turn. Carries this process's `--auto-approve` level, model requested and effort requested |
| `worker_model` | The worker reported the model it runs on: once per worker process, and again if it reports another |
| `plan` / `plan_revised` | Plan produced or revised |
| `plan_approval_required` | Plan phase is waiting for approval |
| `turn_start` | Act-phase turn begins |
| `assistant_message` | Agent output for the turn |
| `sensor_run` / `sensor_result` | A sensor is run, and its result |
| `feedback_sent` | Sensor results sent back to the agent, for a turn the run then takes; never emitted for a turn a cap or interrupt prevents |
| `turn_approval_required` | Act phase is waiting for approval |
| `stuck_detected` | A stuckness detector fired |
| `tamper_detected` | The baseline moved during the run |
| `budget_snapshot` / `budget_exceeded` | Budget state, and a cap being hit |
| `converged` | Sensors agree the work is done |
| `session_end` | Run finished, with exit code and totals |

`sensor_result` carries the gate's `status` word (`pass`, `fail`, `known`,
`reported`, `deferred`) alongside `passed` and `tolerance`, plus `new_count` and
`known_count` when a baseline is in play. `passed` alone cannot express
"failing, but every failure is already recorded" — which is the difference
between a regression this run caused and debt it inherited.

## Telemetry

`ynh agent run` emits OpenTelemetry when it has somewhere to write it: a
`ynh.run.started` event as the run begins, and one `ynh.run` span and a
`ynh.run.finished` event as it ends, with the outcome, exit code, turns, tokens,
cost, model and harness, and a child span for each `ynh check` between turns.
It writes into a spool folder named by `YNR_SPOOL` or the laptop default, joins
the trace in `TRACEPARENT`, passes the trace and the spool on to the worker and
the sensors, and never carries the
task, the agent's output or a path. With no destination nothing is written and
nothing changes. See [Telemetry](telemetry.md).

## Relationship to `ynh check`

They apply the same policy to the same declarations and differ in who drives:

|  | `ynh check` | `ynh agent run` |
|---|---|---|
| Runs sensors | once | between every turn |
| Drives an agent | no | yes |
| Gating rule | blocking sensors only | blocking sensors only |
| Baseline / ratchet | yes | yes — the loop runs `ynh check` |
| Typical use | a gate, in CI or a hook | unattended iteration |

There is one policy, in one place. The loop shells out to `ynh check --format
json` between turns rather than running sensors itself, so the
[ratchet](harness-engineering.md#sensor-gate-ratchet-loop), the
tolerance rules and the verdict are the same ones a human gets at a terminal.

The loop may not write the baseline. `--update-baseline` refuses inside an agent
session and records the attempt; a baseline that changes by any other route
ends the run with exit 14. Nothing being gated may rewrite the gate's reference
point.

## See also

- [Sensors](sensors.md) — declaring what the loop observes
- [Gating with `ynh check`](tutorial/check.md) — the gate, and baselines
- [Harness Engineering](harness-engineering.md) — where the loop sits
- [Telemetry](telemetry.md): what a run emits, and where
