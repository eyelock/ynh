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

The writer is a package of its own (`internal/telemetry/spool`) with no ynh
imports, written to move unchanged into a public module,
`eyelock/otel-spool-exporter`, that every tool will share.

## Joining a trace

A run joins the trace in `TRACEPARENT` and `TRACESTATE` (W3C trace context), so
under a factory step it is the step's child. Run by hand, it starts its own
trace.

When telemetry is on, every process the run starts gets the run's trace and
its spool folder:

| Process | Gets |
|---|---|
| the vendor CLI (the worker) | `TRACEPARENT` (and `TRACESTATE`, if any) naming the run's span, and `YNR_SPOOL` |
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

These wait for ynr, whose repository is design documents only so far:

- **Exporting over OTLP.** The standard OTLP exporters bring in gRPC and
  protobuf, about ninety modules; ynh does not take that on for an endpoint it
  cannot yet use. Until then an `OTEL_EXPORTER_OTLP_*` setting writes nothing,
  with a note.
- **The vendor CLI's own telemetry.** A `--telemetry-relay` setting will start
  `ynr relay` for the run and point Claude Code or Codex at it. Until then ynh
  passes the vendor only `TRACEPARENT`, `TRACESTATE` and `YNR_SPOOL`, and turns
  on none of the vendor's telemetry.
- **The registry** and `ynh telemetry registry --format json`, which will
  replace the draft names above.
- **Conformance** checks with `ynr conformance` in CI.
- **More events**, such as one per turn, per sensor verdict or per budget
  threshold, and metrics.

## See also

- [Agent Loop](agent.md): what `ynh agent run` does, and its run result
- [The Factory Pattern](factory-pattern.md): where a run sits in a factory
