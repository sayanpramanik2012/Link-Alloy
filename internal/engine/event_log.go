package engine

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"
)

const logFileBudget int64 = 4 << 20
const logTailBudget int64 = 1 << 20

type eventLogger struct {
	mu        sync.Mutex
	closed    bool
	queue     chan EngineEvent
	done      chan struct{}
	dropped   atomic.Uint64
	lastError atomic.Value
	path      func() (string, error)
	secure    func(string) error
}

func newEventLogger() *eventLogger {
	var ensure sync.Once
	var ensureErr error
	l := &eventLogger{queue: make(chan EngineEvent, 256), done: make(chan struct{}), secure: SecureProtectedFile, path: func() (string, error) {
		ensure.Do(func() { _, ensureErr = EnsureProtectedSubdirectory("logs") })
		if ensureErr != nil {
			return "", ensureErr
		}
		d, e := EnsureProtectedSubdirectory("logs")
		return filepath.Join(d, "engine.jsonl"), e
	}}
	go l.run()
	return l
}
func (l *eventLogger) enqueue(e EngineEvent) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.closed {
		return
	}
	select {
	case l.queue <- e:
	default:
		l.dropped.Add(1)
	}
}
func (l *eventLogger) run() {
	defer close(l.done)
	for e := range l.queue {
		if err := l.write(e); err != nil {
			l.dropped.Add(1)
			l.lastError.Store(err.Error())
		}
	}
}
func (l *eventLogger) write(e EngineEvent) error {
	p, err := l.path()
	if err != nil {
		return err
	}
	if err = os.MkdirAll(filepath.Dir(p), 0700); err != nil {
		return err
	}
	if l.secure != nil {
		if err = l.secure(p); err != nil {
			return err
		}
	}
	if st, e := os.Stat(p); e == nil && st.Size() >= logFileBudget {
		// Only application-owned log files are rotated; keep at most three backups.
		_ = os.Remove(p + ".3")
		for i := 2; i >= 1; i-- {
			from := p + "." + string(rune('0'+i))
			to := p + "." + string(rune('0'+i+1))
			if e := os.Rename(from, to); e != nil && !os.IsNotExist(e) {
				return e
			}
		}
		if e := os.Rename(p, p+".1"); e != nil {
			return e
		}
	}
	f, err := os.OpenFile(p, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600)
	if err != nil {
		return err
	}
	defer f.Close()
	if err = json.NewEncoder(f).Encode(e); err != nil {
		return err
	}
	if l.secure != nil {
		return l.secure(p)
	}
	return nil
}
func (l *eventLogger) close() {
	l.mu.Lock()
	if !l.closed {
		l.closed = true
		close(l.queue)
	}
	l.mu.Unlock()
	select {
	case <-l.done:
	case <-time.After(2 * time.Second):
	}
}
func (c *TunnelController) CloseLogs() {
	if c.logger != nil {
		c.logger.close()
	}
}
func (c *TunnelController) LogFilePath() (string, error) {
	d, e := ProtectedStateDirectory()
	return filepath.Join(d, "logs", "engine.jsonl"), e
}
func (c *TunnelController) addLog(e EngineEvent) {
	if e.At.IsZero() {
		e.At = time.Now()
	}
	c.logMu.Lock()
	c.appendMemoryLog(e)
	c.logMu.Unlock()
	if c.logger != nil {
		c.logger.enqueue(e)
	}
}
func (c *TunnelController) appendMemoryLog(e EngineEvent) {
	if len(c.logs) == maxLogEntries {
		copy(c.logs, c.logs[1:])
		c.logs[len(c.logs)-1] = e
	} else {
		c.logs = append(c.logs, e)
	}
}
func (c *TunnelController) Logs(limit int) []EngineEvent {
	c.logMu.RLock()
	defer c.logMu.RUnlock()
	if limit <= 0 || limit > len(c.logs) {
		limit = len(c.logs)
	}
	return append([]EngineEvent(nil), c.logs[len(c.logs)-limit:]...)
}
func (c *TunnelController) loadRecentLogs() {
	p, e := c.LogFilePath()
	if e != nil {
		return
	}
	if _, e = EnsureProtectedSubdirectory("logs"); e != nil {
		return
	}
	if e = ValidateProtectedPath(p); e != nil {
		return
	}
	if e = SecureProtectedFile(p); e != nil {
		return
	}
	f, e := os.Open(p)
	if e != nil {
		return
	}
	defer f.Close()
	st, e := f.Stat()
	if e != nil {
		return
	}
	offset := st.Size() - logTailBudget
	if offset < 0 {
		offset = 0
	}
	if _, e = f.Seek(offset, io.SeekStart); e != nil {
		return
	}
	data, e := io.ReadAll(io.LimitReader(f, logTailBudget))
	if e != nil {
		return
	}
	lines := bytes.Split(data, []byte{'\n'})
	if offset > 0 && len(lines) > 0 {
		lines = lines[1:]
	}
	for _, line := range lines {
		var event EngineEvent
		if json.Unmarshal(line, &event) == nil && !event.At.IsZero() {
			c.appendMemoryLog(event)
		}
	}
}
