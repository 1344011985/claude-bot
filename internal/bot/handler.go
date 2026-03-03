package bot

import (
	"context"
	"fmt"
	"log/slog"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/tencent-connect/botgo/dto"
	"github.com/tencent-connect/botgo/openapi"

	"qq-claude-bot/internal/command"
	"qq-claude-bot/internal/config"
)

// Handler handles incoming QQ messages and dispatches them to the command router.
type Handler struct {
	router  *command.Router
	api     openapi.OpenAPI
	cfg     *config.Config
	logger  *slog.Logger
	monitor activityRecorder // 连接监控器（在 bot.go 中定义）
}

// activityRecorder 接口（避免循环依赖）
type activityRecorder interface {
	recordActivity()
}

// setMonitor 设置连接监控器
func (h *Handler) setMonitor(monitor activityRecorder) {
	h.monitor = monitor
}

// New creates a new bot Handler.
func New(router *command.Router, api openapi.OpenAPI, cfg *config.Config, logger *slog.Logger) *Handler {
	return &Handler{
		router: router,
		api:    api,
		cfg:    cfg,
		logger: logger,
	}
}

// isAllowed returns true if the userID is permitted to use the bot.
func (h *Handler) isAllowed(userID string) bool {
	if len(h.cfg.Allowlist) == 0 {
		return true
	}
	for _, id := range h.cfg.Allowlist {
		if id == userID {
			return true
		}
	}
	return false
}

// anchorState tracks the original user msg_id and per-anchor seq counter.
// We always use the original user msg_id — bot's own msg_id expires in ~20s and cannot be reused.
type anchorState struct {
	mu    sync.Mutex
	msgID string // original user msg_id, never changes
	seq   uint32 // next seq to use
}

func newAnchorState(msgID string) *anchorState {
	return &anchorState{msgID: msgID, seq: 2}
}

// next returns the current anchor msgID and atomically increments seq, returning the seq to use.
func (a *anchorState) next() (msgID string, seq uint32) {
	a.mu.Lock()
	defer a.mu.Unlock()
	seq = a.seq
	a.seq++
	return a.msgID, seq
}

// update is a no-op: we never change the anchor away from the original user msg_id.
func (a *anchorState) update(_ string) {}

// get returns the current anchor msgID.
func (a *anchorState) get() string {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.msgID
}

// OnGroupATMessage handles GROUP_AT_MESSAGE_CREATE events.
func (h *Handler) OnGroupATMessage(payload *dto.WSPayload, data *dto.WSGroupATMessageData) error {
	// 记录连接活动
	if h.monitor != nil {
		h.monitor.recordActivity()
	}

	msg := (*dto.Message)(data)
	userID := ""
	if msg.Author != nil {
		userID = msg.Author.ID
	}
	groupID := msg.GroupID
	content := strings.TrimSpace(msg.Content)
	msgID := msg.ID
	eventID := payload.EventID

	var imageURLs []string
	for _, att := range msg.Attachments {
		if att.ContentType != "" && strings.HasPrefix(att.ContentType, "image/") {
			imageURLs = append(imageURLs, att.URL)
		}
	}

	h.logger.Info("group message received", "user_id", userID, "group_id", groupID, "content", content, "images", len(imageURLs))

	if !h.isAllowed(userID) {
		h.logger.Info("user not in allowlist, ignoring", "user_id", userID)
		return nil
	}

	// Send "thinking" immediately (seq=1). Anchor stays on the original user msg_id.
	h.sendGroupThinking(groupID, msgID, eventID)
	anchor := newAnchorState(msgID)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Keepalive: every 3 minutes send a heartbeat using the current anchor.
	// On success, update anchor to the new bot msg_id (resets seq to 2).
	var keepaliveSeq uint32 = 1 // independent counter just for keepalive display dots
	stopKeepalive := h.startKeepalive(ctx, 180, func() {
		atomic.AddUint32(&keepaliveSeq, 1)
		dots := strings.Repeat(".", int(atomic.LoadUint32(&keepaliveSeq))%3+1)
		text := fmt.Sprintf("⏳ 仍在处理中%s", dots)
		curMsgID, seq := anchor.next()
		out := &dto.MessageToCreate{Content: text, MsgType: dto.TextMsg, MsgID: curMsgID, MsgSeq: seq}
		sent, err := h.api.PostGroupMessage(context.Background(), groupID, out)
		if err != nil {
			h.logger.Warn("keepalive failed", "group_id", groupID, "seq", seq, "err", err)
		} else if sent != nil {
			h.logger.Info("keepalive ok, new anchor", "group_id", groupID, "new_msg_id", sent.ID)
			anchor.update(sent.ID)
		}
	})

	reply, err := h.dispatch(context.Background(), userID, groupID, content, imageURLs)
	stopKeepalive()

	if err != nil {
		h.logger.Error("dispatch error", "err", err)
		reply = "处理消息时发生错误，请稍后重试。"
	}

	return h.sendGroupReply(groupID, reply, anchor)
}

// OnC2CMessage handles C2C_MESSAGE_CREATE events.
func (h *Handler) OnC2CMessage(payload *dto.WSPayload, data *dto.WSC2CMessageData) error {
	// 记录连接活动
	if h.monitor != nil {
		h.monitor.recordActivity()
	}

	msg := (*dto.Message)(data)
	userID := ""
	if msg.Author != nil {
		userID = msg.Author.ID
	}
	content := strings.TrimSpace(msg.Content)
	msgID := msg.ID
	eventID := payload.EventID

	var imageURLs []string
	for _, att := range msg.Attachments {
		if att.ContentType != "" && strings.HasPrefix(att.ContentType, "image/") {
			imageURLs = append(imageURLs, att.URL)
		}
	}

	h.logger.Info("c2c message received", "user_id", userID, "content", content, "images", len(imageURLs))

	if !h.isAllowed(userID) {
		h.logger.Info("user not in allowlist, ignoring", "user_id", userID)
		return nil
	}

	h.sendC2CThinking(userID, msgID, eventID)
	anchor := newAnchorState(msgID)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var keepaliveSeq uint32 = 1
	stopKeepalive := h.startKeepalive(ctx, 180, func() {
		atomic.AddUint32(&keepaliveSeq, 1)
		dots := strings.Repeat(".", int(atomic.LoadUint32(&keepaliveSeq))%3+1)
		text := fmt.Sprintf("⏳ 仍在处理中%s", dots)
		curMsgID, seq := anchor.next()
		out := &dto.MessageToCreate{Content: text, MsgType: dto.TextMsg, MsgID: curMsgID, MsgSeq: seq}
		sent, err := h.api.PostC2CMessage(context.Background(), userID, out)
		if err != nil {
			h.logger.Warn("keepalive failed", "user_id", userID, "seq", seq, "err", err)
		} else if sent != nil {
			h.logger.Info("keepalive ok, new anchor", "user_id", userID, "new_msg_id", sent.ID)
			anchor.update(sent.ID)
		}
	})

	reply, err := h.dispatch(context.Background(), userID, "", content, imageURLs)
	stopKeepalive()

	if err != nil {
		h.logger.Error("dispatch error", "err", err)
		reply = "处理消息时发生错误，请稍后重试。"
	}

	return h.sendC2CReply(userID, reply, anchor)
}

// startKeepalive fires fn every intervalSec seconds until the returned stop() is called.
func (h *Handler) startKeepalive(ctx context.Context, intervalSec int, fn func()) func() {
	done := make(chan struct{})
	go func() {
		ticker := time.NewTicker(time.Duration(intervalSec) * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				fn()
			case <-done:
				return
			case <-ctx.Done():
				return
			}
		}
	}()
	return func() { close(done) }
}

// dispatch routes the message through the command router.
func (h *Handler) dispatch(ctx context.Context, userID, groupID, content string, imageURLs []string) (reply string, err error) {
	defer func() {
		if r := recover(); r != nil {
			h.logger.Error("panic recovered in dispatch", "recover", r, "user_id", userID)
			reply = "处理消息时发生内部错误，请稍后重试。"
			err = nil
		}
	}()
	return h.router.Route(ctx, &command.IncomingMessage{
		UserID:    userID,
		GroupID:   groupID,
		Content:   content,
		ImageURLs: imageURLs,
	})
}

// sendGroupThinking sends seq=1 "thinking" message.
func (h *Handler) sendGroupThinking(groupID, msgID, eventID string) {
	out := &dto.MessageToCreate{Content: "⏳ 正在思考中，请稍候...", MsgType: dto.TextMsg, MsgID: msgID, MsgSeq: 1}
	if _, err := h.api.PostGroupMessage(context.Background(), groupID, out); err == nil {
		h.logger.Info("thinking sent via msg_id")
		return
	}
	if eventID != "" {
		out2 := &dto.MessageToCreate{Content: "⏳ 正在思考中，请稍候...", MsgType: dto.TextMsg, EventID: eventID}
		if _, err2 := h.api.PostGroupMessage(context.Background(), groupID, out2); err2 == nil {
			h.logger.Info("thinking sent via event_id")
			return
		}
	}
	h.logger.Warn("thinking send failed", "group_id", groupID)
}

// sendC2CThinking sends seq=1 "thinking" message for C2C.
func (h *Handler) sendC2CThinking(userID, msgID, eventID string) {
	out := &dto.MessageToCreate{Content: "⏳ 正在思考中，请稍候...", MsgType: dto.TextMsg, MsgID: msgID, MsgSeq: 1}
	if _, err := h.api.PostC2CMessage(context.Background(), userID, out); err == nil {
		h.logger.Info("thinking sent via msg_id")
		return
	}
	if eventID != "" {
		out2 := &dto.MessageToCreate{Content: "⏳ 正在思考中，请稍候...", MsgType: dto.TextMsg, EventID: eventID}
		if _, err2 := h.api.PostC2CMessage(context.Background(), userID, out2); err2 == nil {
			h.logger.Info("thinking sent via event_id")
			return
		}
	}
	h.logger.Warn("thinking send failed", "user_id", userID)
}

// windowsPathRe matches Windows absolute paths like D:\foo\bar.py
var windowsPathRe = regexp.MustCompile(`[A-Za-z]:[\\\/][^\s"'` + "`" + `]+`)

// fileNameRe matches filenames with extensions QQ treats as URLs (e.g. search_server_v2.py, bot.go)
var fileNameRe = regexp.MustCompile(`([\w\-]+)\.(py|go|exe|sh|bat|ps1|js|ts|json|yaml|yml|toml|md|txt|log|db|sql)\b`)

// extDesc maps file extensions to human-readable descriptions
var extDesc = map[string]string{
	"py":   "Python文件",
	"go":   "Go文件",
	"exe":  "可执行文件",
	"sh":   "Shell脚本",
	"bat":  "批处理文件",
	"ps1":  "PowerShell脚本",
	"js":   "JS文件",
	"ts":   "TS文件",
	"json": "JSON文件",
	"yaml": "YAML配置文件",
	"yml":  "YAML配置文件",
	"toml": "TOML配置文件",
	"md":   "文档文件",
	"txt":  "文本文件",
	"log":  "日志文件",
	"db":   "数据库文件",
	"sql":  "SQL文件",
}

const maxMsgLen = 2000

func sanitizeForQQ(content string) string {
	lines := strings.Split(content, "\n")
	var out []string
	for _, line := range lines {
		// Replace Windows absolute paths
		if windowsPathRe.MatchString(line) {
			line = windowsPathRe.ReplaceAllStringFunc(line, func(m string) string {
				parts := strings.FieldsFunc(m, func(r rune) bool { return r == '\\' || r == '/' })
				if len(parts) > 0 {
					return "[" + parts[len(parts)-1] + "]"
				}
				return "[path]"
			})
		}
		// Replace bare filenames with extensions that QQ flags as URLs
		line = fileNameRe.ReplaceAllStringFunc(line, func(m string) string {
			parts := fileNameRe.FindStringSubmatch(m)
			if len(parts) >= 3 {
				name := parts[1]
				ext := parts[2]
				if desc, ok := extDesc[ext]; ok {
					return "「" + name + "」" + desc
				}
			}
			return m
		})
		out = append(out, line)
	}
	return strings.Join(out, "\n")
}

func splitMessage(content string) []string {
	runes := []rune(content)
	if len(runes) <= maxMsgLen {
		return []string{content}
	}
	var chunks []string
	for len(runes) > 0 {
		end := maxMsgLen
		if end > len(runes) {
			end = len(runes)
		}
		chunks = append(chunks, string(runes[:end]))
		runes = runes[end:]
	}
	return chunks
}

// sendGroupReply sends the final reply in chunks using the anchor state.
func (h *Handler) sendGroupReply(groupID, content string, anchor *anchorState) error {
	content = sanitizeForQQ(content)
	chunks := splitMessage(content)
	for i, chunk := range chunks {
		label := ""
		if len(chunks) > 1 {
			label = fmt.Sprintf("(%d/%d)\n", i+1, len(chunks))
		}
		curMsgID, seq := anchor.next()
		out := &dto.MessageToCreate{Content: label + chunk, MsgType: dto.TextMsg, MsgID: curMsgID, MsgSeq: seq}
		sent, err := h.api.PostGroupMessage(context.Background(), groupID, out)
		if err != nil {
			h.logger.Error("group reply failed", "group_id", groupID, "seq", seq, "err", err)
			continue
		}
		if sent != nil {
			anchor.update(sent.ID)
		}
	}
	return nil
}

// sendC2CReply sends the final reply in chunks using the anchor state.
func (h *Handler) sendC2CReply(userID, content string, anchor *anchorState) error {
	content = sanitizeForQQ(content)
	chunks := splitMessage(content)
	for i, chunk := range chunks {
		label := ""
		if len(chunks) > 1 {
			label = fmt.Sprintf("(%d/%d)\n", i+1, len(chunks))
		}
		curMsgID, seq := anchor.next()
		out := &dto.MessageToCreate{Content: label + chunk, MsgType: dto.TextMsg, MsgID: curMsgID, MsgSeq: seq}
		sent, err := h.api.PostC2CMessage(context.Background(), userID, out)
		if err != nil {
			h.logger.Error("c2c reply failed", "user_id", userID, "seq", seq, "err", err)
			continue
		}
		if sent != nil {
			anchor.update(sent.ID)
		}
	}
	return nil
}
