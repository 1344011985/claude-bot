package main

import (
	"context"
	"flag"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"qq-claude-bot/internal/bot"
	"qq-claude-bot/internal/claude"
	"qq-claude-bot/internal/command"
	"qq-claude-bot/internal/config"
	"qq-claude-bot/internal/feishu"
	"qq-claude-bot/internal/imageutil"
	"qq-claude-bot/internal/memory"
	"qq-claude-bot/pkg/logger"
)

// Version variables injected via ldflags at build time.
var (
	GitCommit = "unknown"
	BuildDate = "unknown"
)

func main() {
	channel := flag.String("channel", "qq", "messaging channel: qq or feishu")
	flag.Parse()

	// Inject version into command package
	command.GitCommit = GitCommit
	command.BuildDate = BuildDate

	// Load and validate config
	cfg, err := config.Load("config.yaml")
	if err != nil {
		slog.Error("failed to load config", "err", err)
		os.Exit(1)
	}
	if err := cfg.Validate(*channel); err != nil {
		slog.Error("invalid config", "err", err)
		os.Exit(1)
	}

	log := logger.New(cfg.LogLevel)

	// Ensure data directory exists
	if err := os.MkdirAll("data", 0755); err != nil {
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

	// Initialise Claude runner
	runner := claude.New(cfg.Claude.BinPath, cfg.Claude.TimeoutSeconds)

	// Initialise model selector
	selector := claude.NewModelSelector(cfg)

	// Initialise image downloader (nil if cache_dir not configured)
	downloader, err := imageutil.New(cfg.Images.CacheDir, cfg.Images.MaxSizeMB)
	if err != nil {
		log.Error("failed to init image downloader", "err", err)
		os.Exit(1)
	}
	if downloader != nil {
		log.Info("image support enabled", "cache_dir", cfg.Images.CacheDir)
	}

	// Build system prompt based on channel
	systemPrompt := cfg.SystemPrompt
	if *channel == "feishu" {
		systemPrompt = buildFeishuSystemPrompt(cfg)
	}

	// Initialise command router
	router := command.NewRouter(store, runner, downloader, selector, systemPrompt, log)

	// Context for graceful shutdown
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	log.Info("bot starting", "channel", *channel, "commit", GitCommit, "built", BuildDate)

	switch *channel {
	case "feishu":
		if err := feishu.Start(ctx, cfg, router, store, log); err != nil {
			log.Error("feishu bot exited with error", "err", err)
			os.Exit(1)
		}
	default:
		// QQ channel (original behavior)
		handler := bot.New(router, nil, cfg, log)
		if err := bot.Start(ctx, cfg, handler, log); err != nil {
			log.Error("qq bot exited with error", "err", err)
			os.Exit(1)
		}
	}
}

// buildFeishuSystemPrompt returns a cleaner system prompt for Feishu,
// removing QQ-specific restrictions (path/URL/filename interception, no markdown, etc.)
func buildFeishuSystemPrompt(cfg *config.Config) string {
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

## 能力范围
- 回答问题、写代码、分析文本、数学计算
- 查看和修改项目文件（需用户明确授权）
- 运行安全的测试命令

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
