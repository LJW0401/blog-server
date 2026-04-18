// Command blog-server is the HTTP service entry point. Wires config, content,
// render and middleware stacks and serves the public routes.
package main

import (
	"context"
	"errors"
	"flag"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/penguin/blog-server/internal/assets"
	"github.com/penguin/blog-server/internal/config"
	"github.com/penguin/blog-server/internal/content"
	"github.com/penguin/blog-server/internal/middleware"
	"github.com/penguin/blog-server/internal/public"
	"github.com/penguin/blog-server/internal/render"
	"github.com/penguin/blog-server/internal/storage"
)

func main() {
	cfgPath := flag.String("config", "config.yaml", "path to config.yaml")
	flag.Parse()

	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	slog.SetDefault(logger)

	cfg, err := config.Load(*cfgPath)
	if err != nil {
		logger.Error("config.load", slog.String("err", err.Error()))
		os.Exit(1)
	}

	store, err := storage.Open(cfg.DataDir)
	if err != nil && !errors.Is(err, storage.ErrCorruptDB) {
		logger.Error("storage.open", slog.String("err", err.Error()))
		os.Exit(1)
	}
	if errors.Is(err, storage.ErrCorruptDB) {
		logger.Warn("storage.open", slog.String("note", "db was corrupt and rebuilt"))
	}
	defer func() { _ = store.Close() }()

	cstore := content.New(cfg.DataDir, logger)
	if err := cstore.Reload(); err != nil {
		logger.Error("content.reload", slog.String("err", err.Error()))
		os.Exit(1)
	}
	rootCtx, stopWatch := context.WithCancel(context.Background())
	watchCancel, _ := cstore.Watch(rootCtx, 200*time.Millisecond)

	tpl, err := render.NewTemplates(assets.Templates(), render.NewMarkdown())
	if err != nil {
		logger.Error("render.templates", slog.String("err", err.Error()))
		os.Exit(1)
	}

	ph := public.NewHandlers(cstore, tpl, logger)

	mux := http.NewServeMux()
	mux.HandleFunc("/__healthz", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = w.Write([]byte("ok"))
	})
	mux.Handle("/static/", http.StripPrefix("/static/", http.FileServer(http.FS(assets.Static()))))
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		ph.Home(w, r)
	})
	mux.HandleFunc("/docs", ph.DocsList)
	mux.HandleFunc("/docs/", ph.DocDetail)

	chain := middleware.Chain(
		middleware.PanicRecover(logger),
		middleware.RequestID,
		middleware.AccessLog(logger),
		middleware.SecurityHeaders,
		middleware.WithDefaultPasswordBanner(cfg.DefaultPasswordUnchanged),
	)

	srv := &http.Server{
		Addr:              cfg.ListenAddr,
		Handler:           chain(mux),
		ReadHeaderTimeout: 5 * time.Second,
	}

	go func() {
		logger.Info("listen", slog.String("addr", cfg.ListenAddr))
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Error("listen", slog.String("err", err.Error()))
			os.Exit(1)
		}
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	<-stop
	logger.Info("shutdown")
	watchCancel()
	stopWatch()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_ = srv.Shutdown(ctx)
}
