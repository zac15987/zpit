package watcher

import (
	"bufio"
	"fmt"
	"io"
	"log"
	"os"
	"sync"

	"github.com/fsnotify/fsnotify"
)

// Watcher monitors a single Claude Code session log file.
type Watcher struct {
	projectID string
	logPath   string
	logger    *log.Logger
	fsWatcher *fsnotify.Watcher
	offset    int64
	done      chan struct{}
	once      sync.Once
}

// New creates a Watcher for the given project session log.
// It seeks to the end of the file so only new lines are processed.
// logger may be nil; when non-nil, parse failures are logged.
func New(projectID, logPath string, logger *log.Logger) (*Watcher, error) {
	fw, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, fmt.Errorf("creating fsnotify watcher: %w", err)
	}

	// Get current file size to start tailing from end.
	offset := int64(0)
	if info, err := os.Stat(logPath); err == nil {
		offset = info.Size()
	}

	if err := fw.Add(logPath); err != nil {
		fw.Close()
		return nil, fmt.Errorf("watching %s: %w", logPath, err)
	}

	return &Watcher{
		projectID: projectID,
		logPath:   logPath,
		logger:    logger,
		fsWatcher: fw,
		offset:    offset,
		done:      make(chan struct{}),
	}, nil
}

// WatchOnce blocks until new session events are available, then returns them.
// Returns nil events and no error if the watcher is stopped.
func (w *Watcher) WatchOnce() ([]SessionEvent, error) {
	for {
		select {
		case <-w.done:
			return nil, nil
		case event, ok := <-w.fsWatcher.Events:
			if !ok {
				return nil, nil
			}
			if event.Has(fsnotify.Write) {
				events, err := w.readNewLines()
				if err != nil {
					return nil, err
				}
				if len(events) > 0 {
					return events, nil
				}
			}
		case err, ok := <-w.fsWatcher.Errors:
			if !ok {
				return nil, nil
			}
			return nil, fmt.Errorf("fsnotify error: %w", err)
		}
	}
}

// Stop closes the watcher.
func (w *Watcher) Stop() {
	w.once.Do(func() {
		close(w.done)
		w.fsWatcher.Close()
	})
}

// readNewLines reads from the current offset to EOF, parses complete lines.
func (w *Watcher) readNewLines() ([]SessionEvent, error) {
	f, err := os.Open(w.logPath)
	if err != nil {
		return nil, fmt.Errorf("opening log file: %w", err)
	}
	defer f.Close()

	if _, err := f.Seek(w.offset, io.SeekStart); err != nil {
		return nil, fmt.Errorf("seeking: %w", err)
	}

	var events []SessionEvent
	scanner := bufio.NewScanner(f)
	// Increase buffer for potentially large JSONL lines.
	const maxLineSize = 1024 * 1024 // 1 MB
	scanner.Buffer(make([]byte, 0, maxLineSize), maxLineSize)

	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}

		ev, err := ParseLine(line)
		if err != nil {
			// Skip malformed lines instead of failing.
			if w.logger != nil {
				w.logger.Printf("watcher: parse line failed at offset %d: %v", w.offset, err)
			}
			continue
		}
		events = append(events, ev)
	}

	// Update offset to current position.
	pos, err := f.Seek(0, io.SeekCurrent)
	if err == nil {
		w.offset = pos
	} else {
		// Fallback: get file size.
		if info, serr := f.Stat(); serr == nil {
			w.offset = info.Size()
		}
	}

	return events, scanner.Err()
}
