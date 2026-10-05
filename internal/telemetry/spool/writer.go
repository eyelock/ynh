package spool

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// Defaults from the spool format.
const (
	DefaultMaxFileBytes = 8 << 20
	DefaultMaxBytes     = 64 << 20
	DefaultSyncTimeout  = 2 * time.Second
)

// Options configures a Writer. Zero values take the defaults.
type Options struct {
	// Dir is the writer folder. It is created on the first write.
	Dir string
	// Service and InstanceID name the files: <service>-<instance id>-<seq>.
	Service    string
	InstanceID string
	// MaxFileBytes rotates the open file once the next line would pass it.
	MaxFileBytes int64
	// MaxBytes caps this writer's files on disk. Over it, records are dropped.
	MaxBytes int64
	// SyncTimeout bounds a flush to disk.
	SyncTimeout time.Duration
}

// Stats counts what a Writer could not do. Nothing it counts is ever
// reported as an error.
type Stats struct {
	// Dropped is the number of records (spans or log records) not written:
	// over the cap, too large for a file, or lost to a failed write.
	Dropped int64
	// Errors is the number of filesystem operations that failed or timed out.
	Errors int64
}

// Writer appends OTLP JSON lines to a writer's spool files. It is safe for
// concurrent use, so one Writer serves both the trace and the log exporter.
type Writer struct {
	opts Options

	// The counters are atomic so a flush that timed out can count itself
	// without waiting for a write that is stuck holding mu.
	dropped atomic.Int64
	errors  atomic.Int64

	mu         sync.Mutex
	f          *os.File
	openPath   string
	seq        int
	fileBytes  int64
	diskBytes  int64 // the open file plus closed files still counted
	closed     []closedFile
	shutdown   bool
	syncFile   func(*os.File) error
	createFile func(path string) (*os.File, error)
}

type closedFile struct {
	path string
	size int64
}

// NewWriter returns a Writer for opts. It touches nothing on disk until the
// first write, so creating one for a folder that turns out to be unusable
// costs nothing but counted errors.
func NewWriter(opts Options) *Writer {
	if opts.MaxFileBytes <= 0 {
		opts.MaxFileBytes = DefaultMaxFileBytes
	}
	if opts.MaxBytes <= 0 {
		opts.MaxBytes = DefaultMaxBytes
	}
	if opts.SyncTimeout <= 0 {
		opts.SyncTimeout = DefaultSyncTimeout
	}
	opts.Service = fileNamePart(opts.Service, "service")
	opts.InstanceID = fileNamePart(opts.InstanceID, "instance")
	return &Writer{
		opts:     opts,
		syncFile: (*os.File).Sync,
		createFile: func(path string) (*os.File, error) {
			return os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL|os.O_APPEND, 0o644)
		},
	}
}

// fileNamePart keeps a name to characters that are safe in a file name, so
// a service or instance id can never name a path outside the folder.
func fileNamePart(s, fallback string) string {
	s = strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '_', r == '.', r == '-':
			return r
		}
		return '_'
	}, s)
	if strings.Trim(s, "._") == "" {
		return fallback
	}
	return s
}

// Stats reports what the Writer has dropped and failed so far.
func (w *Writer) Stats() Stats {
	return Stats{Dropped: w.dropped.Load(), Errors: w.errors.Load()}
}

// writeLine appends line, which must end in a newline, holding records
// records. It never fails: what it cannot write, it counts.
func (w *Writer) writeLine(line []byte, records int) {
	w.mu.Lock()
	defer w.mu.Unlock()
	n := int64(len(line))
	if w.shutdown || n > w.opts.MaxFileBytes {
		w.dropped.Add(int64(records))
		return
	}
	if w.diskBytes+n > w.opts.MaxBytes {
		w.reclaim()
		if w.diskBytes+n > w.opts.MaxBytes {
			w.dropped.Add(int64(records))
			return
		}
	}
	if w.f != nil && w.fileBytes+n > w.opts.MaxFileBytes {
		w.rotate()
	}
	if w.f == nil {
		if err := w.open(); err != nil {
			w.errors.Add(1)
			w.dropped.Add(int64(records))
			return
		}
	}
	written, err := w.f.Write(line)
	w.fileBytes += int64(written)
	w.diskBytes += int64(written)
	if err != nil {
		w.errors.Add(1)
		w.dropped.Add(int64(records))
		// A partial line ends this file; the reader skips it as malformed and
		// the next record starts a fresh file.
		w.rotate()
	}
}

// reclaim stops counting closed files the reader has removed.
func (w *Writer) reclaim() {
	kept := w.closed[:0]
	for _, c := range w.closed {
		if _, err := os.Lstat(c.path); errors.Is(err, os.ErrNotExist) {
			w.diskBytes -= c.size
			continue
		}
		kept = append(kept, c)
	}
	w.closed = kept
}

// open creates the next .open.jsonl file.
func (w *Writer) open() error {
	if err := os.MkdirAll(w.opts.Dir, 0o755); err != nil {
		return fmt.Errorf("creating spool folder: %w", err)
	}
	// O_EXCL never reuses a file: a name already taken moves on to the next
	// sequence number.
	for range 100 {
		w.seq++
		path := filepath.Join(w.opts.Dir, w.baseName()+".open.jsonl")
		f, err := w.createFile(path)
		if errors.Is(err, os.ErrExist) {
			continue
		}
		if err != nil {
			return fmt.Errorf("creating spool file: %w", err)
		}
		w.f, w.openPath, w.fileBytes = f, path, 0
		return nil
	}
	return errors.New("creating spool file: no free sequence number")
}

func (w *Writer) baseName() string {
	return fmt.Sprintf("%s-%s-%06d", w.opts.Service, w.opts.InstanceID, w.seq)
}

// rotate closes the open file and renames it from .open.jsonl to .jsonl.
func (w *Writer) rotate() {
	if w.f == nil {
		return
	}
	if err := w.f.Close(); err != nil {
		w.errors.Add(1)
	}
	closedPath := strings.TrimSuffix(w.openPath, ".open.jsonl") + ".jsonl"
	if err := os.Rename(w.openPath, closedPath); err != nil {
		w.errors.Add(1)
		closedPath = w.openPath
	}
	w.closed = append(w.closed, closedFile{path: closedPath, size: w.fileBytes})
	w.f, w.openPath, w.fileBytes = nil, "", 0
}

// Sync flushes the open file to disk. It returns within SyncTimeout whatever
// the filesystem does; a flush still running then is abandoned and counted.
func (w *Writer) Sync() {
	w.bounded(func() {
		w.mu.Lock()
		f := w.f
		w.mu.Unlock()
		w.syncOpen(f)
	})
}

// Close flushes the open file to disk, closes it and renames it to .jsonl.
// Records written after Close are dropped and counted. It is bounded like
// Sync.
func (w *Writer) Close() {
	w.bounded(func() {
		w.mu.Lock()
		w.shutdown = true
		f := w.f
		w.mu.Unlock()
		w.syncOpen(f)
		w.mu.Lock()
		w.rotate()
		w.mu.Unlock()
	})
}

func (w *Writer) syncOpen(f *os.File) {
	if f == nil {
		return
	}
	if err := w.syncFile(f); err != nil {
		w.errors.Add(1)
	}
}

// bounded runs op, waiting at most SyncTimeout for it. An op still running
// then is abandoned and counted.
func (w *Writer) bounded(op func()) {
	done := make(chan struct{})
	go func() {
		op()
		close(done)
	}()
	timer := time.NewTimer(w.opts.SyncTimeout)
	defer timer.Stop()
	select {
	case <-done:
	case <-timer.C:
		w.errors.Add(1)
	}
}
