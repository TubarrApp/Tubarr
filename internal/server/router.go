// Package server sets up the Tubarr server.
package server

import (
	"bytes"
	"cmp"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"
	"tubarr/internal/contracts"
	"tubarr/internal/domain/keys"
	"tubarr/internal/domain/logger"
	"tubarr/internal/domain/vars"
	"tubarr/internal/serverauth"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/spf13/viper"
)

// serverStore holds the database, stores, and contexts for the server.
type serverStore struct {
	s      contracts.Store
	cs     contracts.ChannelStore
	ds     contracts.DownloadStore
	vs     contracts.VideoStore
	tss    contracts.TubarrSettingsStore
	db     *sql.DB
	ctx    context.Context
	cancel context.CancelFunc
	access *serverauth.Manager // Nil in tests that don't exercise logins.
}

// tubarrPort.
const tubarrPort = "8827"

// dockerEnv is set by the Docker image, so the interface listens on all addresses there by default.
const dockerEnv = "TUBARR_DOCKER"

// listenAddress returns the address to listen on: the --listen flag or TUBARR_LISTEN, else localhost outside Docker.
func listenAddress() string {
	if addr := cmp.Or(viper.GetString(keys.WebListen), os.Getenv("TUBARR_LISTEN")); addr != "" {
		return addr
	}
	if os.Getenv(dockerEnv) != "" {
		return ":" + tubarrPort
	}
	return "127.0.0.1:" + tubarrPort
}

// loginRequired reports whether the --web-auth flag or TUBARR_WEB_AUTH leaves logins on (anything but "none").
func loginRequired() bool {
	mode := cmp.Or(viper.GetString(keys.WebAuth), os.Getenv("TUBARR_WEB_AUTH"))
	return !strings.EqualFold(strings.TrimSpace(mode), "none")
}

// healthPath is the container healthcheck endpoint.
const healthPath = "/health"

// activityPath is polled by the web UI for running crawls and Metarr processes.
const activityPath = "/api/v1/channels/activity"

// maxRequestBody is the largest request body accepted (scrape config file saves are the largest).
const maxRequestBody = 10 << 20

// limitRequestBody caps request bodies at maxRequestBody.
func limitRequestBody(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.Body = http.MaxBytesReader(w, r.Body, maxRequestBody)
		next.ServeHTTP(w, r)
	})
}

// skipLogging applies the logger middleware to all requests except those for paths.
func skipLogging(logMiddleware func(http.Handler) http.Handler, paths ...string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		logged := logMiddleware(next)
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if slices.Contains(paths, r.URL.Path) {
				next.ServeHTTP(w, r)
				return
			}
			logged.ServeHTTP(w, r)
		})
	}
}

// NewRouter returns a http Handler.
func NewRouter(ss serverStore) (http.Handler, error) {
	// Initialize router
	r := chi.NewRouter()

	// Middleware
	r.Use(middleware.RequestID)
	r.Use(skipLogging(middleware.Logger, healthPath, activityPath)) // Polled often, keep out of logs.
	r.Use(middleware.Recoverer)
	r.Use(http.NewCrossOriginProtection().Handler) // Blocks other websites' pages from sending changes (CSRF).
	r.Use(limitRequestBody)
	if ss.access != nil {
		r.Use(ss.access.Middleware)
	}

	// --- Health ---
	r.Get(healthPath, ss.handleHealth)

	// --- Metarr Log ---
	// POST endpoint for Metarr instances to send logs.
	r.Post(serverauth.MetarrLogsPath, ss.handlePostMetarrLogs)

	// --- API Routes ---
	r.Route("/api/v1", func(r chi.Router) {
		// Login and access settings API.
		if ss.access != nil {
			ss.access.Mount(r)
		}

		// Channels API.
		r.Route("/channels", func(r chi.Router) {
			r.Get("/all", ss.handleListChannels)
			r.Get("/activity", ss.handleGetActivity)
			r.Post("/add", ss.handleCreateChannel)
			r.Post("/add-from-file", ss.handleAddChannelFromFile)
			r.Post("/add-from-directory", ss.handleAddChannelsFromDir)
			r.Get("/{id}", ss.handleGetChannel)
			r.Get("/{id}/latest-downloads", ss.handleLatestDownloads)
			r.Get("/{id}/all-videos", ss.handleGetAllVideos)
			r.Put("/{id}/update", ss.handleUpdateChannel)
			r.Delete("/{id}/delete", ss.handleDeleteChannel)
			r.Delete("/{id}/delete-videos", ss.handleDeleteChannelVideos)
			r.Delete("/{id}/cancel-download/{videoID}", ss.handleCancelDownload)
			r.Post("/{id}/crawl", ss.handleCrawlChannel)
			r.Post("/{id}/stop-queue", ss.handleStopQueue)
			r.Post("/{id}/abort", ss.handleAbortAll)
			r.Post("/{id}/stop-metarr", ss.handleStopMetarr)
			r.Post("/{id}/ignore-crawl", ss.handleIgnoreCrawlChannel)
			r.Post("/{id}/download-urls", ss.handleDownloadURLs)
			r.Post("/{id}/available", ss.handleAvailableAction)
			r.Post("/{id}/run-metarr", ss.handleRunMetarr)
			r.Post("/{id}/notification-seen", ss.handleNewVideoNotificationSeen)
			r.Post("/{id}/toggle-pause", ss.handleTogglePauseChannel)
		})

		// Status API (200 when idle, 503 while jobs run; for container update hooks).
		r.Get("/status/idle", ss.handleGetIdle)

		// Downloads API.
		r.Get("/downloads", ss.handleGetDownloads)
		r.Get("/transcodes", ss.handleGetTranscodes)
		r.Get("/videos/available", ss.handleGetAvailableVideos)

		// Logs API.
		r.Get("/logs", ss.handleGetTubarrLogs)
		r.Get("/logs/metarr", ss.handleGetMetarrLogs)
		r.Get("/logs/level", ss.handleGetLogLevel)
		r.Post("/logs/level/{level}", ss.handleSetLogLevel)

		// Blocked Domains API.
		r.Get("/blocked-domains", ss.handleGetBlockedDomains)
		r.Delete("/blocked-domains/{domain}", ss.handleUnblockDomain)

		// Settings API.
		r.Get("/settings/crawl-concurrency", ss.handleGetCrawlConcurrency)
		r.Put("/settings/crawl-concurrency", ss.handleSetCrawlConcurrency)
		r.Get("/settings/global-download-concurrency", ss.handleGetGlobalDownloadConcurrency)
		r.Put("/settings/global-download-concurrency", ss.handleSetGlobalDownloadConcurrency)
		r.Get("/settings/domain-limits", ss.handleGetDomainLimits)
		r.Put("/settings/domain-limits", ss.handleSetDomainLimit)
		r.Delete("/settings/domain-limits/{hostname}", ss.handleDeleteDomainLimit)
		r.Get("/settings/flaresolverr-url", ss.handleGetFlareSolverrURL)
		r.Put("/settings/flaresolverr-url", ss.handleSetFlareSolverrURL)
		r.Post("/settings/flaresolverr-url/test", ss.handleTestFlareSolverr)
		r.Get("/settings/scrape-config-file", ss.handleGetScrapeConfigFile)
		r.Put("/settings/scrape-config-file", ss.handleSetScrapeConfigFile)
		r.Post("/settings/scrape-config-file/reload", ss.handleReloadScrapeConfigFile)
		r.Get("/settings/scrape-config-file/content", ss.handleGetScrapeConfigContent)
		r.Put("/settings/scrape-config-file/content", ss.handleSetScrapeConfigContent)
		r.Get("/settings/scrape-config-file/sites", ss.handleGetScrapeSites)
		r.Post("/settings/scrape-config-file/test", ss.handleTestScrapeSite)
		r.Post("/settings/scrape-config-file/test-crawl", ss.handleTestCrawlSite)
	})

	// --- Static Frontend ---
	// Serve compiled web UI for all unmatched routes (after API routes).
	staticHandler, err := StaticHandler()
	if err != nil {
		return nil, err
	}
	r.Handle("/*", staticHandler)

	return r, nil
}

// StartServer starts the HTTP server on the specified port with graceful shutdown.
func StartServer(inputCtx context.Context, inputCtxCancel context.CancelFunc, store contracts.Store, database *sql.DB) error {
	access, err := serverauth.New(database, store.TubarrSettingsStore(), loginRequired())
	if err != nil {
		return fmt.Errorf("failed to set up web interface access: %w", err)
	}
	vars.MetarrLogToken = access.LogToken()

	ss := serverStore{
		s:      store,
		cs:     store.ChannelStore(),
		ds:     store.DownloadStore(),
		vs:     store.VideoStore(),
		tss:    store.TubarrSettingsStore(),
		db:     database,
		ctx:    inputCtx,
		cancel: inputCtxCancel,
		access: access,
	}

	r, err := NewRouter(ss)
	if err != nil {
		return err
	}
	addr := listenAddress()

	srv := &http.Server{
		Addr:              addr,
		Handler:           r,
		ReadHeaderTimeout: 10 * time.Second, // Stops slow clients holding connections open.
		ReadTimeout:       time.Minute,      // Whole request, body included.
		IdleTimeout:       2 * time.Minute,  // Keep-alive connections between requests.
		// No WriteTimeout: test crawls and FlareSolverr tests answer synchronously and can take minutes.
	}

	// Start server in a goroutine
	serverErr := make(chan error, 1)
	go func() {
		logger.Pl.I("Tubarr web server listening on %s\n", srv.Addr)

		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			serverErr <- fmt.Errorf("server failed: %w", err)
			return
		}

		serverErr <- nil
	}()

	// Start crawl watchdog in background (respects ctx cancellation).
	go ss.startCrawlWatchdog(inputCtx, nil)

	// Wait for interrupt signal.
	select {
	case <-inputCtx.Done():
		logger.Pl.S("Shutting down server (context cancelled)...")

	case err := <-serverErr:
		// server crashed
		if err != nil {
			return fmt.Errorf("server error: %w", err)
		}
	}

	// Create shutdown context with timeout (detached, inputCtx is already cancelled on shutdown).
	shutdownCtx, cancel := context.WithTimeout(context.WithoutCancel(inputCtx), 10*time.Second)
	defer cancel()

	// Attempt graceful shutdown.
	if err := srv.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("server forced to shutdown: %w", err)
	}

	logger.Pl.S("Server at %s shut down successfully\n", addr)
	return nil
}

// StaticHandler handles serving of web pages from the web folder next to the executable.
func StaticHandler() (http.Handler, error) {
	exe, err := os.Executable()
	if err != nil {
		return nil, fmt.Errorf("failed to get executable path, cannot locate web pages: %w", err)
	}
	baseDir := filepath.Dir(exe)
	webRoot := filepath.Join(baseDir, "web")

	if _, err := os.Stat(webRoot); err != nil {
		return nil, fmt.Errorf("webpage root %q cannot be loaded: %w", webRoot, err)
	}

	fs := http.FileServer(http.Dir(webRoot))
	notFoundPage := filepath.Join(webRoot, "404.html")
	indexPage := filepath.Join(webRoot, "index.html")

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cleanPath := filepath.Clean(r.URL.Path)
		fullPath := filepath.Join(webRoot, cleanPath)

		// Serve / → index.html
		if cleanPath == "/" {
			servePage(w, r, webRoot, indexPage)
			return
		}

		// Serve existing file if present
		if info, err := os.Stat(fullPath); err == nil && !info.IsDir() {
			if filepath.Ext(fullPath) == ".html" {
				servePage(w, r, webRoot, fullPath)
				return
			}
			fs.ServeHTTP(w, r)
			return
		}

		// Fallback: if the path looks like a SPA route (no file extension)
		if !strings.Contains(filepath.Base(cleanPath), ".") {
			servePage(w, r, webRoot, indexPage)
			return
		}

		// Otherwise, show 404 page if it exists
		if _, err := os.Stat(notFoundPage); err == nil {
			w.WriteHeader(http.StatusNotFound)
			http.ServeFile(w, r, notFoundPage)
			return
		}

		// Final fallback: plain 404 text
		http.NotFound(w, r)
	}), nil
}

var (
	includeRe    = regexp.MustCompile(`<!--\s*include:([\w-]+)((?:\s+[\w-]+="[^"]*")*)\s*-->`)
	includeArgRe = regexp.MustCompile(`([\w-]+)="([^"]*)"`)
)

// servePage serves an HTML page, replacing each <!-- include:name key="value" --> with web/partials/name.html, filling its {{key}} placeholders.
func servePage(w http.ResponseWriter, r *http.Request, webRoot, page string) {
	content, err := os.ReadFile(page)
	if err != nil {
		http.NotFound(w, r)
		return
	}

	var includeErr error
	content = includeRe.ReplaceAllFunc(content, func(marker []byte) []byte {
		m := includeRe.FindSubmatch(marker)
		partial, err := os.ReadFile(filepath.Join(webRoot, "partials", string(m[1])+".html"))
		if err != nil {
			includeErr = err
			return nil
		}
		for _, arg := range includeArgRe.FindAllSubmatch(m[2], -1) {
			partial = bytes.ReplaceAll(partial, []byte("{{"+string(arg[1])+"}}"), arg[2])
		}
		return partial
	})
	if includeErr != nil {
		logger.Pl.E("Failed to build page %q: %v", page, includeErr)
		http.Error(w, "failed to build page", http.StatusInternalServerError)
		return
	}
	http.ServeContent(w, r, filepath.Base(page), time.Time{}, bytes.NewReader(content))
}
