package main

import (
	"context"
	"flag"
	"log/slog"
	"os"
	"os/signal"
	"syscall"


	"claude-bot/internal/browser"
	"claude-bot/internal/claude"
	"claude-bot/internal/command"
	"claude-bot/internal/config"
	"claude-bot/internal/feishu"
	"claude-bot/internal/imageutil"
	"claude-bot/internal/memory"
	"claude-bot/internal/skills"
	"claude-bot/pkg/logger"
)

// Version variables injected via ldflags at build time.
var (
	GitCommit = "unknown"
	BuildDate = "unknown"
)

func main() {
	channelFlag := flag.String("channel", "", "override channel from config (e.g. feishu)")
	configFlag := flag.String("config", "", "override config file path")
	flag.Parse()

	// Inject version into command package
	command.GitCommit = GitCommit
	command.BuildDate = BuildDate

	// Resolve config path
	cfgPath := *configFlag
	if cfgPath == "" {
		var err error
		cfgPath, err = config.ConfigPath()
		if err != nil {
			slog.Error("failed to resolve config path", "err", err)
			os.Exit(1)
		}
	}

	// Log platform and config path before logger is fully initialised
	slog.Info("starting claude-bot",
		"platform", config.Platform(),
		"config_path", cfgPath,
	)

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

	log.Info("config loaded",
		"platform", config.Platform(),
		"config_path", cfgPath,
		"config_dir", cfg.ConfigDir,
		"channel", cfg.Channel,
	)

	// Ensure data directory exists
	if err := os.MkdirAll(cfg.ConfigDir+"/data", 0755); err != nil {
		log.Error("failed to create data directory", "err", err)
		os.Exit(1)
	}

	// Initialise memory store
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

	// Skills Hub (optional — failure is non-fatal, hub stays nil)
	var skillsHub *skills.Hub
	if skillStore, err := skills.NewSQLiteSkillStore(store.DB()); err != nil {
		log.Warn("failed to init skills store, skills disabled", "err", err)
	} else {
		skillsHub = skills.NewHub(skillStore)
		log.Info("skills hub enabled")
	}

	// Browser Manager (optional — failure is non-fatal, /browse stays disabled)
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
	router := command.NewRouter(store, runner, downloader, selector, systemPrompt, log, skillsHub, browserMgr)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

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

// buildSystemPrompt returns the system prompt to inject into every Claude call.
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
- /ask <问题> — 向 Claude 提问（续接上下文）
- /new — 开启新对话，清除当前 session
- /remember <内容> — 保存长期记忆
- /forget — 清除所有长期记忆
- /history [n] — 查看最近 n 条对话
- /news [关键词] — 搜索最新新闻
- /help — 显示帮助
- /version — 显示版本信息
- 直接发消息等同于 /ask`
}
