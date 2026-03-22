package main

import (
	"context"
	"flag"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"claude-bot/internal/browser"
	"claude-bot/internal/claude"
	"claude-bot/internal/command"
	"claude-bot/internal/config"
	"claude-bot/internal/feishu"
	"claude-bot/internal/httpbridge"
	"claude-bot/internal/imageutil"
	"claude-bot/internal/memory"
	"claude-bot/internal/skills"
	"claude-bot/internal/taskqueue"
	"claude-bot/pkg/logger"
)

var (
	GitCommit = "unknown"
	BuildDate = "unknown"
)

func main() {
	channelFlag := flag.String("channel", "", "override channel from config (e.g. feishu)")
	configFlag := flag.String("config", "", "override config file path")
	flag.Parse()

	command.GitCommit = GitCommit
	command.BuildDate = BuildDate

	cfgPath := *configFlag
	if cfgPath == "" {
		var err error
		cfgPath, err = config.ConfigPath()
		if err != nil {
			slog.Error("failed to resolve config path", "err", err)
			os.Exit(1)
		}
	}

	slog.Info("starting claude-bot", "platform", config.Platform(), "config_path", cfgPath)

	cfg, err := config.LoadFrom(cfgPath)
	if err != nil {
		slog.Error("failed to load config", "err", err)
		os.Exit(1)
	}
	if *channelFlag != "" {
		cfg.Channel = *channelFlag
	}
	if err := cfg.Validate(); err != nil {
		slog.Error("invalid config", "err", err)
		os.Exit(1)
	}

	log := logger.New(cfg.LogLevel, cfg.ConfigDir)
	log.Info("config loaded", "platform", config.Platform(), "config_path", cfgPath, "config_dir", cfg.ConfigDir, "channel", cfg.Channel)

	if err := os.MkdirAll(cfg.ConfigDir+"/data", 0755); err != nil {
		log.Error("failed to create data directory", "err", err)
		os.Exit(1)
	}

	store, err := memory.NewSQLiteStore(cfg.Memory.DBPath)
	if err != nil {
		log.Error("failed to open memory store", "err", err)
		os.Exit(1)
	}
	defer func() {
		if err := store.Close(); err != nil {
			log.Error("error closing memory store", "err", err)
		}
	}()

	runner := claude.New(cfg.Claude.BinPath, cfg.Claude.TimeoutSeconds)
	selector := claude.NewModelSelector(cfg)

	downloader, err := imageutil.New(cfg.Images.CacheDir, cfg.Images.MaxSizeMB)
	if err != nil {
		log.Error("failed to init image downloader", "err", err)
		os.Exit(1)
	}
	if downloader != nil {
		log.Info("image support enabled", "cache_dir", cfg.Images.CacheDir)
	}

	var skillsHub *skills.Hub
	if skillStore, err := skills.NewSQLiteSkillStore(store.DB()); err != nil {
		log.Warn("failed to init skills store, skills disabled", "err", err)
	} else {
		skillsHub = skills.NewHub(skillStore)
		log.Info("skills hub enabled")
	}

	var browserMgr *browser.Manager
	browserCacheDir := cfg.ConfigDir + "/data/browser_cache"
	if bm, err := browser.NewManager(browserCacheDir); err != nil {
		log.Warn("failed to init browser manager, /browse disabled", "err", err)
	} else {
		browserMgr = bm
		log.Info("browser manager enabled", "cache_dir", browserCacheDir)
		defer browserMgr.Close()
	}

	systemPrompt := buildSystemPrompt(cfg)
	queue, err := taskqueue.New(store.DB(), store, runner, downloader, selector, systemPrompt, log, 2)
	if err != nil {
		log.Error("failed to init task queue", "err", err)
		os.Exit(1)
	}
	router := command.NewRouter(store, runner, downloader, selector, systemPrompt, log, skillsHub, browserMgr, queue)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	bridge := httpbridge.New("127.0.0.1:9191", queue, log)
	go func() {
		if err := bridge.Start(); err != nil {
			log.Error("http bridge exited with error", "err", err)
		}
	}()
	defer func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		if err := bridge.Shutdown(shutdownCtx); err != nil {
			log.Warn("http bridge shutdown failed", "err", err)
		}
	}()

	log.Info("bot starting", "channel", cfg.Channel, "commit", GitCommit, "built", BuildDate)

	switch cfg.Channel {
	case "feishu":
		if err := feishu.Start(ctx, cfg, router, store, log); err != nil {
			log.Error("feishu bot exited with error", "err", err)
			os.Exit(1)
		}
	default:
		log.Error("unsupported channel", "channel", cfg.Channel)
		os.Exit(1)
	}
}

func buildSystemPrompt(cfg *config.Config) string {
	if cfg.SystemPrompt != "" {
		return cfg.SystemPrompt
	}
	return `你是一个运行在飞书机器人上的 AI 助手，由 Claude 驱动。

## 行为准则
- 用中文回复，除非用户明确要求其他语言
- 可以使用 Markdown 格式（标题、列表、代码块），飞书支持渲染
- 回复内容尽量清晰有条理，善用格式化提升可读性
- 直接给出核心答案，避免冗长的铺垫

## 安全边界（严格遵守）
- 禁止执行任何删除、格式化、清空数据的危险操作
- 禁止访问或泄露配置文件中的敏感信息（AppID、AppSecret 等）
- 禁止访问工作目录以外的系统文件
- 禁止安装软件、修改系统配置、创建计划任务
- 如果用户要求执行危险操作，礼貌拒绝并说明原因

## 可用命令
- /ask <问题> —— 向 Claude 提问（续接上下文）
- /new —— 开启新对话，清除当前 session
- /remember <内容> —— 保存长期记忆
- /forget —— 清除所有长期记忆
- /history [n] —— 查看最近 n 条对话
- /tasks —— 查看最近任务
- /status <task_id> —— 查看任务状态
- /cancel <task_id> —— 取消任务
- /news [关键词] —— 搜索最新新闻
- /help —— 显示帮助
- /version —— 显示版本信息
- 直接发消息等同于 /ask`
}
