// Package logx is the app's shared structured log. It writes operator
// lines to stderr in a compact format and keeps the last N entries in
// a ring buffer that the admin UI streams via WebSocket.
//
//	2026-04-24T15:23:45Z INFO event.started event_id=12 team_id=4
//
// Conventions:
//   - Levels: DEBUG | INFO | WARN | ERROR.
//   - Msg is a short event label, not a sentence. Details go in attrs.
//   - Attrs are alternating key/value pairs:
//     logx.Info("event.started", "event_id", event.ID, "auto", true)
//   - Uneven attrs are rendered as <bad-attrs> — the emitter is wrong,
//     not the world, so don't crash.
package logx

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// Level orders the severities so a numeric compare decides whether an
// entry is above the current threshold.
type Level int32

const (
	LevelDebug Level = iota
	LevelInfo
	LevelWarn
	LevelError
)

func (l Level) String() string {
	switch l {
	case LevelDebug:
		return "DEBUG"
	case LevelInfo:
		return "INFO"
	case LevelWarn:
		return "WARN"
	case LevelError:
		return "ERROR"
	}
	return "LOG"
}

// ParseLevel accepts case-insensitive names used over the wire.
// Defaults to LevelInfo for anything unrecognised so a typo doesn't
// hide all logs.
func ParseLevel(s string) Level {
	switch strings.ToUpper(strings.TrimSpace(s)) {
	case "DEBUG":
		return LevelDebug
	case "WARN", "WARNING":
		return LevelWarn
	case "ERROR":
		return LevelError
	default:
		return LevelInfo
	}
}

// Entry is one log line. Timestamp is UTC; Attrs preserve insertion
// order as they're emitted so the UI can render them consistently.
type Entry struct {
	Seq   uint64    `json:"seq"`
	TS    time.Time `json:"ts"`
	Level Level     `json:"level"`
	Msg   string    `json:"msg"`
	Attrs []AttrKV  `json:"attrs,omitempty"`
}

// AttrKV is a single key=value pair carried alongside an entry.
// Values are stored as their JSON-encodable native form so the UI can
// pick its own formatting.
type AttrKV struct {
	Key   string `json:"k"`
	Value any    `json:"v"`
}

// MarshalJSON produces the wire format with a stringified level so
// client code doesn't need the int enum.
func (e Entry) MarshalJSON() ([]byte, error) {
	out := struct {
		Seq   uint64   `json:"seq"`
		TS    string   `json:"ts"`
		Level string   `json:"level"`
		Msg   string   `json:"msg"`
		Attrs []AttrKV `json:"attrs,omitempty"`
	}{
		Seq:   e.Seq,
		TS:    e.TS.Format(time.RFC3339Nano),
		Level: e.Level.String(),
		Msg:   e.Msg,
		Attrs: e.Attrs,
	}
	return json.Marshal(out)
}

// Format selects the on-stderr rendering. Text is the developer-friendly
// single-line shape; JSON is one object per line for log-aggregator
// ingestion. The UI ring buffer is unaffected — it always serialises
// through Entry.MarshalJSON.
type Format int32

const (
	FormatText Format = iota
	FormatJSON
)

// Sink is the mutable shared state — a ring buffer plus a live set of
// subscribers. The package exports one global sink via the free
// functions; tests can still construct their own if they need to.
type Sink struct {
	mu     sync.RWMutex
	buf    []Entry
	max    int
	seq    uint64
	level  atomic.Int32
	format atomic.Int32 // Format
	subs   map[chan Entry]struct{}
	subsMu sync.Mutex
	writer *os.File
}

// New returns a sink sized for `max` entries. Writer receives the
// formatted text lines (use os.Stderr in prod).
func New(max int, writer *os.File) *Sink {
	if max <= 0 {
		max = 1
	}
	s := &Sink{
		buf:    make([]Entry, 0, max),
		max:    max,
		subs:   make(map[chan Entry]struct{}),
		writer: writer,
	}
	s.level.Store(int32(LevelInfo))
	return s
}

// SetLevel changes the threshold below which entries are dropped.
// Applies to future calls only; already-buffered entries stay.
func (s *Sink) SetLevel(l Level) { s.level.Store(int32(l)) }

// GetLevel reports the current threshold.
func (s *Sink) GetLevel() Level { return Level(s.level.Load()) }

// SetFormat picks the writer-side rendering. Switching at runtime is
// safe — every Log call re-reads the field — so an admin endpoint
// could flip it on the fly without restarting the process.
func (s *Sink) SetFormat(f Format) { s.format.Store(int32(f)) }

// GetFormat reports the currently selected rendering.
func (s *Sink) GetFormat() Format { return Format(s.format.Load()) }

// Log emits an entry. Levels below the threshold are dropped before
// the ring append so the UI only ever sees filtered output.
func (s *Sink) Log(l Level, msg string, attrs ...any) {
	if l < s.GetLevel() {
		return
	}
	e := Entry{
		Seq:   atomic.AddUint64(&s.seq, 1),
		TS:    time.Now().UTC(),
		Level: l,
		Msg:   msg,
		Attrs: pairAttrs(attrs),
	}
	// Ring append + slide.
	s.mu.Lock()
	s.buf = append(s.buf, e)
	if len(s.buf) > s.max {
		s.buf = s.buf[len(s.buf)-s.max:]
	}
	s.mu.Unlock()
	// Mirror to stderr (or wherever writer points) so operators see
	// logs without opening the UI. Format is re-read each call so a
	// runtime flip lands on the very next entry.
	if s.writer != nil {
		var line string
		if Format(s.format.Load()) == FormatJSON {
			line = formatJSON(e)
		} else {
			line = format(e)
		}
		_, _ = fmt.Fprintln(s.writer, line)
	}
	// Fan out to subscribers. Slow readers drop on purpose — we don't
	// want one stuck tab to back-pressure the whole process.
	s.subsMu.Lock()
	for ch := range s.subs {
		select {
		case ch <- e:
		default:
		}
	}
	s.subsMu.Unlock()
}

// Snapshot returns a copy of the current ring contents, oldest first.
// Filtering is applied both by level (already implicit from the drop
// at Log time) and by a minimum sequence number — the UI uses that to
// reconcile what it has with what's new after a reconnect.
func (s *Sink) Snapshot(minSeq uint64) []Entry {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]Entry, 0, len(s.buf))
	for _, e := range s.buf {
		if e.Seq > minSeq {
			out = append(out, e)
		}
	}
	return out
}

// Subscribe registers a channel that receives each new entry. The
// returned cancel removes the subscription and closes the channel.
// Buffer defaults to 64 so a brief stall doesn't drop entries; beyond
// that the fanout prefers dropping over blocking.
func (s *Sink) Subscribe() (chan Entry, func()) {
	ch := make(chan Entry, 64)
	s.subsMu.Lock()
	s.subs[ch] = struct{}{}
	s.subsMu.Unlock()
	cancel := func() {
		s.subsMu.Lock()
		if _, ok := s.subs[ch]; ok {
			delete(s.subs, ch)
			close(ch)
		}
		s.subsMu.Unlock()
	}
	return ch, cancel
}

// ---- Package-level convenience over a shared sink ----

// Default is the process-wide sink. ZV_LOG_FORMAT picks the renderer
// at startup ("json" → JSON-per-line, anything else → text). Tests
// don't pin the env var, so they get the text default.
var Default = newDefault()

func newDefault() *Sink {
	s := New(2000, os.Stderr)
	if strings.EqualFold(os.Getenv("ZV_LOG_FORMAT"), "json") {
		s.SetFormat(FormatJSON)
	}
	return s
}

// Debug/Info/Warn/Error are the everyday entry points. They funnel to
// Default; tests or alternative wiring can swap Default or use their
// own Sink directly.
func Debug(msg string, attrs ...any) { Default.Log(LevelDebug, msg, attrs...) }
func Info(msg string, attrs ...any)  { Default.Log(LevelInfo, msg, attrs...) }
func Warn(msg string, attrs ...any)  { Default.Log(LevelWarn, msg, attrs...) }
func Error(msg string, attrs ...any) { Default.Log(LevelError, msg, attrs...) }

// SetLevel / GetLevel / Snapshot / Subscribe are thin shims over
// Default for call sites that don't hold a sink reference.
func SetLevel(l Level)                { Default.SetLevel(l) }
func GetLevel() Level                 { return Default.GetLevel() }
func SetFormat(f Format)              { Default.SetFormat(f) }
func GetFormat() Format               { return Default.GetFormat() }
func Snapshot(minSeq uint64) []Entry  { return Default.Snapshot(minSeq) }
func Subscribe() (chan Entry, func()) { return Default.Subscribe() }

// pairAttrs folds alternating key/value pairs into AttrKVs.
// Keys are coerced to strings; odd counts or non-string keys are
// flagged with a synthetic "!BADKEY" entry so the UI surfaces it.
func pairAttrs(raw []any) []AttrKV {
	if len(raw) == 0 {
		return nil
	}
	out := make([]AttrKV, 0, len(raw)/2+1)
	for i := 0; i < len(raw); i += 2 {
		if i+1 >= len(raw) {
			out = append(out, AttrKV{Key: "!BADKEY", Value: raw[i]})
			break
		}
		k, ok := raw[i].(string)
		if !ok {
			k = fmt.Sprintf("%v", raw[i])
		}
		out = append(out, AttrKV{Key: k, Value: raw[i+1]})
	}
	return out
}

// formatJSON renders one entry as a single JSON object, suitable for
// log aggregators (Loki / CloudWatch / Datadog) that key off "level",
// "msg", and arbitrary fields. Attrs are flattened into the top level
// so the operator can filter on attr keys directly without a JSONPath
// dance — collisions with the reserved "ts"/"level"/"msg"/"seq" names
// are renamed with an "attr." prefix.
func formatJSON(e Entry) string {
	out := map[string]any{
		"ts":    e.TS.Format(time.RFC3339Nano),
		"level": e.Level.String(),
		"msg":   e.Msg,
		"seq":   e.Seq,
	}
	for _, a := range e.Attrs {
		k := a.Key
		switch k {
		case "ts", "level", "msg", "seq":
			k = "attr." + k
		}
		out[k] = a.Value
	}
	b, err := json.Marshal(out)
	if err != nil {
		// Fall back to the text renderer rather than swallow the line —
		// silent log loss is the worst kind of failure mode.
		return format(e)
	}
	return string(b)
}

// format is the stderr-side rendering. Single line, easy to grep.
func format(e Entry) string {
	var b strings.Builder
	b.WriteString(e.TS.Format(time.RFC3339))
	b.WriteByte(' ')
	b.WriteString(e.Level.String())
	b.WriteByte(' ')
	b.WriteString(e.Msg)
	for _, a := range e.Attrs {
		b.WriteByte(' ')
		b.WriteString(a.Key)
		b.WriteByte('=')
		b.WriteString(renderAttr(a.Value))
	}
	return b.String()
}

// renderAttr keeps strings with spaces or equals quoted so the line
// stays parseable; everything else goes through fmt's default.
func renderAttr(v any) string {
	switch x := v.(type) {
	case string:
		if strings.ContainsAny(x, ` =\"`) {
			b, _ := json.Marshal(x)
			return string(b)
		}
		return x
	case error:
		return renderAttr(x.Error())
	default:
		return fmt.Sprintf("%v", v)
	}
}
