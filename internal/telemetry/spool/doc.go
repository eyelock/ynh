// Package spool is an OpenTelemetry exporter that writes traces and logs as
// OTLP JSON lines into a spool folder, where a separate reader collects them.
//
// It imports nothing but the standard library and the OpenTelemetry SDK, and
// knows nothing of the program using it or of the reader. It is written to
// move unchanged into its own public module, github.com/eyelock/otel-spool-exporter,
// once that exists; until then it lives here.
//
// # Format
//
// A spool folder belongs to one or more writer processes. Each process writes
// its own files, so there is no locking:
//
//	<service>-<instance id>-<seq>.open.jsonl   the file being written
//	<service>-<instance id>-<seq>.jsonl        a closed file
//
// Each line is one OTLP export request in the OTLP/JSON encoding: an
// ExportTraceServiceRequest (resourceSpans) or an ExportLogsServiceRequest
// (resourceLogs). Trace and span ids are lowercase hex, 64-bit integers and
// timestamps are decimal strings, enums are numbers, as the OTLP
// specification requires of JSON. Traces and logs share a file.
//
// # Guarantees
//
//   - A file is rotated, renamed from .open.jsonl to .jsonl, once the next
//     line would take it past MaxFileBytes (8 MiB by default), and on Close.
//   - The writer's files on disk are capped at MaxBytes (64 MiB by default).
//     Over the cap, new records are dropped and counted; nothing fails. A
//     closed file the reader has removed no longer counts.
//   - A line is written with one write call, so a batch already written
//     survives the process being killed.
//   - Sync flushes the open file to disk, bounded by SyncTimeout (2 seconds
//     by default): past it the flush is abandoned, so a slow network
//     filesystem never blocks the caller.
//   - Every error is swallowed and counted (see Stats). The exporters never
//     return one, so a failing spool cannot reach the program's output or
//     its exit code.
//
// Batching is the SDK's job: register the exporters with the SDK's batch
// processors, at an export interval of one second or less, so records reach
// the file at least every second. Call the providers' ForceFlush and then
// Writer.Sync at the end of each unit of work, and Writer.Close after the
// providers have shut down.
package spool
