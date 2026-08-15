package web

import (
	"io"
	"log"
	"os"
	"sync"
)

type logSubscriber chan string

type LoggerBroadcaster struct {
	mu          sync.RWMutex
	buffer      []string
	maxBuffer   int
	subscribers map[logSubscriber]bool
}

var Broadcaster = &LoggerBroadcaster{
	buffer:      make([]string, 0, 500),
	maxBuffer:   500,
	subscribers: make(map[logSubscriber]bool),
}

type multiLogWriter struct {
	stdout io.Writer
}

func (w *multiLogWriter) Write(p []byte) (n int, err error) {
	n, err = w.stdout.Write(p)
	line := string(p)
	Broadcaster.AddLine(line)
	return n, err
}

// InitLogger mengalihkan log.Stdout agar dicatat di memory buffer dan disiarkan ke subscriber WebSocket dashboard.
func InitLogger() {
	writer := &multiLogWriter{stdout: os.Stdout}
	log.SetOutput(writer)
}

func (b *LoggerBroadcaster) AddLine(line string) {
	b.mu.Lock()
	if len(b.buffer) >= b.maxBuffer {
		b.buffer = b.buffer[1:]
	}
	b.buffer = append(b.buffer, line)

	subs := make([]logSubscriber, 0, len(b.subscribers))
	for ch := range b.subscribers {
		subs = append(subs, ch)
	}
	b.mu.Unlock()

	for _, ch := range subs {
		select {
		case ch <- line:
		default:
		}
	}
}

func (b *LoggerBroadcaster) GetRecentLogs() []string {
	b.mu.RLock()
	defer b.mu.RUnlock()
	out := make([]string, len(b.buffer))
	copy(out, b.buffer)
	return out
}

func (b *LoggerBroadcaster) Subscribe() logSubscriber {
	ch := make(logSubscriber, 100)
	b.mu.Lock()
	b.subscribers[ch] = true
	b.mu.Unlock()
	return ch
}

func (b *LoggerBroadcaster) Unsubscribe(ch logSubscriber) {
	b.mu.Lock()
	delete(b.subscribers, ch)
	b.mu.Unlock()
	// Drain before close
	for len(ch) > 0 {
		<-ch
	}
	close(ch)
}
