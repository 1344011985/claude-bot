package feishu

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	lark "github.com/larksuite/oapi-sdk-go/v3"
	larkcore "github.com/larksuite/oapi-sdk-go/v3/core"
	"github.com/larksuite/oapi-sdk-go/v3/event/dispatcher"
	larkim "github.com/larksuite/oapi-sdk-go/v3/service/im/v1"
	larkws "github.com/larksuite/oapi-sdk-go/v3/ws"

	"claude-bot/internal/command"
	"claude-bot/internal/config"
	"claude-bot/internal/memory"
)

// chatHistoryEntry is a single message in a group chat history buffer.
type chatHistoryEntry struct {
	SenderOpenID string
	SenderName   string
	Content      string
	Timestamp    time.Time
}

// Bot wraps the Feishu bot client and message handling.
type Bot struct {
	client    *lark.Client
	router    *command.Router
	cfg       *config.Config
	logger    *slog.Logger
	store     memory.Store // for persistent dedup

	// Group chat history buffers (key = chatID)
	historyMu sync.RWMutex
	histories  map[string][]chatHistoryEntry

	// Sender name cache (key = open_id)
	nameCacheMu sync.RWMutex
	nameCache   map[string]nameCacheEntry
}

type nameCacheEntry struct {
	name      string
	expiresAt time.Time
}

const (
	// botOpenID is the Feishu open_id of this bot, used to detect @mentions in group chats.
	botOpenID = "ou_ceefb0e03942a04947a292fa51846d42"

	maxGroupHistorySize = 5
	nameCacheTTL        = 10 * time.Minute
)

// Message dedup is handled by persistent SQLite store (memory.Store.CheckAndMarkSeen)

type feishuTextContent struct {
	Text string `json:"text"`
}

// Start initialises the Feishu bot via WebSocket long connection.
func Start(ctx context.Context, cfg *config.Config, router *command.Router, store memory.Store, logger *slog.Logger) error {
	appID := cfg.Feishu.AppID
	appSecret := cfg.Feishu.AppSecret

	client := lark.NewClient(appID, appSecret)

	bot := &Bot{
		client:    client,
		router:    router,
		cfg:       cfg,
		logger:    logger,
		store:     store,
		histories: make(map[string][]chatHistoryEntry),
		nameCache: make(map[string]nameCacheEntry),
	}

	eventDispatcher := dispatcher.NewEventDispatcher("", "").
		OnP2MessageReceiveV1(bot.onMessage).
		OnP2MessageReadV1(func(ctx context.Context, event *larkim.P2MessageReadV1) error {
			return nil
		})

	wsClient := larkws.NewClient(appID, appSecret,
		larkws.WithEventHandler(eventDispatcher),
		larkws.WithLogLevel(larkcore.LogLevelInfo),
	)

	logger.Info("feishu bot starting via WebSocket", "app_id", appID)
	return wsClient.Start(ctx)
}

// onMessage handles incoming messages from Feishu.
func (b *Bot) onMessage(ctx context.Context, event *larkim.P2MessageReceiveV1) error {
	if event == nil || event.Event == nil || event.Event.Message == nil {
		return nil
	}

	msg := event.Event.Message
	sender := event.Event.Sender

	userID := ""
	if sender != nil && sender.SenderId != nil && sender.SenderId.OpenId != nil {
		userID = *sender.SenderId.OpenId
	}
	if userID == "" {
		return nil
	}

	if !b.isAllowed(userID) {
		b.logger.Info("user not in allowlist, ignoring", "user_id", userID)
		return nil
	}

	msgType := ""
	if msg.MessageType != nil {
		msgType = *msg.MessageType
	}
	if msgType != "text" {
		b.logger.Info("ignoring non-text message", "type", msgType, "user_id", userID)
		return nil
	}

	content := ""
	if msg.Content != nil {
		var tc feishuTextContent
		if err := json.Unmarshal([]byte(*msg.Content), &tc); err == nil {
			content = tc.Text
		}
	}

	content = stripMentions(content, msg.Mentions)
	content = strings.TrimSpace(content)
	if content == "" {
		return nil
	}

	chatType := ""
	if msg.ChatType != nil {
		chatType = *msg.ChatType
	}
	isGroup := chatType == "group"

	chatID := ""
	if msg.ChatId != nil {
		chatID = *msg.ChatId
	}

	msgID := ""
	if msg.MessageId != nil {
		msgID = *msg.MessageId
	}

	if msgID != "" {
		alreadySeen, err := b.store.CheckAndMarkSeen(msgID, "feishu")
		if err != nil {
			b.logger.Warn("dedup check failed, proceeding", "err", err)
		} else if alreadySeen {
			b.logger.Info("duplicate message skipped (persistent dedup)", "msg_id", msgID, "user_id", userID)
			return nil
		}
	}

	// Skip stale messages (older than 2 minutes)
	if msg.CreateTime != nil {
		if ts, err := strconv.ParseInt(*msg.CreateTime, 10, 64); err == nil {
			msgTime := time.UnixMilli(ts)
			age := time.Since(msgTime)
			if age > 2*time.Minute {
				b.logger.Info("stale message skipped", "msg_id", msgID, "user_id", userID, "age", age.String())
				return nil
			}
		}
	}

	// Resolve sender display name (best-effort, cached)
	senderName := b.resolveSenderName(ctx, userID)

	b.logger.Info("feishu message received",
		"user_id", userID,
		"sender_name", senderName,
		"chat_type", chatType,
		"chat_id", chatID,
		"content", content,
	)

	// Send "eyes" reaction immediately to acknowledge receipt
	if msgID != "" {
		go b.addReaction(context.Background(), msgID, "EYES")
	}

	// Fetch quoted/replied message content if parent_id exists
	var quotedContent string
	if msg.ParentId != nil && *msg.ParentId != "" {
		quotedContent = b.fetchQuotedMessage(ctx, *msg.ParentId)
		if quotedContent != "" {
			b.logger.Info("fetched quoted message", "parent_id", *msg.ParentId, "preview", quotedContent[:min(len(quotedContent), 80)])
		}
	}

	// In group chats: record this message to history buffer, then decide if we should respond.
	// We respond when: (a) DM, (b) bot is @mentioned, (c) message starts with /
	shouldRespond := !isGroup
	if isGroup {
		// Record to history first
		b.recordHistory(chatID, userID, senderName, content)

		// Check if bot itself is @mentioned (not just any mention)
		botMentioned := false
		for _, mention := range msg.Mentions {
			if mention.Id != nil && mention.Id.OpenId != nil && *mention.Id.OpenId == botOpenID {
				botMentioned = true
				break
			}
		}
		if botMentioned || strings.HasPrefix(content, "/") {
			shouldRespond = true
		}
	}

	if !shouldRespond {
		b.logger.Info("group message recorded to history, not responding (no mention)", "chat_id", chatID)
		return nil
	}

	// Build enriched prompt: inject quoted content + group history context
	enrichedContent := b.buildEnrichedContent(content, quotedContent, chatID, isGroup, userID, senderName)

	// Determine receive_id and type for streaming card
	var receiveID, receiveIDType string
	if isGroup {
		receiveID = chatID
		receiveIDType = "chat_id"
	} else {
		receiveID = userID
		receiveIDType = "open_id"
	}

	// Try to start streaming card; fall back to Patch-based if unavailable
	stream := newStreamingSession(b.cfg.Feishu.AppID, b.cfg.Feishu.AppSecret, receiveID, receiveIDType,
		func(s string) { b.logger.Info(s) })

	var thinkingMsgID string
	if stream == nil {
		// Fallback: send Patch-based thinking card
		thinkingMsgID = b.sendThinkingCard(ctx, msgID)
	}

	// Dispatch to Claude with a progress callback that updates the streaming card
	var progressFn func(partial string)
	if stream != nil {
		progressFn = func(partial string) {
			stream.update(partial)
		}
	}

	reply := b.dispatchAsyncOrSync(ctx, userID, chatID, enrichedContent, progressFn)

	if stream != nil {
		stream.close(reply)
	} else if thinkingMsgID != "" {
		if err := b.patchCard(ctx, thinkingMsgID, reply); err != nil {
			b.logger.Error("patch card failed, sending new reply", "err", err)
			b.sendReply(ctx, msgID, reply)
		}
	} else {
		b.sendReply(ctx, msgID, reply)
	}

	// Replace "eyes" reaction with checkmark to indicate completion
	if msgID != "" {
		go func() {
			b.removeReaction(context.Background(), msgID, "EYES")
			b.addReaction(context.Background(), msgID, "DONE")
		}()
	}

	// Clear group history after responding (keeps context fresh)
	if isGroup {
		b.clearHistory(chatID)
	}

	return nil
}

// buildEnrichedContent constructs the full prompt sent to Claude,
// including quoted message context and group chat history.
func (b *Bot) buildEnrichedContent(content, quotedContent, chatID string, isGroup bool, userID, senderName string) string {
	var parts []string

	// Group history context (messages before this one)
	if isGroup {
		history := b.getHistory(chatID)
		if len(history) > 0 {
			var histLines []string
			for _, h := range history {
				name := h.SenderName
				if name == "" {
					name = h.SenderOpenID
				}
				histLines = append(histLines, fmt.Sprintf("[%s]: %s", name, h.Content))
			}
			parts = append(parts, "## 群聊上下文（最近消息）\n"+strings.Join(histLines, "\n"))
		}
	}

	// Quoted/replied message
	if quotedContent != "" {
		parts = append(parts, fmt.Sprintf("## 引用的消息\n%s", quotedContent))
	}

	// Sender attribution in group chats
	speaker := senderName
	if speaker == "" {
		speaker = userID
	}
	if isGroup {
		parts = append(parts, fmt.Sprintf("## 当前消息（来自 %s）\n%s", speaker, content))
	} else {
		parts = append(parts, content)
	}

	return strings.Join(parts, "\n\n")
}

// recordHistory appends a message to the group chat history buffer.
func (b *Bot) recordHistory(chatID, openID, name, content string) {
	b.historyMu.Lock()
	defer b.historyMu.Unlock()

	entry := chatHistoryEntry{
		SenderOpenID: openID,
		SenderName:   name,
		Content:      content,
		Timestamp:    time.Now(),
	}

	hist := b.histories[chatID]
	hist = append(hist, entry)
	if len(hist) > maxGroupHistorySize {
		hist = hist[len(hist)-maxGroupHistorySize:]
	}
	b.histories[chatID] = hist
}

// getHistory returns a copy of the group chat history (excluding last message which is current).
func (b *Bot) getHistory(chatID string) []chatHistoryEntry {
	b.historyMu.RLock()
	defer b.historyMu.RUnlock()

	hist := b.histories[chatID]
	if len(hist) == 0 {
		return nil
	}
	// Exclude the last entry (which is the current message we just recorded)
	if len(hist) == 1 {
		return nil
	}
	out := make([]chatHistoryEntry, len(hist)-1)
	copy(out, hist[:len(hist)-1])
	return out
}

// clearHistory clears the group chat history after a response.
func (b *Bot) clearHistory(chatID string) {
	b.historyMu.Lock()
	defer b.historyMu.Unlock()
	delete(b.histories, chatID)
}

// resolveSenderName looks up the display name for a Feishu user (with TTL cache).
func (b *Bot) resolveSenderName(ctx context.Context, openID string) string {
	b.nameCacheMu.RLock()
	if entry, ok := b.nameCache[openID]; ok && time.Now().Before(entry.expiresAt) {
		b.nameCacheMu.RUnlock()
		return entry.name
	}
	b.nameCacheMu.RUnlock()

	// Fetch from Feishu API
	name := b.fetchSenderName(ctx, openID)

	b.nameCacheMu.Lock()
	b.nameCache[openID] = nameCacheEntry{
		name:      name,
		expiresAt: time.Now().Add(nameCacheTTL),
	}
	b.nameCacheMu.Unlock()

	return name
}

// fetchSenderName calls the Feishu Contact API to get a user's display name.
func (b *Bot) fetchSenderName(ctx context.Context, openID string) string {
	token, err := getTenantAccessToken(b.cfg.Feishu.AppID, b.cfg.Feishu.AppSecret)
	if err != nil {
		return ""
	}

	url := fmt.Sprintf("https://open.feishu.cn/open-apis/contact/v3/users/%s?user_id_type=open_id", openID)
	req, _ := http.NewRequestWithContext(ctx, "GET", url, nil)
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return ""
	}
	defer resp.Body.Close()

	var result struct {
		Code int `json:"code"`
		Data struct {
			User struct {
				Name        string `json:"name"`
				DisplayName string `json:"display_name"`
				Nickname    string `json:"nickname"`
				EnName      string `json:"en_name"`
			} `json:"user"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return ""
	}
	if result.Code != 0 {
		b.logger.Debug("fetchSenderName api error", "code", result.Code, "open_id", openID)
		return ""
	}

	u := result.Data.User
	if u.Name != "" {
		return u.Name
	}
	if u.DisplayName != "" {
		return u.DisplayName
	}
	if u.Nickname != "" {
		return u.Nickname
	}
	return u.EnName
}

// fetchQuotedMessage retrieves the text content of a parent/quoted message.
func (b *Bot) fetchQuotedMessage(ctx context.Context, parentMsgID string) string {
	token, err := getTenantAccessToken(b.cfg.Feishu.AppID, b.cfg.Feishu.AppSecret)
	if err != nil {
		return ""
	}

	url := fmt.Sprintf("https://open.feishu.cn/open-apis/im/v1/messages/%s", parentMsgID)
	req, _ := http.NewRequestWithContext(ctx, "GET", url, nil)
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return ""
	}
	defer resp.Body.Close()

	var result struct {
		Code int `json:"code"`
		Data struct {
			Items []struct {
				MsgType string `json:"msg_type"`
				Body    struct {
					Content string `json:"content"`
				} `json:"body"`
			} `json:"items"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return ""
	}
	if result.Code != 0 || len(result.Data.Items) == 0 {
		return ""
	}

	item := result.Data.Items[0]
	if item.MsgType == "text" {
		var tc feishuTextContent
		if err := json.Unmarshal([]byte(item.Body.Content), &tc); err == nil {
			return tc.Text
		}
	}
	return item.Body.Content
}

// isAllowed checks if the user is in the allowlist (empty list = allow all).
func (b *Bot) isAllowed(userID string) bool {
	if len(b.cfg.Allowlist) == 0 {
		return true
	}
	for _, id := range b.cfg.Allowlist {
		if id == userID {
			return true
		}
	}
	return false
}

// dispatch routes the message with panic recovery.
// progressFn is called with partial text as Claude generates (may be nil).
func (b *Bot) dispatch(ctx context.Context, userID, chatID, content string, progressFn func(string)) (reply string) {
	defer func() {
		if r := recover(); r != nil {
			b.logger.Error("panic recovered in dispatch", "recover", r, "user_id", userID)
			reply = "处理消息时发生内部错误，请稍后重试。"
		}
	}()

	result, err := b.router.Route(ctx, &command.IncomingMessage{
		UserID:     userID,
		GroupID:    chatID,
		Content:    content,
		ProgressFn: progressFn,
	})
	if err != nil {
		b.logger.Error("dispatch error", "err", err)
		return "处理消息时发生错误，请稍后重试。"
	}
	return result
}

// buildCardJSON builds a Feishu interactive card JSON with the given markdown text.
func buildCardJSON(text string) string {
	card := map[string]interface{}{
		"config": map[string]interface{}{
			"wide_screen_mode": true,
		},
		"elements": []map[string]interface{}{
			{
				"tag": "div",
				"text": map[string]interface{}{
					"tag":     "lark_md",
					"content": text,
				},
			},
		},
	}
	b, _ := json.Marshal(card)
	return string(b)
}

// sendThinkingCard sends a "thinking" interactive card as a reply (fallback path).
func (b *Bot) sendThinkingCard(ctx context.Context, msgID string) string {
	if msgID == "" {
		return ""
	}

	cardJSON := buildCardJSON("")

	req := larkim.NewReplyMessageReqBuilder().
		MessageId(msgID).
		Body(larkim.NewReplyMessageReqBodyBuilder().
			MsgType("interactive").
			Content(cardJSON).
			Build()).
		Build()

	resp, err := b.client.Im.Message.Reply(ctx, req)
	if err != nil {
		b.logger.Warn("thinking card send failed", "msg_id", msgID, "err", err)
		return ""
	}
	if !resp.Success() {
		b.logger.Warn("thinking card api error", "code", resp.Code, "msg", resp.Msg)
		return ""
	}
	if resp.Data != nil && resp.Data.MessageId != nil {
		return *resp.Data.MessageId
	}
	return ""
}

// patchCard updates an existing interactive card message with new content.
func (b *Bot) patchCard(ctx context.Context, msgID, content string) error {
	if msgID == "" || content == "" {
		return fmt.Errorf("empty msgID or content")
	}

	cardJSON := buildCardJSON(content)

	req := larkim.NewPatchMessageReqBuilder().
		MessageId(msgID).
		Body(larkim.NewPatchMessageReqBodyBuilder().
			Content(cardJSON).
			Build()).
		Build()

	resp, err := b.client.Im.Message.Patch(ctx, req)
	if err != nil {
		return fmt.Errorf("patch request failed: %w", err)
	}
	if !resp.Success() {
		return fmt.Errorf("patch api error: code=%d msg=%s", resp.Code, resp.Msg)
	}
	b.logger.Info("feishu reply sent (card updated)", "msg_id", msgID)
	return nil
}

// sendReply sends a reply to a specific message (fallback path).
func (b *Bot) sendReply(ctx context.Context, msgID, content string) error {
	if msgID == "" || content == "" {
		return nil
	}

	const maxLen = 30000
	chunks := splitFeishuMessage(content, maxLen)

	for i, chunk := range chunks {
		label := ""
		if len(chunks) > 1 {
			label = fmt.Sprintf("(%d/%d)\n", i+1, len(chunks))
		}

		textJSON, _ := json.Marshal(map[string]string{"text": label + chunk})

		req := larkim.NewReplyMessageReqBuilder().
			MessageId(msgID).
			Body(larkim.NewReplyMessageReqBodyBuilder().
				MsgType("text").
				Content(string(textJSON)).
				Build()).
			Build()

		resp, err := b.client.Im.Message.Reply(ctx, req)
		if err != nil {
			b.logger.Error("feishu reply failed", "msg_id", msgID, "chunk", i+1, "err", err)
			continue
		}
		if !resp.Success() {
			b.logger.Error("feishu reply api error", "code", resp.Code, "msg", resp.Msg)
			continue
		}

		if resp.Data != nil && resp.Data.MessageId != nil {
			msgID = *resp.Data.MessageId
		}

		b.logger.Info("feishu reply sent", "chunk", i+1, "total", len(chunks))
	}
	return nil
}

// stripMentions removes @bot mention placeholders from the text.
func stripMentions(text string, mentions []*larkim.MentionEvent) string {
	if len(mentions) == 0 {
		return text
	}
	for _, m := range mentions {
		if m.Key != nil {
			text = strings.ReplaceAll(text, *m.Key, "")
		}
	}
	return text
}

// splitFeishuMessage splits content into chunks of maxLen runes.
func splitFeishuMessage(content string, maxLen int) []string {
	runes := []rune(content)
	if len(runes) <= maxLen {
		return []string{content}
	}
	var chunks []string
	for len(runes) > 0 {
		end := maxLen
		if end > len(runes) {
			end = len(runes)
		}
		chunks = append(chunks, string(runes[:end]))
		runes = runes[end:]
	}
	return chunks
}

// addReaction adds an emoji reaction to a message.
func (b *Bot) addReaction(ctx context.Context, msgID, emojiType string) {
	token, err := getTenantAccessToken(b.cfg.Feishu.AppID, b.cfg.Feishu.AppSecret)
	if err != nil {
		return
	}
	body, _ := json.Marshal(map[string]interface{}{
		"reaction_type": map[string]string{"emoji_type": emojiType},
	})
	url := fmt.Sprintf("https://open.feishu.cn/open-apis/im/v1/messages/%s/reactions", msgID)
	req, _ := http.NewRequestWithContext(ctx, "POST", url, strings.NewReader(string(body)))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		b.logger.Warn("add reaction failed", "emoji", emojiType, "err", err)
		return
	}
	defer resp.Body.Close()
	var res struct {
		Code int    `json:"code"`
		Msg  string `json:"msg"`
	}
	if err2 := json.NewDecoder(resp.Body).Decode(&res); err2 == nil {
		if res.Code == 0 {
			b.logger.Info("reaction added", "emoji", emojiType, "msg_id", msgID)
		} else {
			b.logger.Warn("add reaction api error", "emoji", emojiType, "code", res.Code, "msg", res.Msg)
		}
	}
}

// removeReaction removes the bot own emoji reaction from a message.
func (b *Bot) removeReaction(ctx context.Context, msgID, emojiType string) {
	token, err := getTenantAccessToken(b.cfg.Feishu.AppID, b.cfg.Feishu.AppSecret)
	if err != nil {
		return
	}
	// List reactions to find our reaction_id
	listURL := fmt.Sprintf("https://open.feishu.cn/open-apis/im/v1/messages/%s/reactions?reaction_type=%s", msgID, emojiType)
	req, _ := http.NewRequestWithContext(ctx, "GET", listURL, nil)
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return
	}
	defer resp.Body.Close()

	type reactionItem struct {
		ReactionID string `json:"reaction_id"`
		Operator   struct {
			OperatorType string `json:"operator_type"`
		} `json:"operator"`
	}
	var listRes struct {
		Code int `json:"code"`
		Data struct {
			Items []reactionItem `json:"items"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&listRes); err != nil || listRes.Code != 0 {
		return
	}
	for _, item := range listRes.Data.Items {
		if item.Operator.OperatorType == "app" && item.ReactionID != "" {
			delURL := fmt.Sprintf("https://open.feishu.cn/open-apis/im/v1/messages/%s/reactions/%s", msgID, item.ReactionID)
			delReq, _ := http.NewRequestWithContext(ctx, "DELETE", delURL, nil)
			delReq.Header.Set("Authorization", "Bearer "+token)
			http.DefaultClient.Do(delReq)
			b.logger.Info("reaction removed", "emoji", emojiType, "msg_id", msgID)
			return
		}
	}
}

