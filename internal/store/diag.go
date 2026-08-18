package store

import (
	"encoding/json"
	"fmt"
	"io"
	"sync"
)

// Diagnostics is a write-only side channel for append-path events. It must
// never change store behavior, return values, or error text; it exists so CI
// and debugging can capture structured evidence (ticket #65) without altering
// user-facing output.
type Diagnostics interface {
	Emit(event string, fields map[string]any)
}

// JSONLinesDiagnostics emits each diagnostic as one JSON object per line.
// Field names are stable so CI can grep and aggregate them.
type JSONLinesDiagnostics struct {
	mu sync.Mutex
	w  io.Writer
}

// NewJSONLinesDiagnostics returns a Diagnostics sink that writes JSON lines to
// w. Writes are serialized; writer errors are ignored (diagnostics are
// best-effort by contract).
func NewJSONLinesDiagnostics(w io.Writer) *JSONLinesDiagnostics {
	return &JSONLinesDiagnostics{w: w}
}

func (d *JSONLinesDiagnostics) Emit(event string, fields map[string]any) {
	record := make(map[string]any, len(fields)+1)
	for k, v := range fields {
		record[k] = v
	}
	record["event"] = event
	line, err := json.Marshal(record)
	if err != nil {
		return
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	_, _ = fmt.Fprintln(d.w, string(line))
}
