// cmd/apitest/main.go
// Lightweight HTTP server for testing bot logic without Feishu.
// Usage: go run ./cmd/apitest -port 8080
// curl -X POST http://localhost:8080/chat -d '{"user":"test","msg":"/browse https://example.com"}'
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"

	"claude-bot/internal/browser"
	"claude-bot/internal/claude"
	"claude-bot/internal/command"
	"claude-bot/internal/config"
	"claude-bot/internal/imageutil"
	"claude-bot/internal/memory"
	"claude-bot/internal/skills"
	"claude-bot/pkg/logger"
)

type chatRequest struct {
	User string `json:"user"`
	Msg  string `json:"msg"`
}

type chatResponse struct {
	Reply string `json:"reply,omitempty"`
	Error string `json:"error,omitempty"`
}

func main() {
	port := flag.Int("port", 8080, "HTTP listen port")
	configPath := flag.String("config", "", "path to claude-bot.json (default: auto-detect)")
	flag.Parse()

	// Resolve config path
	cfgPath := *configPath
	if cfgPath == "" {
		var err error
		cfgPath, err = config.ConfigPath()
		if err != nil {
			slog.Error("failed to resolve config path", "err", err)
			os.Exit(1)
		}
	}

	cfg, err := config.LoadFrom(cfgPath)
	if err != nil {
		slog.Error("failed to load config", "err", err)
		os.Exit(1)
	}
	// apitest does not need a real channel; override Validate by setting feishu stub
	if cfg.Channel == "" {
		cfg.Channel = "feishu"
	}

	log := logger.New(cfg.LogLevel, cfg.ConfigDir)

	// Ensure data directory
	dataDir := filepath.Join(cfg.ConfigDir, "data")
	if err := os.MkdirAll(dataDir, 0755); err != nil {
		log.Error("failed to create data directory", "err", err)
		os.Exit(1)
	}

	store, err := memory.NewSQLiteStore(cfg.Memory.DBPath)
	if err != nil {
		log.Error("failed to open memory store", "err", err)
		os.Exit(1)
	}
	defer store.Close() //nolint:errcheck

	runner := claude.New(cfg.Claude.BinPath, cfg.Claude.TimeoutSeconds)
	selector := claude.NewModelSelector(cfg)

	downloader, err := imageutil.New(cfg.Images.CacheDir, cfg.Images.MaxSizeMB)
	if err != nil {
		log.Warn("image support disabled", "err", err)
	}

	var skillsHub *skills.Hub
	if skillStore, err := skills.NewSQLiteSkillStore(store.DB()); err != nil {
		log.Warn("skills disabled", "err", err)
	} else {
		skillsHub = skills.NewHub(skillStore)
	}

	var browserMgr *browser.Manager
	browserCacheDir := filepath.Join(cfg.ConfigDir, "data", "browser_cache")
	if bm, err := browser.NewManager(browserCacheDir); err != nil {
		log.Warn("browser disabled", "err", err)
	} else {
		browserMgr = bm
		defer browserMgr.Close()
	}

	systemPrompt := "你是一个测试用的 AI 助手，由 Claude 驱动。"
	router := command.NewRouter(store, runner, downloader, selector, systemPrompt, log, skillsHub, browserMgr)

	http.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
	})

	http.HandleFunc("/chat", func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if rec := recover(); rec != nil {
				log.Error("panic in /chat handler", "panic", fmt.Sprintf("%v", rec))
				writeJSON(w, chatResponse{Error: fmt.Sprintf("internal panic: %v", rec)})
			}
		}()
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		var req chatRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSON(w, chatResponse{Error: "invalid JSON: " + err.Error()})
			return
		}
		if req.User == "" {
			req.User = "apitest_user"
		}
		if req.Msg == "" {
			writeJSON(w, chatResponse{Error: "msg is required"})
			return
		}

		msg := &command.IncomingMessage{
			UserID:  req.User,
			Content: req.Msg,
		}
		reply, err := router.Route(context.Background(), msg)
		if err != nil {
			writeJSON(w, chatResponse{Error: err.Error()})
			return
		}
		writeJSON(w, chatResponse{Reply: reply})
	})

	addr := fmt.Sprintf(":%d", *port)
	log.Info("apitest server starting", "addr", addr)
	if err := http.ListenAndServe(addr, nil); err != nil {
		log.Error("server error", "err", err)
		os.Exit(1)
	}
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}
