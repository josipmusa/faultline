package config

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"sync"
	"time"

	"github.com/josipmusa/faultline/internal/rules"
)

// How often the file is looked at. Polling rather than a notification from the
// operating system keeps Faultline free of another dependency and, more to the
// point, survives the write to a temporary file and rename that most editors
// do, which replaces the file a notification would have been watching.
const pollInterval = 500 * time.Millisecond

// Applier is what a configuration is applied to, and what the rules written
// back to the file are read from. It is the running Faultline seen from the
// file's side.
type Applier interface {
	// Apply installs a configuration that has already been validated. It is
	// called with the file's lock held, one apply at a time.
	Apply(*Config) error
	// Rules returns the rule set as it stands now, to write back to the file.
	Rules() []rules.Rule
	// Bypass returns the hosts the forward proxy is passing through untouched,
	// leaving out Faultline's own defaults, to write back to the file.
	Bypass() []string
	// Scenarios returns the named situations as they stand now, to write back
	// to the file. A scenario created through the API is in the list and not
	// yet in the file, which is how it gets there.
	Scenarios() []rules.Scenario
}

// File is the configuration file in use: it watches for edits and applies them,
// and it writes changes made through the API back.
//
// Reading and writing share one lock, so a reload never lands in the middle of
// a write and a write always starts from what the file says now.
type File struct {
	path  string
	apply Applier
	log   *slog.Logger
	every time.Duration

	mu      sync.Mutex
	seen    stamp // the file as Faultline last read or wrote it
	pending stamp // a change seen once and waiting to settle
	broken  error // what is wrong with the file as it stands, nil when it reads
	missing bool  // whether the last look found no file, so it is said once

	stop context.CancelFunc
	done chan struct{}
}

// Watch prepares to follow the file cfg was read from. The configuration is
// taken as already applied, so nothing happens until the file changes.
func Watch(cfg *Config, apply Applier, log *slog.Logger) *File {
	if log == nil {
		log = slog.Default()
	}
	return &File{path: cfg.Path, apply: apply, log: log, every: pollInterval, seen: cfg.stamp}
}

// Path is the file being watched and written back to.
func (f *File) Path() string { return f.path }

// Start begins watching until ctx is cancelled or Stop is called.
func (f *File) Start(ctx context.Context) {
	ctx, cancel := context.WithCancel(ctx)
	f.stop, f.done = cancel, make(chan struct{})

	go func() {
		defer close(f.done)

		ticker := time.NewTicker(f.every)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				f.reload()
			}
		}
	}()
}

// Stop ends the watch and waits for it to finish.
func (f *File) Stop() {
	if f.stop == nil {
		return
	}
	f.stop()
	<-f.done
	f.stop, f.done = nil, nil
}

// Change makes one change to the rules and writes the result back to the file.
//
// A hand edit the watch has not picked up yet is applied first, so a change
// through the API is made on top of what the file says rather than silently
// overwriting it. A file that cannot be read is not overwritten at all: the
// change is refused and the error says where the problem is, because a rule
// written into a file Faultline does not understand would be lost on the next
// read anyway.
func (f *File) Change(mutate func() error) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	if news, err := f.sync(); err != nil {
		if news {
			f.report(err)
		}
		return err
	}
	if err := mutate(); err != nil {
		return err
	}
	return f.save()
}

func (f *File) reload() {
	f.mu.Lock()
	defer f.mu.Unlock()

	if !f.settled() {
		return
	}
	if news, err := f.sync(); news && err != nil {
		f.report(err)
	}
}

// settled reports whether the file has stopped changing. Saving over a file
// truncates it and writes it again, so there is a moment when it is empty or
// half written, and reading it then would throw every rule away for something
// nobody wrote. A change is therefore looked at twice, one interval apart, and
// read only once it has stopped moving.
func (f *File) settled() bool {
	now, err := statOf(f.path)
	if err != nil || now.same(f.seen) {
		return true // nothing is in the middle of changing; sync says what it makes of that
	}
	if !now.same(f.pending) {
		f.pending = now
		return false
	}
	return true
}

// sync applies the file when it has changed since Faultline last read or wrote
// it, and says what is wrong with it if anything is. The first return says
// whether the problem is news: a file left broken keeps failing every time it
// is looked at, and one broken save should be reported once, not twice a
// second. The caller holds the lock.
func (f *File) sync() (news bool, err error) {
	now, statErr := statOf(f.path)
	if statErr != nil {
		if !f.missing {
			f.missing = true
			f.log.Warn("config: the file cannot be read, the rules already in memory stay in force",
				"path", f.path, "err", statErr)
		}
		return false, nil
	}
	if f.missing {
		f.missing = false
		f.log.Info("config: the file is readable again", "path", f.path)
	}
	if now.same(f.seen) {
		return false, f.broken
	}
	f.seen = now

	cfg, err := Load(f.path)
	if err != nil {
		// Saving over a file truncates it before it is written again, so one
		// save can be caught twice on the way through; what makes a problem
		// news is the problem, not the moment it was noticed.
		news = f.broken == nil || f.broken.Error() != err.Error()
		f.broken = err
		return news, err
	}
	f.broken = nil

	if err := f.apply.Apply(cfg); err != nil {
		return true, err
	}
	f.log.Info("config: reloaded", "path", f.path, "rules", len(cfg.Rules))
	return true, nil
}

// ErrNotSaved says a change was made but did not reach the file, so what is in
// memory and what is on disk have parted ways.
var ErrNotSaved = errors.New("could not be written to the configuration file")

// save writes the rules back and remembers the file as Faultline left it, so
// its own write does not come back as an edit and reset behavior state. The
// caller holds the lock.
func (f *File) save() error {
	if err := Save(f.path, f.apply.Rules(), f.apply.Bypass(), f.apply.Scenarios()); err != nil {
		return fmt.Errorf("%w: %w", ErrNotSaved, err)
	}
	if now, err := statOf(f.path); err == nil {
		f.seen = now
	}
	f.broken = nil
	return nil
}

// report says, at error level and in one line, that the file was not applied
// and what is wrong with it.
func (f *File) report(err error) {
	const message = "config: the file was not applied, the rules already in memory stay in force"

	var cfgErr *Error
	if !errors.As(err, &cfgErr) {
		f.log.Error(message, "path", f.path, "err", err)
		return
	}
	attrs := []any{"path", cfgErr.File, "line", cfgErr.Line, "problem", cfgErr.Message}
	if cfgErr.Path != "" {
		attrs = append(attrs, "field", cfgErr.Path)
	}
	f.log.Error(message, attrs...)
}

// stamp is how a change to the file is noticed: its size and the time it was
// last written.
type stamp struct {
	size int64
	mod  time.Time
}

func (s stamp) same(other stamp) bool { return s.size == other.size && s.mod.Equal(other.mod) }

func statOf(path string) (stamp, error) {
	info, err := os.Stat(path)
	if err != nil {
		return stamp{}, err
	}
	return stamp{size: info.Size(), mod: info.ModTime()}, nil
}
