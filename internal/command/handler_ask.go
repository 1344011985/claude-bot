package command

import (
	"context"
	"fmt"
	"strings"
	"time"

	"feishu-claude-bot/internal/claude"
	"feishu-claude-bot/internal/imageutil"
	"feishu-claude-bot/internal/memory"
)

// --- /ask handler ---

type askHandler struct {
	store        memory.Store
	runner       *claude.Runner
	downloader   *imageutil.Downloader
	selector     *claude.ModelSelector
	systemPrompt string
	logger       Logger
}

func (h *askHandler) Handle(ctx context.Context, msg *IncomingMessage) (string, error) {
	prompt := strings.TrimPrefix(msg.Content, "/ask")
	prompt = strings.TrimSpace(prompt)
	if prompt == "" {
		return "请提供问题内容，例如：/ask 你好", nil
	}

	sessionID, err := h.store.GetSession(msg.UserID)
	if err != nil {
		h.logger.Error("get session failed", "user_id", msg.UserID, "err", err)
		sessionID = ""
	}

	memories, err := h.store.GetMemories(msg.UserID)
	if err != nil {
		h.logger.Error("get memories failed", "user_id", msg.UserID, "err", err)
		memories = nil
	}

	var promptParts []string
	if h.systemPrompt != "" {
		promptParts = append(promptParts, h.systemPrompt)
	}
	if len(memories) > 0 {
		promptParts = append(promptParts, "## 用户个人记忆\n"+strings.Join(memories, "\n"))
	}
	systemPrompt := strings.Join(promptParts, "\n\n")

	var imagePaths []string
	if h.downloader != nil {
		for _, url := range msg.ImageURLs {
			path, err := h.downloader.Download(url)
			if err != nil {
				h.logger.Error("download image failed", "url", url, "err", err)
				continue
			}
			imagePaths = append(imagePaths, path)
		}
	}

	history, _ := h.store.GetHistory(msg.UserID, 100)
	conversationTurns := len(history)

	userPref, _ := h.store.GetModelPreference(msg.UserID)
	modelKey := h.selector.SelectModel(userPref, prompt, len(imagePaths), conversationTurns)
	modelName := h.selector.GetModelName(modelKey)

	h.logger.Info("selected model", "user_id", msg.UserID, "model", modelKey, "preference", userPref)

	// Run with selected model, pass progress callback for streaming
	result, err := h.runner.RunWithModel(ctx, prompt, sessionID, systemPrompt, imagePaths, modelName, msg.ProgressFn)
	if err != nil {
		return fmt.Sprintf("执行出错：%v", err), nil
	}

	if err := h.store.SaveSession(msg.UserID, result.SessionID); err != nil {
		h.logger.Error("save session failed", "user_id", msg.UserID, "err", err)
	}
	if err := h.store.SaveHistory(msg.UserID, prompt, result.Text); err != nil {
		h.logger.Error("save history failed", "user_id", msg.UserID, "err", err)
	}

	if result.Usage != nil {
		cost := h.selector.CalculateCost(
			modelKey,
			result.Usage.InputTokens,
			result.Usage.OutputTokens,
			result.Usage.CacheCreationTokens,
			result.Usage.CacheReadTokens,
		)
		usageRecord := &memory.UsageRecord{
			UserID:              msg.UserID,
			SessionID:           result.SessionID,
			Model:               modelKey,
			InputTokens:         result.Usage.InputTokens,
			OutputTokens:        result.Usage.OutputTokens,
			CacheCreationTokens: result.Usage.CacheCreationTokens,
			CacheReadTokens:     result.Usage.CacheReadTokens,
			TotalCostUSD:        cost,
			CreatedAt:           time.Now(),
		}
		if err := h.store.RecordUsage(usageRecord); err != nil {
			h.logger.Error("record usage failed", "err", err)
		}
	}

	return result.Text, nil
}
