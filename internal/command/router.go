package command

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"claude-bot/internal/claude"
	"claude-bot/internal/imageutil"
	"claude-bot/internal/memory"
	"claude-bot/internal/newsearch"
)

// modelSwitchPatterns matches natural language model switching requests.
var modelSwitchPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)(?:切换|换|改|设置|设定)(?:模型|model)?(?:为|到|成)\s*(haiku|sonnet|opus|auto)`),
	regexp.MustCompile(`(?i)(?:使用|用)\s*(haiku|sonnet|opus|auto)\s*(?:模型|model)?`),
	regexp.MustCompile(`(?i)(?:模型|model)\s*(?:切换|换|改|设置|设定)(?:为|到|成)?\s*(haiku|sonnet|opus|auto)`),
}

// modelDisplayNames maps model keys to friendly display names.
var modelDisplayNames = map[string]string{
	"haiku":  "Haiku (快速轻量)",
	"sonnet": "Sonnet (均衡)",
	"opus":   "Opus (最强)",
	"auto":   "自动选择",
}

// IncomingMessage holds the parsed incoming message from any channel.
type IncomingMessage struct {
	UserID     string
	GroupID    string       // empty for direct messages
	Content    string
	ImageURLs  []string     // attachment image URLs, may be empty
	ProgressFn func(string) // optional: called with partial AI output (for streaming)
}

// Handler processes an incoming message and returns a reply string.
type Handler interface {
	Handle(ctx context.Context, msg *IncomingMessage) (string, error)
}

// Logger is the minimal logging interface used by command handlers.
type Logger interface {
	Error(msg string, args ...any)
	Info(msg string, args ...any)
}

// Router dispatches messages to the appropriate Handler based on command prefix.
type Router struct {
	handlers map[string]Handler
	fallback Handler
	store    memory.Store
}

// Version info injected via ldflags from main package.
var (
	GitCommit = "unknown"
	BuildDate = "unknown"
)

// NewRouter wires up all command handlers.
func NewRouter(store memory.Store, runner *claude.Runner, downloader *imageutil.Downloader, selector *claude.ModelSelector, systemPrompt string, logger Logger) *Router {
	askH := &askHandler{
		store:        store,
		runner:       runner,
		downloader:   downloader,
		selector:     selector,
		systemPrompt: systemPrompt,
		logger:       logger,
	}
	newsSearcher := newsearch.NewSearcher()
	r := &Router{
		handlers: map[string]Handler{
			"/ask":      askH,
			"/new":      &newHandler{store: store},
			"/remember": &rememberHandler{store: store},
			"/forget":   &forgetHandler{store: store},
			"/history":  &historyHandler{store: store},
			"/help":     &helpHandler{},
			"/version":  &versionHandler{},
			"/news":     &newsHandler{searcher: newsSearcher, logger: logger},
		},
		fallback: askH,
		store:    store,
	}
	return r
}

// Route dispatches the message to the correct handler.
func (r *Router) Route(ctx context.Context, msg *IncomingMessage) (string, error) {
	// Pre-processing: check for model switch intent
	if model, ok := r.detectModelSwitch(msg.Content); ok {
		if err := r.store.SetModelPreference(msg.UserID, model); err != nil {
			return "模型切换失败，请稍后重试。", nil
		}
		displayName := modelDisplayNames[model]
		return fmt.Sprintf("已切换模型为 %s", displayName), nil
	}

	if !strings.HasPrefix(msg.Content, "/") {
		return r.fallback.Handle(ctx, msg)
	}

	parts := strings.SplitN(msg.Content, " ", 2)
	cmd := strings.ToLower(parts[0])

	h, ok := r.handlers[cmd]
	if !ok {
		return fmt.Sprintf("未知指令 %q，输入 /help 查看可用指令", cmd), nil
	}
	return h.Handle(ctx, msg)
}

// detectModelSwitch checks if the message is a model switching request.
func (r *Router) detectModelSwitch(content string) (string, bool) {
	trimmed := strings.TrimSpace(content)
	for _, pat := range modelSwitchPatterns {
		if matches := pat.FindStringSubmatch(trimmed); len(matches) >= 2 {
			model := strings.ToLower(matches[1])
			if _, ok := modelDisplayNames[model]; ok {
				return model, true
			}
		}
	}
	return "", false
}
