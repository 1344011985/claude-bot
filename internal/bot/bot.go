package bot

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	botgo "github.com/tencent-connect/botgo"
	"github.com/tencent-connect/botgo/event"
	"github.com/tencent-connect/botgo/openapi"
	v1 "github.com/tencent-connect/botgo/openapi/v1"
	"github.com/tencent-connect/botgo/token"
	"golang.org/x/oauth2"

	"qq-claude-bot/internal/config"
)

// Start initialises the QQ bot: authenticates, registers event handlers,
// and starts the WebSocket session manager (which handles heartbeat and reconnect).
//
// This function blocks until ctx is cancelled or a fatal error occurs.
// All transient errors (token refresh failures, WS disconnects) are handled
// internally with retries — they will NOT cause the process to exit.
func Start(ctx context.Context, cfg *config.Config, handler *Handler, logger *slog.Logger) error {
	// Ensure v1 API is registered
	v1.Setup()

	// Build token source (handles automatic refresh)
	tokenSource := token.NewQQBotTokenSource(&token.QQBotCredentials{
		AppID:     cfg.QQ.AppID,
		AppSecret: cfg.QQ.AppSecret,
	})

	// StartRefreshAccessToken launches a background goroutine that panics after
	// 10 consecutive failures. We wrap it so that panic doesn't kill the process.
	refreshErr := safeStartRefreshToken(ctx, tokenSource, logger)
	if refreshErr != nil {
		return fmt.Errorf("initial token fetch failed: %w", refreshErr)
	}

	// Create OpenAPI client
	api := botgo.NewOpenAPI(cfg.QQ.AppID, tokenSource)
	handler.api = api

	// Fetch WebSocket gateway info
	wsAP, err := api.WS(ctx, nil, "")
	if err != nil {
		return fmt.Errorf("fetch websocket gateway: %w", err)
	}

	// 创建连接健康监控器
	monitor := newConnectionMonitor(api, logger)
	handler.setMonitor(monitor) // 让 handler 可以更新活动时间

	// Register event handlers
	intent := event.RegisterHandlers(
		event.GroupATMessageEventHandler(handler.OnGroupATMessage),
		event.C2CMessageEventHandler(handler.OnC2CMessage),
		event.ErrorNotifyHandler(func(err error) {
			// WS errors are logged but do not stop the bot — SDK handles reconnect
			logger.Error("websocket error", "err", err)
			monitor.recordError()
		}),
	)

	logger.Info("starting bot", "shard_count", wsAP.Shards, "intent", intent)

	// 启动连接健康监控
	go monitor.start(ctx)

	// Start session manager — handles WS connect, heartbeat, and exponential-backoff reconnect
	return botgo.NewSessionManager().Start(wsAP, tokenSource, &intent)
}

// safeStartRefreshToken calls token.StartRefreshAccessToken and recovers from
// any panic it may raise (the SDK panics after 10 consecutive token failures).
func safeStartRefreshToken(ctx context.Context, ts oauth2.TokenSource, logger *slog.Logger) (err error) {
	defer func() {
		if r := recover(); r != nil {
			logger.Error("token refresh goroutine panicked", "recover", r)
			err = fmt.Errorf("token refresh panic: %v", r)
		}
	}()
	return token.StartRefreshAccessToken(ctx, ts)
}

// connectionMonitor 监控 WebSocket 连接健康状态
type connectionMonitor struct {
	api          openapi.OpenAPI
	logger       *slog.Logger
	lastActivity time.Time
	lastError    time.Time
	errorCount   int
	mu           sync.RWMutex
}

func newConnectionMonitor(api openapi.OpenAPI, logger *slog.Logger) *connectionMonitor {
	return &connectionMonitor{
		api:          api,
		logger:       logger,
		lastActivity: time.Now(),
	}
}

// start 启动连接监控协程
func (m *connectionMonitor) start(ctx context.Context) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	m.logger.Info("connection monitor started", "check_interval", "30s")

	for {
		select {
		case <-ctx.Done():
			m.logger.Info("connection monitor stopped")
			return
		case <-ticker.C:
			m.checkHealth()
		}
	}
}

// checkHealth 检查连接健康状态
func (m *connectionMonitor) checkHealth() {
	m.mu.RLock()
	lastActivity := m.lastActivity
	lastError := m.lastError
	errorCount := m.errorCount
	m.mu.RUnlock()

	inactiveDuration := time.Since(lastActivity)

	// 超过 90 秒无活动，记录警告
	if inactiveDuration > 90*time.Second {
		m.logger.Warn("connection inactive",
			"inactive_duration", inactiveDuration,
			"last_activity", lastActivity,
			"error_count", errorCount,
		)
	}

	// 短时间内多次错误，记录警告
	if errorCount >= 3 && time.Since(lastError) < 5*time.Minute {
		m.logger.Warn("frequent connection errors",
			"error_count", errorCount,
			"last_error_ago", time.Since(lastError),
		)
	}

	// 正常情况下记录健康日志（降低频率，每 2 分钟一次）
	if inactiveDuration < 90*time.Second && errorCount < 3 {
		m.logger.Debug("connection healthy",
			"last_activity_ago", inactiveDuration,
			"error_count", errorCount,
		)
	}
}

// recordActivity 记录连接活动（每次收到消息时调用）
func (m *connectionMonitor) recordActivity() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.lastActivity = time.Now()
	// 收到消息后重置错误计数
	if m.errorCount > 0 {
		m.logger.Debug("connection recovered, resetting error count", "was", m.errorCount)
		m.errorCount = 0
	}
}

// recordError 记录连接错误
func (m *connectionMonitor) recordError() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.lastError = time.Now()
	m.errorCount++
}

// Ensure openapi.OpenAPI is used (compile check)
var _ openapi.OpenAPI = nil
