package main

import (
	"flag"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/TrueBlocks/rulesforpennies.io/internal/arbiter"
	"github.com/TrueBlocks/rulesforpennies.io/internal/ratelimit"
	"github.com/TrueBlocks/rulesforpennies.io/internal/rulesdb"
	"github.com/TrueBlocks/rulesforpennies.io/internal/suggestions"
)

func main() {
	addr := flag.String("addr", ":9092", "listen address")
	dataDir := flag.String("data", "", "data directory (default: ~/.local/share/trueblocks/arbiterd)")
	dbFile := flag.String("db", "", "path to rules.db SQLite database")
	promptFile := flag.String("prompt", "", "path to system prompt template file")
	dailyCap := flag.Float64("daily-cap", 10.0, "daily spend cap in USD")
	devMode := flag.Bool("dev", false, "enable dev mode (CORS for localhost)")
	flag.Parse()

	if *dataDir == "" {
		home, _ := os.UserHomeDir()
		*dataDir = filepath.Join(home, ".local", "share", "trueblocks", "arbiterd")
	}
	if err := os.MkdirAll(*dataDir, 0755); err != nil {
		log.Fatalf("cannot create data dir: %v", err)
	}

	apiKey := os.Getenv("OPENAI_API_KEY")
	if apiKey == "" {
		log.Fatal("OPENAI_API_KEY environment variable is required")
	}

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

	limiter := ratelimit.New(*dataDir, *dailyCap)

	sgPath := filepath.Join(*dataDir, "suggestions.db")
	sg, err := suggestions.Open(sgPath)
	if err != nil {
		log.Fatalf("cannot open suggestions db: %v", err)
	}
	defer sg.Close()

	svc := arbiter.New(apiKey, string(promptTemplate), db, limiter, sg)

	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/ruling", svc.HandleRuling)
	mux.HandleFunc("POST /api/suggest", svc.HandleSuggest)
	mux.HandleFunc("GET /api/rulings", svc.HandleListRulings)
	mux.HandleFunc("DELETE /api/rulings/{id}", svc.HandleDeleteRuling)
	mux.HandleFunc("GET /api/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"status":"ok","version":"2026-06-29i"}`))
	})

	var handler http.Handler = mux
	if *devMode {
		handler = corsMiddleware(mux)
	}

	log.Printf("arbiterd listening on %s (dev=%v, cap=$%.2f/day)", *addr, *devMode, *dailyCap)
	log.Fatal(http.ListenAndServe(*addr, handler))
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
