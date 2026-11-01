# Telemetry

`ynh agent run` emits OpenTelemetry: one span per run with a child span for
each sensor check, and an event when the run starts and when it ends. A factory
can then follow a run from the step that started it, through the run, into the
vendor CLI and the sensors.

It is the only ynh command that does. Every other command launches the vendor
CLI and gets out of the way, so none lives long enough to report.

Telemetry is off unless something gives it a destination, and it never changes
a run: the result, the output and the exit code are the same with it and
without it.

## Where it goes

ynh chooses when the run starts, in this order:

1. **`OTEL_EXPORTER_OTLP_*`.** Any non-empty variable with this prefix means the
   operator chose an OTLP endpoint, and that choice wins. This version of ynh
   cannot export over OTLP yet (see [Not yet](#not-yet)), so it prints one note
   on stderr and writes nothing.
2. **The spool.** `YNR_SPOOL` names a spool folder, or the laptop default
   `$XDG_STATE_HOME/ynr/spool/local` exists (`~/.local/state/ynr/spool/local`
   when `XDG_STATE_HOME` is unset). ynh writes OTLP JSON lines into it.
3. **Nothing.** With neither, ynh uses OpenTelemetry's no-op providers: nothing
   is written, nothing is printed, and the vendor CLI's environment is exactly
   what it was before telemetry existed.

A run that found nothing looks again for the spool folder once a minute, in
the background, as a long-lived process should (ynr ADR-004): an agent run can
last an hour, and a `ynr serve` started after it should still receive it. When
the folder appears, ynh starts writing, and records the run from its real start
time: the started event and the span carry the time the run began, not the time
the spool appeared. A worker already running keeps the environment it started
with; every process started after that gets the trace and the spool. An
operator's `OTEL_EXPORTER_OTLP_*` stops the search, since that choice is theirs.

ynh never creates the laptop default folder, and never starts anything because
it found [ynr](https://github.com/eyelock/ynr). It creates a folder named by
`YNR_SPOOL` if it is missing, since whoever set the variable asked for it.

## The spool

A spool folder holds OpenTelemetry's JSON-lines format: each line is one OTLP
export request (`resourceSpans` or `resourceLogs`) in the OTLP/JSON encoding.
Each ynh process writes its own files, so parallel runs never share one:

```text
ynh-<instance id>-000001.open.jsonl   being written
ynh-<instance id>-000001.jsonl        closed
```

- Records are written in batches at least every second, and the started event
  at once.
- A file is closed and renamed at 8 MB, and when the run ends.
- One run's files are capped at 64 MB. Past the cap, records are dropped and
  counted.
- The file is flushed to disk when the run starts and when it ends. Each flush
  is abandoned after 2 seconds, so a slow network filesystem never holds up a
  run or its exit.
- Every telemetry error is swallowed and counted. None reaches stderr, the run
  result or the exit code.

A run killed with `kill -9` leaves its started event and every batch already
written; a started event with no finished event and no span is how a crash
shows up.

The writer is ynr's spool exporter, the Go module
`github.com/eyelock/ynr/spoolexporter`, which every tool that writes to the
spool shares. Besides the rules above, it drops an export request over 4 MB
rather than write a line the reader would skip, and never follows a link
planted where it creates a file.

## Joining a trace

A run joins the trace in `TRACEPARENT` and `TRACESTATE` (W3C trace context), so
under a factory step it is the step's child. Run by hand, it starts its own
trace.

When telemetry is on, every process the run starts gets the run's trace and
its spool folder:

| Process | Gets |
|---|---|
| the vendor CLI (the worker) | `TRACEPARENT` (and `TRACESTATE`, if any) naming the run's span, and `YNR_SPOOL` |
| the vendor CLI, with the [relay](#vendor-telemetry-through-the-relay) on | also its telemetry settings, pointing at the relay |
| each `ynh check` between turns and the one that runs the convergence verifiers, and every sensor command they run | `TRACEPARENT` naming a `ynh.check` span, a child of the run, and `YNR_SPOOL` |

`YNR_SPOOL` is the folder ynh resolved, absolute, including the laptop default,
so whatever those processes start that follows the same contract, such as an MCP
server the vendor launches, writes beside the run.

These variables are ynh's own, like `YNH_AGENT_SESSION`: the worker still
receives none of the operator's environment beyond `env_passthrough`. They come
after `env_passthrough`, so a harness that passes the caller's `TRACEPARENT`
through still gets the run as the parent. Hooks run inside the vendor CLI and
inherit its environment. The local `git` commands the run uses (to find the base
commit, the changed files, and for `--auto-commit`) get nothing added: they make
no network calls and emit nothing.

When telemetry is off, the worker gets no trace context and no `YNR_SPOOL`, and
`ynh check` inherits ynh's environment exactly as it
always have, with nothing added or removed.

## What a run emits

| Name | Kind | When |
|---|---|---|
| `ynh.run.started` | event (log record) | the run begins, before any worker starts |
| `ynh.run` | span | the run, from start to end |
| `ynh.check` | span, child of `ynh.run` | each `ynh check` between turns, and each one that runs the convergence verifiers |
| `ynh.run.finished` | event (log record) | the run ends |

A run refused before any worker starts still emits all three, with the outcome
`refused`. An argument error is not a run and emits nothing.

Every record carries the resource `service.name=ynh`, `service.version`, and
`service.instance.id`, a random id per process. `OTEL_RESOURCE_ATTRIBUTES` adds
to the resource, for example `deployment.environment.name`, but cannot replace
those three: they say which registry describes the records.

### Attributes

The started event and the span carry what the run was asked for. The span and
the finished event carry how it ended. An attribute is absent when there is
nothing to say: tokens and cost the backend did not report are absent, never
zero.

| Attribute | On | Meaning |
|---|---|---|
| `ynh.run.outcome` | span, finished | the outcome word for the exit code, below |
| `process.exit.code` | span, finished | the exit code, as in the run result |
| `ynh.run.bound_by` | span, finished | the cap that ended the run: `turns`, `tokens` or `wall_clock` |
| `ynh.run.turns` | span, finished | turns taken |
| `ynh.run.session_id` | span, finished | the run's session id, as in the run result |
| `ynh.run.vendor` | all | the backend: `claude`, `codex` or `cursor` |
| `gen_ai.request.model` | all | the model asked for with `--model` |
| `gen_ai.response.model` | span, finished | the model the backend reported running |
| `ynh.run.effort.requested` | all | the effort asked for |
| `ynh.run.effort` | span, finished | the effort the backend reported |
| `gen_ai.usage.input_tokens` | span, finished | input tokens, including cache reads and writes |
| `gen_ai.usage.output_tokens` | span, finished | output tokens |
| `gen_ai.usage.cache_read.input_tokens` | span, finished | tokens read from the prompt cache |
| `gen_ai.usage.cache_creation.input_tokens` | span, finished | tokens written to the prompt cache |
| `ynh.run.cost_usd` | span, finished | cost in US dollars, as the backend reported it; ynh never prices tokens |
| `ynh.harness.name` | all | the harness's canonical id |
| `ynh.harness.version` | span, finished | the harness's declared version |
| `ynh.harness.commit` | span, finished | the commit the harness was installed from |
| `ynh.run.focus` | all | the `--focus` given |
| `ynh.run.profile` | all | the `--profile` given |
| `ynh.run.resumed` | all | `true` for a `--resume` |
| `ynh.run.turn` | call spans | the turn the call follows |
| `ynh.call.outcome` | call spans | the gate's verdict (`pass`, `blocked`), the sensor's status word, or `error` when the call could not run; an `error` call has an error status |

On a `--resume`, the harness, focus and profile are the ones the checkpoint
restores where the command line gave none, by the same rule the run itself
applies.

`gen_ai.usage.input_tokens` follows the semantic conventions and includes cached
tokens, so it is the run result's `input_tokens` plus `cache_read_tokens` and
`cache_creation_tokens`.

Outcomes, one per [exit code](agent.md#exit-codes): `converged`, `refused`,
`turn_cap`, `token_budget`, `wall_clock`, `stuck`, `tamper`,
`plan_iteration_cap`, `worker_error`, `resume_error`, `gate_error`,
`user_aborted`, `interrupted`. The span's status is ok for `converged` and an
error otherwise, described by the outcome word alone.

## The registry

Every name above is declared in ynh's telemetry registry, an
[OpenTelemetry Weaver](https://github.com/open-telemetry/weaver) registry in
[`telemetry/registry/`](https://github.com/eyelock/ynh/tree/develop/telemetry/registry)
(ynr ADR-007): `manifest.yaml` names the tool and pins the upstream semantic
conventions, `attributes.yaml` defines ynh's own `ynh.*` attributes with their
types, enum members and examples, and `signals.yaml` lists each span and event
with the attributes it carries and their requirement levels. ynh records no
metrics yet.

The registry is embedded in the binary, and `ynh telemetry registry` prints it:

```bash
ynh telemetry registry                 # the names, for a person
ynh telemetry registry --format json   # the registry, for ynr
```

The JSON is a bare object in the shape every tool of the family prints
(`ynf telemetry registry --format json` defined it): `tool` and `version`,
which `service.name` and `service.version` repeat on every record,
`semantic_conventions`, `attributes`, `standard_attributes`, `spans`, `events`
and `metrics`. ynr reads it to learn the names. The schema is
[`docs/schema/cli/telemetry-registry.schema.json`](https://github.com/eyelock/ynh/blob/develop/docs/schema/cli/telemetry-registry.schema.json).

The Go constants ynh emits with (`internal/telemetry/names_gen.go`) are
generated from the registry with `go generate ./internal/telemetry`, and a test
fails when they differ from it, or when code spells a registered name as a
string instead of using its constant. CI checks the registry itself with
Weaver's `registry check`.

The standard names follow semantic conventions 1.41.0, the last version whose
Go package carries the `gen_ai.*` names, which upstream still marks as in
development. The registry's attributes are at stability `development`: a name
may still change, and when one does, the registry says so.

## Vendor telemetry through the relay

Claude Code and Codex can report their own spans, metrics and events, but only
over the network. The telemetry relay setting lets a run collect them into its spool
too: ynh starts [`ynr relay`](https://github.com/eyelock/ynr), a small OTLP/HTTP
receiver on a random loopback port, points the vendor CLI at it, and stops it
when the run ends. The relay writes what it receives into the run's spool
folder, beside ynh's own records and in the same trace.

### Turning it on

The setting is off by default. The first of these that says anything decides:

1. `--telemetry-relay` on `ynh agent run` turns it on for that run.
2. `YNH_TELEMETRY_RELAY`: `1`, `true`, `yes` or `on` turn it on; `0`, `false`,
   `no` or `off` turn it off, including when the configuration turns it on.
   Any other value is a note on stderr, and off. A factory lane turns the
   relay on for its runs by setting this variable.
3. `"telemetry_relay": true` in `~/.ynh/config.json` (`$YNH_HOME/config.json`)
   turns it on for every run. An unreadable `config.json` leaves it off.

With the setting on, ynh starts the relay only when all of these hold, just
before the worker starts:

- the backend is `claude` or `codex` (see [Other vendors](#other-vendors))
- the run's telemetry goes to the spool: an operator's `OTEL_EXPORTER_OTLP_*`
  means the vendor's telemetry is theirs to direct, so ynh leaves it alone,
  and with no spool there is nowhere to write
- `ynr` is on `PATH`
- the run is not under `--sandbox srt`. srt's settings can allow the relay's
  `127.0.0.1:<port>`, but that entry governs only what goes through srt's
  proxy, and srt sets `NO_PROXY` to cover `127.0.0.1` and `localhost` inside
  the sandbox, so a client that honours it dials the relay directly and the
  sandbox refuses the connection. srt's one direct opening,
  `network.allowLocalBinding`, opens every loopback port on macOS rather than
  the relay's, and none on Linux, where the worker has its own network
  namespace

When one does not hold, ynh prints one note on stderr and the run carries on
without the vendor's telemetry. ynh never starts the relay because it found
`ynr`: only the setting does. The base image carries `ynr`, and that
changes nothing: see [Docker](docker.md#image-contents).

### Its lifetime

ynh runs `ynr relay --spool <the run's spool folder> --format json
--exit-on-stdin-eof` and reads the endpoint from its first line of output,
waiting at most 5 seconds. One relay serves the whole run. It runs in its own
process group, and its stderr is kept in a small buffer that ynh shows only if
the relay fails.

The relay's standard input is a pipe that ynh holds open for the run and never
writes to. No other process ynh starts, the worker included, holds it. With
`--exit-on-stdin-eof` the relay treats that pipe closing as it treats
`SIGTERM`: it drains, flushes and exits. So when ynh ends by any means, a
`kill -9` included, the kernel closes the pipe and the relay ends with it, on
Linux and macOS alike. This needs a `ynr` that has `--exit-on-stdin-eof`; an
older one fails to start and the run carries on without it, with the note
below.

When the worker has exited, ynh closes the pipe and sends the relay `SIGTERM`
(either one stops it), so it drains the requests in flight, flushes and closes
its spool file, and waits up to 10 seconds before killing it. The same happens
when ynh itself is interrupted with `SIGINT` or `SIGTERM`, or a run ends in a
panic.

A relay that fails to start (it exits, prints nothing within the bound, or
prints something other than a loopback endpoint) or dies during the run costs
the run only the vendor's telemetry, with one note on stderr. The run's
result, output and exit code are the same as without the relay.

### What Claude Code receives

With a relay running, the worker's environment carries the settings recorded
in ynr ADR-004 as verified with Claude Code 2.1.289, plus the per-signal
endpoints:

| Variable | Value |
|---|---|
| `CLAUDE_CODE_ENABLE_TELEMETRY` | `1` |
| `OTEL_TRACES_EXPORTER`, `OTEL_METRICS_EXPORTER`, `OTEL_LOGS_EXPORTER` | `otlp` |
| `OTEL_EXPORTER_OTLP_PROTOCOL` | `http/protobuf` |
| `OTEL_EXPORTER_OTLP_ENDPOINT` | the relay, such as `http://127.0.0.1:41234` |
| `OTEL_EXPORTER_OTLP_TRACES_ENDPOINT`, `_METRICS_ENDPOINT`, `_LOGS_ENDPOINT` | the relay with each signal's OTLP/HTTP path: `/v1/traces`, `/v1/metrics`, `/v1/logs` |
| `CLAUDE_CODE_ENHANCED_TELEMETRY_BETA` | `1`; without it Claude Code sends no spans |
| `TRACEPARENT` | the run's span, as for every run with telemetry on |

The per-signal endpoints repeat the generic one because Claude Code uses a
per-signal endpoint "instead of the generic variable for that signal"
([monitoring](https://code.claude.com/docs/en/monitoring-usage)): one set in
the operator's user settings would otherwise send that signal elsewhere.

It also carries every content switch Claude Code documents, forced off:

| Variable | Value | What it would export |
|---|---|---|
| `OTEL_LOG_USER_PROMPTS` | `0` | prompts, the system prompt, and model output under detailed tracing |
| `OTEL_LOG_ASSISTANT_RESPONSES` | `0` | the model's responses |
| `OTEL_LOG_TOOL_DETAILS` | `0` | Bash commands, tool arguments, error strings |
| `OTEL_LOG_TOOL_CONTENT` | `0` | file contents, command output, fetched pages |
| `OTEL_LOG_RAW_API_BODIES` | `0` | whole API requests and responses |
| `OTEL_LOG_MANAGED_SETTINGS` | `0` | the organisation's managed settings, redacted |
| `ENABLE_BETA_TRACING_DETAILED` | `0` | content attributes on spans |

Before adding them, ynh removes every variable of these families that a
harness's `env_passthrough` would otherwise pass: any `OTEL_EXPORTER_OTLP_*`,
any `OTEL_LOG_*`, the exporter selectors, `BETA_TRACING_ENDPOINT` and both
telemetry switches. A harness cannot turn content back on, or send the
vendor's telemetry somewhere else, by passing a variable through.

The same settings also go on the command line, as
`claude --settings '{"env":{...}}'`, for the reason below.

### Can a repository's settings override them?

Claude Code's documentation answers this, as of October 2026:

- **A repository cannot.** `.claude/settings.json` and
  `.claude/settings.local.json` cannot set the telemetry switches, the exporter
  selectors, the `OTEL_EXPORTER_OTLP_*` endpoints or the content switches:
  Claude Code ignores them there. They may set only values that turn something
  off (`none` for a selector, `0` for a content switch), and such a value does
  not override one set in "the environment you start Claude Code from, a
  `--settings` file, or managed settings"
  ([settings reference, "Variables Claude Code ignores in `env`"](https://code.claude.com/docs/en/settings-reference#variables-claude-code-ignores-in-env);
  [monitoring](https://code.claude.com/docs/en/monitoring-usage)). This needs
  Claude Code 2.1.282 or later.
- **The operator's user settings could.** An `env` block in
  `~/.claude/settings.json` "overwrites the same variable exported in your
  shell" ([settings reference, `env`](https://code.claude.com/docs/en/settings-reference#env)),
  so on its own the environment would not stop a user's settings from turning
  prompt logging on or moving the endpoint.
- **`--settings` outranks every file but managed settings.** It "applies it
  above your user, project, and local files and below managed settings", and
  it can set `env` ([settings, "Settings precedence"](https://code.claude.com/docs/en/settings#settings-precedence)).
  ynh therefore passes its settings both ways: in the environment, and as
  `--settings`, the highest precedence it can set for one session.
- **Managed settings win over both.** An organisation's managed settings are
  its own policy, and ynh does not try to override them.

The scheduled real-vendor check in ynr (ADR-008) is where this stays verified
against a live Claude Code.

Two things Claude Code does that ynh does not control, from the same pages:
with tracing on, it sends a W3C `traceparent` header on its requests to the
Anthropic API and to HTTP MCP servers; and it does not pass `OTEL_*` variables
to the processes it starts (the Bash tool, hooks, MCP servers), so an MCP
server such as ynm writes to the spool through `YNR_SPOOL` rather than to the
relay.

The `traceparent` header is an accepted exception to the rule that trace
context never goes to third parties. That rule binds ynh and its sibling
tools; Claude Code's own requests are the vendor's behaviour, there is no
documented switch to stop them short of turning spans off, and the header
carries only random ids, never content.

### What Codex receives

Codex reads its telemetry settings from its `config.toml` (an `[otel]` table),
not from `OTEL_*` variables. ynh gives each `codex exec` (one per turn) these
`-c` overrides, which outrank `~/.codex/config.toml` and its profiles:

| Override | Value | Why |
|---|---|---|
| `otel.trace_exporter` | `{otlp-http={endpoint="<relay>/v1/traces",protocol="binary"}}` | spans to the relay over OTLP/HTTP protobuf |
| `otel.metrics_exporter` | `{otlp-http={endpoint="<relay>/v1/metrics",protocol="binary"}}` | metrics to the relay |
| `otel.exporter` | `"none"` | Codex's logs (events) off: see below |
| `otel.log_user_prompt` | `false` | the prompt, which Codex otherwise logs |
| `otel.log_agent_responses` | `false` | the model's responses |
| `otel.log_guardian_assessments` | `false` | the auto-reviewer's assessments |

Codex's `otlp-http` endpoint is the URL it posts to, so each one carries its
signal's path. Each exporter is given as a whole inline table, so headers, TLS
or another endpoint in a user's `[otel]` block do not survive. Before the run,
ynh also drops any `OTEL_EXPORTER_OTLP_*` a harness's `env_passthrough` would
hand to Codex.

**Codex's logs are off** because every Codex log record carries the signed-in
account's email address and account id as attributes (`user.email`,
`user.account_id`), and nothing in its `[otel]` table leaves them out. People
appear in telemetry only by their handle, never by email ([ynr ADR-006, rule
10](https://github.com/eyelock/ynr/blob/develop/docs/adr/006-sibling-instrumentation-contract.md)). Codex's spans and metrics carry neither. The token counts its events
would have reported are on its turn spans too (`gen_ai.usage.*`,
`codex.turn.token_usage.*`).

**Codex joins the run's trace.** It reads `TRACEPARENT` and `TRACESTATE` from
its environment ("TRACEPARENT detected; continuing trace from parent
context"), and its `codex.exec` span is a child of the run's `ynh.run` span.

Verified on 2026-10-31 with codex-cli 0.166.0, signed in with ChatGPT, against
a real `ynr relay`: `ynh agent run --backend codex` with the relay on wrote
Codex's spans and metrics into the run's spool, in the caller's trace, with
`codex.exec` under `ynh.run`. No log records and no email address reached the
spool. With logs on, Codex's `codex.user_prompt` event carried
`prompt="[REDACTED]"` under `log_user_prompt=false`. An
`OTEL_EXPORTER_OTLP_TRACES_ENDPOINT` or `OTEL_EXPORTER_OTLP_ENDPOINT` in Codex's
environment did not move the traces away from the `-c` endpoint.

**The relay strips paths and host names.** Codex's spans carry the working
directory (`cwd`, ynh's run directory, which sits under the user's home and so
names their account) and the host's name (`host.name`), and nothing in Codex's
`[otel]` table drops an attribute. `ynr relay` removes them before anything
reaches the spool ([ynr #86](https://github.com/eyelock/ynr/pull/86)). It drops
`cwd`, `host.name`, `host.id`, `host.ip`, `host.mac`, `process.owner`,
`process.command`, `process.command_line`, `process.command_args` and
`process.executable.path`, and any attribute whose value is the user's home
folder or a path under it, on every signal and from every vendor. Paths and host
names are dropped rather than hashed
([ynr ADR-006, rule 10](https://github.com/eyelock/ynr/blob/develop/docs/adr/006-sibling-instrumentation-contract.md)). This needs a `ynr` newer than v0.3.2, the
latest release when this was written; an older relay writes those attributes
as Codex sends them. The relay scrubs attributes only, not log bodies or span
names; Codex's log bodies are off anyway, and its span names carry no paths.

**What Codex passes on.** The commands Codex runs inherit `TRACEPARENT` and
`OTEL_RESOURCE_ATTRIBUTES` from its environment. This was checked live on
2026-10-31 with codex-cli 0.166.0, outside ynh. `codex exec --json` ran with
`TRACEPARENT` and `OTEL_RESOURCE_ATTRIBUTES` set in its environment and was
asked to run, in Codex's default shell environment policy:

```sh
env | grep -E '^(TRACEPARENT|TRACESTATE|OTEL_)' | sed 's/=.*//' ; echo done
```

The command's output in the `command_execution` event (exit code 0) was the
variable names only, never their values:

```
OTEL_RESOURCE_ATTRIBUTES
TRACEPARENT
done
```

That shows the names reach the command, not whether the `TRACEPARENT` value
is the one Codex was given or one for its own span. `TRACESTATE` was not set
for that run, so whether it passes through is unchecked. Under ynh with the
relay on, Codex's environment carries no `OTEL_EXPORTER_OTLP_*`, so neither do
its commands.
Unchecked: whether Codex passes them to the MCP servers it starts, and whether
it sends a W3C `traceparent` header on its requests to the OpenAI API.

### Other vendors

Cursor is unexamined. With the setting on, a Cursor run starts no relay and
prints one note.

## No content

Telemetry carries ids, names, enums, counts and durations, never content. In
particular it never carries:

- the task, the focus's prompt, the plan or anything the agent wrote
- the run's error text (`reason` in the run result), which can quote paths and
  vendor output; the span's status is described by the outcome word instead
- any path: the worktree, the session directory, `--resume`, `--emit-jsonl`, a
  harness given as a path, or the changed files
- people's names or email addresses

A vendor's own telemetry through the relay is held to the same rules
([ynr ADR-006, rule 10](https://github.com/eyelock/ynr/blob/develop/docs/adr/006-sibling-instrumentation-contract.md)). ynh turns the vendor's content options off,
turns off Codex's logs (they carry the account's email), and the relay drops
paths and host names before the spool (see
[What Codex receives](#what-codex-receives)).

## Conformance

[`ynr conformance`](https://github.com/eyelock/ynr/blob/develop/docs/how-to/add-conformance-to-ci.md)
checks ynh's telemetry against the instrumentation contract (ynr ADR-006), with
no model and no secret. Its scenarios are in
[`.ynr/conformance.yaml`](https://github.com/eyelock/ynh/blob/develop/.ynr/conformance.yaml).
Each runs `ynh agent run` against ynr's stub vendor, which answers as `claude`,
on a harness in `testdata/conformance/`. The four scenarios end converged, at the
turn cap, at the token budget, and refused before any worker starts. Each plants
a prompt and a credential, and the check fails if either reaches a record.

CI's Conformance job downloads a pinned ynr release, with the SHA-256 of its
`ynr` and `ynr-stub-vendor` archives pinned beside the version. The release's
`checksums.txt` must list the same digests, and the downloads must match them,
before anything is extracted or run. Locally, with `ynr` and `ynr-stub-vendor`
on your `PATH`:

```bash
make conformance
```

Every rule passes or is skipped by design, except one: rule 1's check that a
tool writes to the operator's `OTEL_EXPORTER_OTLP_ENDPOINT` fails, because ynh
cannot export over OTLP yet (see below). Until it can, the job is not among the
checks All Clear needs.

## Not yet

These are still to come:

- **Exporting over OTLP.** The standard OTLP exporters bring in gRPC and
  protobuf, about ninety modules; ynh does not take that on for an endpoint it
  cannot yet use. Until then an `OTEL_EXPORTER_OTLP_*` setting writes nothing,
  with a note, and [conformance](#conformance) fails rule 1's operator-endpoint
  check.
- **Cursor's telemetry** through the relay, once its behaviour is examined.
- **More events**, such as one per turn, per sensor verdict or per budget
  threshold, and metrics.

## See also

- [Agent Loop](agent.md): what `ynh agent run` does, and its run result
- [The Factory Pattern](factory-pattern.md): where a run sits in a factory
