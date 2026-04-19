// Command blog-server is the HTTP service entry point. Wires config, content,
// render, middleware and GitHub sync.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"runtime/debug"
	"syscall"
	"time"

	"github.com/penguin/blog-server/internal/admin"
	"github.com/penguin/blog-server/internal/assets"
	"github.com/penguin/blog-server/internal/auth"
	"github.com/penguin/blog-server/internal/backup"
	"github.com/penguin/blog-server/internal/config"
	"github.com/penguin/blog-server/internal/content"
	"github.com/penguin/blog-server/internal/diary"
	gh "github.com/penguin/blog-server/internal/github"
	"github.com/penguin/blog-server/internal/middleware"
	"github.com/penguin/blog-server/internal/public"
	"github.com/penguin/blog-server/internal/render"
	"github.com/penguin/blog-server/internal/settings"
	"github.com/penguin/blog-server/internal/stats"
	"github.com/penguin/blog-server/internal/storage"
)

// version is injected at build time via -ldflags="-X main.version=..."
// (see Makefile's build target). Falls back to VCS info from debug.BuildInfo
// when not injected (e.g. `go run`), and ultimately "dev".
var version = ""

func resolveVersion() string {
	if version != "" {
		return version
	}
	if info, ok := debug.ReadBuildInfo(); ok {
		// Prefer the module version if we were built as a proper module.
		if v := info.Main.Version; v != "" && v != "(devel)" {
			return v
		}
		// Otherwise fall back to VCS info (commit hash + dirty flag).
		var rev, mod string
		for _, s := range info.Settings {
			switch s.Key {
			case "vcs.revision":
				if len(s.Value) >= 7 {
					rev = s.Value[:7]
				} else {
					rev = s.Value
				}
			case "vcs.modified":
				if s.Value == "true" {
					mod = "-dirty"
				}
			}
		}
		if rev != "" {
			return rev + mod
		}
	}
	return "dev"
}

func main() {
	cfgPath := flag.String("config", "config.yaml", "path to config.yaml")
	showVersion := flag.Bool("version", false, "print version and exit")
	flag.Parse()

	if *showVersion {
		fmt.Println(resolveVersion())
		return
	}

	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	slog.SetDefault(logger)
	logger.Info("startup", slog.String("version", resolveVersion()))

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
	rootCtx, stopRoot := context.WithCancel(context.Background())
	watchStop, _ := cstore.Watch(rootCtx, 200*time.Millisecond)

	tpl, err := render.NewTemplates(assets.Templates(), render.NewMarkdown())
	if err != nil {
		logger.Error("render.templates", slog.String("err", err.Error()))
		os.Exit(1)
	}

	// GitHub cache + sync
	ghCache := gh.NewCache(store.DB)
	ghClient := gh.NewClient(
		gh.WithToken(cfg.GitHubToken),
		gh.WithTimeout(10*time.Second),
	)
	interval := time.Duration(cfg.GitHubSyncIntervalMin) * time.Minute
	if interval < time.Minute {
		interval = 30 * time.Minute
	}
	syncer := gh.NewSyncer(ghClient, ghCache, cstore,
		gh.WithInterval(interval),
		gh.WithLogger(logger),
		gh.WithTokenConfigured(cfg.GitHubToken != ""),
	)
	syncStop := syncer.Start(rootCtx)

	settingsStore := settings.New(store.DB)
	statsStore := stats.New(store.DB, logger)

	ph := public.NewHandlers(cstore, tpl, logger)
	ph.GitHubCache = ghCache
	ph.SettingsDB = settingsStore
	ph.Stats = statsStore
	// Expose site settings to layout.html footer across all pages (public +
	// admin); SiteSettings is cached 30s inside ph.Settings() already.
	tpl.SettingsFn = func() any { return ph.Settings() }

	// Daily cold backup task.
	backupStore := backup.New(cfg.DataDir, store.DB, logger)
	backupStop := backupStore.Start(rootCtx)
	_ = backupStop

	// Auth + admin wiring
	sessionSecret, err := auth.LoadOrCreateSecret(store.DB)
	if err != nil {
		logger.Error("auth.secret", slog.String("err", err.Error()))
		os.Exit(1)
	}
	authStore := auth.NewStore(store.DB, sessionSecret)
	adminH := admin.New(authStore, cfg, *cfgPath, tpl, logger)
	docsAdmin := &admin.DocHandlers{Parent: adminH, Content: cstore, DataDir: cfg.DataDir}
	imagesAdmin := &admin.ImageHandlers{Parent: adminH, DataDir: cfg.DataDir}
	settingsAdmin := &admin.SettingsHandlers{Parent: adminH, Settings: settingsStore, Invalidate: ph.InvalidateSettings}
	projectsAdmin := &admin.ProjectHandlers{
		Parent: adminH, Content: cstore, DataDir: cfg.DataDir,
		GitHubClient: ghClient, GitHubCache: ghCache,
	}

	mux := http.NewServeMux()
	healthzBody := fmt.Sprintf("ok blog-server %s\n", resolveVersion())
	mux.HandleFunc("/__healthz", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = w.Write([]byte(healthzBody))
	})
	mux.Handle("/static/", staticFileServer(assets.Static()))
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			ph.NotFound(w, r)
			return
		}
		ph.Home(w, r)
	})
	mux.HandleFunc("/docs", ph.DocsList)
	mux.HandleFunc("/docs/", ph.DocDetail)
	mux.HandleFunc("/projects", ph.ProjectsList)
	mux.HandleFunc("/projects/", ph.ProjectDetail)
	mux.HandleFunc("/rss.xml", ph.RSS)
	mux.HandleFunc("/sitemap.xml", ph.Sitemap)

	// Diary: /diary 是认证 only 的顶层路由；cookie scope 已是 `/`，与
	// /manage/* 共享 session。Handler 内部自己判未登录并 302 到登录页。
	diaryStore, err := diary.NewStore(cfg.DataDir)
	if err != nil {
		logger.Error("diary.store.init", slog.String("err", err.Error()))
		os.Exit(1)
	}
	diaryH := diary.New(diaryStore, tpl, authStore, logger)
	mux.HandleFunc("/diary", diaryH.Page)
	mux.HandleFunc("/diary/api/day", diaryH.APIDay)
	mux.HandleFunc("/diary/api/save", diaryH.APISave)

	// Admin routes: public login/password-reset endpoints are at /manage/login;
	// the authGate middleware protects everything else under /manage.
	mux.HandleFunc("/manage/login", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			adminH.LoginSubmit(w, r)
			return
		}
		adminH.LoginPage(w, r)
	})
	mux.HandleFunc("/manage/logout", adminH.Logout)

	protected := buildAdminMux(adminH, docsAdmin, imagesAdmin, settingsAdmin, projectsAdmin)
	// /images/* static file serving (uploaded content).
	mux.Handle("/images/", http.StripPrefix("/images/", http.FileServer(http.Dir(filepath.Join(cfg.DataDir, "images")))))

	gate := middleware.AuthGate(authStore)(protected)
	mux.Handle("/manage", gate)
	mux.Handle("/manage/", gate)

	chain := middleware.Chain(
		middleware.PanicRecover(logger),
		middleware.RequestID,
		middleware.AccessLog(logger),
		middleware.SecurityHeaders,
		middleware.Gzip,
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

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	<-sig
	logger.Info("shutdown")
	stopRoot()
	watchStop()
	syncStop()
	backupStop()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_ = srv.Shutdown(ctx)
}
