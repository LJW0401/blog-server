package content

import (
	"context"
	"log/slog"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
)

// Watch starts an fsnotify-backed hot reloader. File system events within
// content/ are coalesced for `debounce` before triggering a full Reload().
// Returns a cancel function; cancelling also unsubscribes the watcher.
//
// When fsnotify fails to set up, Watch falls back to startup-time snapshot and
// logs a warning — the service still starts, just without live updates.
func (s *Store) Watch(ctx context.Context, debounce time.Duration) (cancel func(), err error) {
	if debounce <= 0 {
		debounce = 200 * time.Millisecond
	}
	watcher, werr := fsnotify.NewWatcher()
	if werr != nil {
		s.logger.Warn("content.watch.init_failed",
			slog.String("err", werr.Error()),
			slog.String("note", "hot reload disabled; startup snapshot only"))
		return func() {}, nil //nolint:nilerr // intentional degradation
	}
	// Watch both leaf dirs; missing dirs are tolerated.
	for _, d := range []string{
		filepath.Join(s.root, "docs"),
		filepath.Join(s.root, "projects"),
	} {
		if err := watcher.Add(d); err != nil {
			s.logger.Warn("content.watch.add",
				slog.String("dir", d),
				slog.String("err", err.Error()))
		}
	}

	loop := &watchLoop{
		store:    s,
		watcher:  watcher,
		debounce: debounce,
	}
	wctx, wcancel := context.WithCancel(ctx)
	go loop.run(wctx)
	return func() {
		wcancel()
		_ = watcher.Close()
	}, nil
}

type watchLoop struct {
	store    *Store
	watcher  *fsnotify.Watcher
	debounce time.Duration
	mu       sync.Mutex
	timer    *time.Timer
}

func (w *watchLoop) run(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case ev, ok := <-w.watcher.Events:
			if !ok {
				return
			}
			if !strings.HasSuffix(ev.Name, ".md") {
				continue
			}
			w.schedule()
		case err, ok := <-w.watcher.Errors:
			if !ok {
				return
			}
			w.store.logger.Warn("content.watch.error", slog.String("err", err.Error()))
		}
	}
}

func (w *watchLoop) schedule() {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.timer != nil {
		w.timer.Stop()
	}
	w.timer = time.AfterFunc(w.debounce, func() {
		if err := w.store.Reload(); err != nil {
			w.store.logger.Error("content.reload", slog.String("err", err.Error()))
		}
	})
}
