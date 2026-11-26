package main

import (
	"database/sql"
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"

	_ "modernc.org/sqlite"
)

// A proxy restarts: a config edit, an upgrade, a crash. Keeping the whole
// picture in memory means the dashboard goes blank each time. The observations
// are written to one SQLite file and read back at startup.
//
// The file lives in the plugin's own data directory inside the container, the
// way other plugins keep their data: plugins/<plugin-id>/. It is created beside
// the plugin directory, never inside it, so the library stays the only file
// there.
const stateFileName = "context.db"

// journal is the persistent store. Every write is a single statement, and the
// whole journal is replayed into memory at startup: the dashboard reads a
// snapshot, so keeping the query path in memory and the durability in SQLite
// gives both speed and survival across restarts.
type journal struct {
	mu   sync.Mutex
	db   *sql.DB
	path string
	// broken records that the file could not be opened. The plugin keeps
	// working in memory and reports it, rather than refusing to start.
	broken string
}

type journalSnapshot struct {
	TraceID     string    `json:"traceId"`
	RequestID   string    `json:"requestId"`
	SessionID   string    `json:"sessionId"`
	ParentID    string    `json:"parentSessionId"`
	Model       string    `json:"model"`
	Provider    string    `json:"provider"`
	SourceFmt   string    `json:"sourceFormat"`
	Stream      bool      `json:"stream"`
	StartedAt   time.Time `json:"startedAt"`
	CompletedAt time.Time `json:"completedAt"`
	Outcome     string    `json:"outcome"`
	StatusCode  int       `json:"statusCode"`
	Error       string    `json:"error"`
	MessageCnt  int       `json:"messageCount"`
	ToolCnt     int       `json:"toolCount"`
	MaxTokens   int       `json:"maxTokens"`
	// Composition is the calibrated split; Estimated is the pre-scale one.
	Composition  map[string]int `json:"composition"`
	Estimated    map[string]int `json:"estimated"`
	InputTokens  int64          `json:"inputTokens"`
	Uncached     int64          `json:"uncachedTokens"`
	CacheRead    int64          `json:"cacheReadTokens"`
	CacheWrite   int64          `json:"cacheWriteTokens"`
	OutputTokens int64          `json:"outputTokens"`
	Reasoning    int64          `json:"reasoningTokens"`
	TotalTokens  int64          `json:"totalTokens"`
	LatencyMs    int64          `json:"latencyMs"`
	TTFTMs       int64          `json:"ttftMs"`
	Failed       bool           `json:"failed"`
	Calibrated   bool           `json:"calibrated"`
	Known        bool           `json:"compositionKnown"`
	SystemChars  int            `json:"systemChars"`
	ClientType   string         `json:"clientType"`
	AgentName    string         `json:"agentName"`
	IsSubagent   bool           `json:"isSubagent"`
	IsFork       bool           `json:"isFork"`
	Compaction   bool           `json:"compaction"`
	Project      string         `json:"project"`
	Title        string         `json:"title"`
	// Items is kept because the projection needs the prompt of the newest
	// requests to rebuild nodes, file operations and commands.
	Items []item `json:"items"`
}

func snapshotToJournal(snap *snapshot) journalSnapshot {
	return journalSnapshot{
		TraceID: snap.TraceID, RequestID: snap.RequestID, SessionID: snap.SessionID,
		ParentID: snap.ParentSession, Model: snap.Model, Provider: snap.Provider,
		SourceFmt: snap.SourceFormat, Stream: snap.Stream,
		StartedAt: snap.StartedAt, CompletedAt: snap.CompletedAt,
		Outcome: snap.Outcome, StatusCode: snap.StatusCode, Error: snap.Error,
		MessageCnt: snap.MessageCount, ToolCnt: snap.ToolCount, MaxTokens: snap.MaxTokens,
		Composition: snap.Composition, Estimated: snap.Estimated,
		InputTokens: snap.InputTokens, Uncached: snap.UncachedTokens,
		CacheRead: snap.CacheReadTokens, CacheWrite: snap.CacheWriteTokens,
		OutputTokens: snap.OutputTokens, Reasoning: snap.ReasoningTokens,
		TotalTokens: snap.TotalTokens, LatencyMs: snap.LatencyMs, TTFTMs: snap.TTFTMs,
		Failed: snap.Failed, Calibrated: snap.Calibrated, Known: snap.CompositionKnown,
		SystemChars: snap.SystemChars, ClientType: snap.ClientType, AgentName: snap.AgentName,
		IsSubagent: snap.IsSubagent, IsFork: snap.IsFork, Compaction: snap.Compaction,
		Project: snap.Project, Title: snap.Title, Items: snap.Items,
	}
}

func (j journalSnapshot) toSnapshot() *snapshot {
	return &snapshot{
		TraceID: j.TraceID, RequestID: j.RequestID, SessionID: j.SessionID,
		ParentSession: j.ParentID, Model: j.Model, Provider: j.Provider,
		SourceFormat: j.SourceFmt, Stream: j.Stream,
		StartedAt: j.StartedAt, CompletedAt: j.CompletedAt,
		Outcome: j.Outcome, StatusCode: j.StatusCode, Error: j.Error,
		MessageCount: j.MessageCnt, ToolCount: j.ToolCnt, MaxTokens: j.MaxTokens,
		Composition: j.Composition, Estimated: j.Estimated,
		InputTokens: j.InputTokens, UncachedTokens: j.Uncached,
		CacheReadTokens: j.CacheRead, CacheWriteTokens: j.CacheWrite,
		OutputTokens: j.OutputTokens, ReasoningTokens: j.Reasoning,
		TotalTokens: j.TotalTokens, LatencyMs: j.LatencyMs, TTFTMs: j.TTFTMs,
		Failed: j.Failed, Calibrated: j.Calibrated, CompositionKnown: j.Known,
		SystemChars: j.SystemChars, ClientType: j.ClientType, AgentName: j.AgentName,
		IsSubagent: j.IsSubagent, IsFork: j.IsFork, Compaction: j.Compaction,
		Project: j.Project, Title: j.Title, Items: j.Items,
	}
}

func openJournal(path string) *journal {
	j := &journal{path: path}
	if path == "" {
		j.broken = "no data directory is writable"
		return j
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		j.broken = err.Error()
		return j
	}
	db, err := sql.Open("sqlite", path+"?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)")
	if err != nil {
		j.broken = err.Error()
		return j
	}
	// One writer keeps the file simple; the dashboard reads memory, not the db.
	db.SetMaxOpenConns(1)
	if _, err := db.Exec(schemaSQL); err != nil {
		db.Close()
		j.broken = err.Error()
		return j
	}
	j.db = db
	return j
}

const schemaSQL = `
CREATE TABLE IF NOT EXISTS requests (
  seq          INTEGER PRIMARY KEY AUTOINCREMENT,
  trace_id     TEXT NOT NULL,
  session_id   TEXT NOT NULL,
  started_at   INTEGER NOT NULL,
  body         TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS requests_session ON requests(session_id, seq);
CREATE INDEX IF NOT EXISTS requests_trace ON requests(trace_id);
CREATE TABLE IF NOT EXISTS events (
  seq          INTEGER PRIMARY KEY AUTOINCREMENT,
  session_id   TEXT NOT NULL,
  at           INTEGER NOT NULL,
  kind         TEXT NOT NULL,
  detail       TEXT,
  delta        INTEGER NOT NULL DEFAULT 0
);
CREATE INDEX IF NOT EXISTS events_session ON events(session_id, seq);
`

// appendRequest records one finished request. A duplicate trace id replaces the
// earlier row: the same request observed twice is one request.
func (j *journal) appendRequest(snap *snapshot) {
	if j == nil || j.db == nil {
		return
	}
	// The journal keeps counters, not prompt text: the text of an old request is
	// superseded by the next one, and storing it would make the file grow without
	// bound while nothing reads it back.
	stored := snapshotToJournal(snap)
	stored.Items = nil
	payload, err := json.Marshal(stored)
	if err != nil {
		return
	}
	j.mu.Lock()
	defer j.mu.Unlock()
	if snap.TraceID != "" {
		if _, err := j.db.Exec(`DELETE FROM requests WHERE trace_id = ?`, snap.TraceID); err != nil {
			return
		}
	}
	_, _ = j.db.Exec(
		`INSERT INTO requests (trace_id, session_id, started_at, body) VALUES (?, ?, ?, ?)`,
		snap.TraceID, snap.SessionID, snap.StartedAt.UnixMilli(), string(payload),
	)
}

func (j *journal) appendEvent(ev event) {
	if j == nil || j.db == nil {
		return
	}
	j.mu.Lock()
	defer j.mu.Unlock()
	_, _ = j.db.Exec(
		`INSERT INTO events (session_id, at, kind, detail, delta) VALUES (?, ?, ?, ?, ?)`,
		ev.SessionID, ev.At.UnixMilli(), ev.Kind, ev.Detail, ev.Delta,
	)
}

// retention is how long an observation is kept. The journal exists so a restart
// does not erase the picture, not so it becomes a permanent archive, and every
// replay loads the whole file into memory.
const journalRetention = 30 * 24 * time.Hour

// pruneLocked drops observations older than the retention window and returns
// how many were removed. It runs at startup and on a slow ticker, so a proxy
// that stays up for months does not grow a file it then has to read back.
func (j *journal) pruneOld(now time.Time) int {
	if j == nil || j.db == nil {
		return 0
	}
	cutoff := now.Add(-journalRetention).UnixMilli()
	j.mu.Lock()
	defer j.mu.Unlock()
	removed := 0
	if result, err := j.db.Exec(`DELETE FROM requests WHERE started_at < ?`, cutoff); err == nil {
		if n, errRows := result.RowsAffected(); errRows == nil {
			removed += int(n)
		}
	}
	if result, err := j.db.Exec(`DELETE FROM events WHERE at < ?`, cutoff); err == nil {
		if n, errRows := result.RowsAffected(); errRows == nil {
			removed += int(n)
		}
	}
	return removed
}

// replay reads the stored observations back, oldest first.
func (j *journal) replay() ([]*snapshot, []event) {
	if j == nil || j.db == nil {
		return nil, nil
	}
	j.mu.Lock()
	defer j.mu.Unlock()

	snaps := make([]*snapshot, 0, 256)
	rows, err := j.db.Query(`SELECT body FROM requests ORDER BY seq`)
	if err == nil {
		for rows.Next() {
			var body string
			if err := rows.Scan(&body); err != nil {
				continue
			}
			var stored journalSnapshot
			if err := json.Unmarshal([]byte(body), &stored); err != nil {
				continue
			}
			snaps = append(snaps, stored.toSnapshot())
		}
		rows.Close()
	}

	events := make([]event, 0, 256)
	eventRows, err := j.db.Query(`SELECT session_id, at, kind, detail, delta FROM events ORDER BY seq`)
	if err == nil {
		for eventRows.Next() {
			var (
				sessionID string
				at        int64
				kind      string
				detail    sql.NullString
				delta     int64
			)
			if err := eventRows.Scan(&sessionID, &at, &kind, &detail, &delta); err != nil {
				continue
			}
			events = append(events, event{
				SessionID: sessionID,
				At:        time.UnixMilli(at),
				Kind:      kind,
				Detail:    detail.String,
				Delta:     delta,
			})
		}
		eventRows.Close()
	}
	return snaps, events
}


func (j *journal) reset() {
	if j == nil || j.db == nil {
		return
	}
	j.mu.Lock()
	defer j.mu.Unlock()
	_, _ = j.db.Exec(`DELETE FROM requests`)
	_, _ = j.db.Exec(`DELETE FROM events`)
}

func (j *journal) close() {
	if j == nil || j.db == nil {
		return
	}
	j.mu.Lock()
	defer j.mu.Unlock()
	// Closing the handle is not enough on Windows while other goroutines may
	// still reach for the file, so the database is also released explicitly.
	_, _ = j.db.Exec(`PRAGMA wal_checkpoint(TRUNCATE)`)
	_ = j.db.Close()
	j.db = nil
}
