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
| each `ynh check` between turns, and every sensor command it runs | `TRACEPARENT` naming a `ynh.check` span, a child of the run, and `YNR_SPOOL` |
| the convergence verifier's `ynh sensors run` | `TRACEPARENT` naming a `ynh.sensors.run` span, and `YNR_SPOOL` |

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
`ynh check` and `ynh sensors run` inherit ynh's environment exactly as they
always have, with nothing added or removed.

## What a run emits

| Name | Kind | When |
|---|---|---|
| `ynh.run.started` | event (log record) | the run begins, before any worker starts |
| `ynh.run` | span | the run, from start to end |
| `ynh.check` | span, child of `ynh.run` | each `ynh check` between turns |
| `ynh.sensors.run` | span, child of `ynh.run` | each run of the convergence verifier |
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
| `ynh.sensor.name` | `ynh.sensors.run` | the sensor run |
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

**These names are a draft.** The `ynh.*` names will move into a registry ynh
owns, with constants generated from it, and may change then. The standard names
follow semantic conventions 1.41.0, the last version whose Go package carries
the `gen_ai.*` names, which upstream still marks as in development.

## Vendor telemetry through the relay

Claude Code can report its own spans, metrics and events, but only over the
network. The telemetry relay setting lets a run collect them into its spool
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

- the backend is `claude` (see [Other vendors](#other-vendors))
- the run's telemetry goes to the spool: an operator's `OTEL_EXPORTER_OTLP_*`
  means the vendor's telemetry is theirs to direct, so ynh leaves it alone,
  and with no spool there is nowhere to write
- `ynr` is on `PATH`
- the run is not under `--sandbox srt`: srt refuses loopback connections
  unless its settings file allow-lists the address, and ynh does not yet give
  srt a settings file

When one does not hold, ynh prints one note on stderr and the run carries on
without the vendor's telemetry. ynh never starts the relay because it found
`ynr`: only the setting does.

### Its lifetime

ynh runs `ynr relay --spool <the run's spool folder> --format json` and reads
the endpoint from its first line of output, waiting at most 5 seconds. One
relay serves the whole run. It gets no standard input, runs in its own process
group, and its stderr is kept in a small buffer that ynh shows only if the
relay fails.

When the worker has exited, ynh sends the relay `SIGTERM`, so it drains the
requests in flight, flushes and closes its spool file, and waits up to 10
seconds before killing it. The same happens when ynh itself is interrupted
with `SIGINT` or `SIGTERM`, or a run ends in a panic. On Linux the relay also
receives `SIGTERM` if ynh is killed outright (`SIGKILL`); macOS has no such
signal, so there a `kill -9` of ynh leaves its relay running.

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

### Other vendors

Nothing yet. Codex is configured through its `config.toml` rather than the
environment, and whether it reads `TRACEPARENT` is unverified; Cursor is
unexamined. With the setting on, a Codex or Cursor run starts no relay and
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

## Not yet

These are still to come:

- **Exporting over OTLP.** The standard OTLP exporters bring in gRPC and
  protobuf, about ninety modules; ynh does not take that on for an endpoint it
  cannot yet use. Until then an `OTEL_EXPORTER_OTLP_*` setting writes nothing,
  with a note.
- **Other vendors' telemetry** through the relay, starting with Codex once its
  behaviour is verified.
- **The registry** and `ynh telemetry registry --format json`, which will
  replace the draft names above.
- **Conformance** checks with `ynr conformance` in CI.
- **More events**, such as one per turn, per sensor verdict or per budget
  threshold, and metrics.

## See also

- [Agent Loop](agent.md): what `ynh agent run` does, and its run result
- [The Factory Pattern](factory-pattern.md): where a run sits in a factory
