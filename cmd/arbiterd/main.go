package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/TrueBlocks/rulesforpennies.io/internal/arbiter"
	"github.com/TrueBlocks/rulesforpennies.io/internal/ratelimit"
	"github.com/TrueBlocks/rulesforpennies.io/internal/rulesdb"
	"github.com/TrueBlocks/rulesforpennies.io/internal/suggestions"
	"github.com/TrueBlocks/trueblocks-art/packages/appd"
	"github.com/TrueBlocks/trueblocks-art/packages/creds"
)

// paused reports whether arbiterd is held in its paused state. See issue #598: the
// pennies launchd stack crash-loops and floods its error log, so the daemon answers
// every request with a notice instead of serving rulings. Set this to false to restore
// normal service.
var paused = true

const pausedMessage = "This process is paused. See issue #598"

func main() {
	addr := flag.String("addr", ":8080", "listen address")
	dataDir := flag.String("data", "", "data directory (default: ~/.local/share/trueblocks/arbiterd)")
	dbFile := flag.String("db", "", "path to rules.db SQLite database")
	promptFile := flag.String("prompt", "", "path to system prompt template file")
	dailyCap := flag.Float64("daily-cap", 10.0, "daily spend cap in USD")
	devMode := flag.Bool("dev", false, "enable dev mode (CORS for localhost)")
	logFile := flag.String("log", "", "path to log file (default: stderr)")
	publicDir := flag.String("public", "", "path to pennies/public static files")
	appsConfig := flag.String("apps-config", appd.DefaultConfigPath(), "path to apps.json for cross-app nav")
	flag.Parse()

	if *dataDir == "" {
		home, _ := os.UserHomeDir()
		*dataDir = filepath.Join(home, ".local", "share", "trueblocks", "arbiterd")
	}
	if err := os.MkdirAll(*dataDir, 0755); err != nil {
		log.Fatalf("cannot create data dir: %v", err)
	}

	if *logFile != "" {
		f, err := os.OpenFile(*logFile, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
		if err != nil {
			log.Fatalf("cannot open log file: %v", err)
		}
		defer f.Close()
		log.SetOutput(f)
	}

	if paused {
		servePaused(*addr)
		return
	}

	apiKey := creds.MustGet("OPENAI_API_KEY")

	if *dbFile == "" {
		log.Fatal("-db flag is required (path to rules.db)")
	}
	db, err := rulesdb.Open(*dbFile)
	if err != nil {
		log.Fatalf("cannot open rules db: %v", err)
	}
	defer db.Close()

	if *promptFile == "" {
		log.Fatal("-prompt flag is required (path to system prompt template)")
	}
	promptTemplate, err := os.ReadFile(*promptFile)
	if err != nil {
		log.Fatalf("cannot read prompt file: %v", err)
	}

	if *publicDir == "" {
		log.Fatal("-public flag is required (path to pennies/public)")
	}

	limiter := ratelimit.New(*dataDir, *dailyCap)

	sgPath := filepath.Join(*dataDir, "suggestions.db")
	sg, err := suggestions.Open(sgPath)
	if err != nil {
		log.Fatalf("cannot open suggestions db: %v", err)
	}
	defer sg.Close()

	svc := arbiter.New(apiKey, string(promptTemplate), db, limiter, sg)

	mux := http.NewServeMux()
	if _, err := appd.RegisterNav(mux, *appsConfig); err != nil {
		log.Fatalf("cannot register nav: %v", err)
	}

	mux.HandleFunc("POST /ruling", svc.HandleRuling)
	mux.HandleFunc("POST /suggest", svc.HandleSuggest)
	mux.HandleFunc("GET /rulings", svc.HandleListRulings)
	mux.HandleFunc("DELETE /rulings/{id}", svc.HandleDeleteRuling)
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"status":"ok","version":"2026-06-29i"}`))
	})

	// Static files from pennies/public. API routes above take precedence.
	mux.Handle("/", http.FileServer(http.Dir(*publicDir)))

	var handler http.Handler = mux
	if *devMode {
		handler = corsMiddleware(mux)
	}

	srv := &http.Server{
		Addr:              *addr,
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      60 * time.Second,
		IdleTimeout:       120 * time.Second,
	}

	go func() {
		log.Printf("arbiterd listening on %s (dev=%v, cap=$%.2f/day)", *addr, *devMode, *dailyCap)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("listen error: %v", err)
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit
	log.Println("shutting down")

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := srv.Shutdown(ctx); err != nil {
		log.Fatalf("shutdown error: %v", err)
	}
}

// servePaused answers every method and path with the paused notice. It opens no
// database, reads no credential, and makes no OpenAI call. See issue #598.
func servePaused(addr string) {
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Retry-After", "3600")
		if strings.Contains(r.Header.Get("Accept"), "application/json") {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusServiceUnavailable)
			json.NewEncoder(w).Encode(map[string]string{
				"error": pausedMessage,
				"code":  "paused",
			})
			return
		}
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.WriteHeader(http.StatusServiceUnavailable)
		w.Write([]byte(pausedMessage + "\n"))
	})

	srv := &http.Server{
		Addr:              addr,
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      60 * time.Second,
		IdleTimeout:       120 * time.Second,
	}

	go func() {
		log.Printf("arbiterd paused on %s — %s", addr, pausedMessage)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("listen error: %v", err)
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit
	log.Println("shutting down")

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := srv.Shutdown(ctx); err != nil {
		log.Fatalf("shutdown error: %v", err)
	}
}

func corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		if strings.HasPrefix(origin, "http://localhost") || strings.HasPrefix(origin, "http://127.0.0.1") {
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Set("Access-Control-Allow-Methods", "POST, GET, DELETE, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type, X-Session-Token, X-Admin-Token")
			w.Header().Set("Access-Control-Allow-Credentials", "true")
		}
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}
