package command

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"claude-bot/internal/browser"
	"claude-bot/internal/claude"
	"claude-bot/internal/imageutil"
	"claude-bot/internal/memory"
	"claude-bot/internal/newsearch"
	"claude-bot/internal/skills"
	"claude-bot/internal/taskqueue"
)

var modelSwitchPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)(?:切换|换|改|设置|设定)(?:模型|model)?(?:为|到|成)\s*(haiku|sonnet|opus|auto)`),
	regexp.MustCompile(`(?i)(?:使用|用)\s*(haiku|sonnet|opus|auto)\s*(?:模型|model)?`),
	regexp.MustCompile(`(?i)(?:模型|model)\s*(?:切换|换|改|设置|设定)(?:为|到|成)?\s*(haiku|sonnet|opus|auto)`),
}

var modelDisplayNames = map[string]string{
	"haiku":  "Haiku (快速轻量)",
	"sonnet": "Sonnet (均衡)",
	"opus":   "Opus (最强)",
	"auto":   "自动选择",
}

type IncomingMessage struct {
	UserID     string
	GroupID    string
	Content    string
	ImageURLs  []string
	ProgressFn func(string)
}

type Handler interface {
	Handle(ctx context.Context, msg *IncomingMessage) (string, error)
}

type Logger interface {
	Error(msg string, args ...any)
	Info(msg string, args ...any)
}

type Router struct {
	handlers  map[string]Handler
	fallback  Handler
	store     memory.Store
	tasks     taskqueue.Queue
	skillsHub *skills.Hub
}

var (
	GitCommit = "unknown"
	BuildDate = "unknown"
)

func NewRouter(store memory.Store, runner *claude.Runner, downloader *imageutil.Downloader, selector *claude.ModelSelector, systemPrompt string, logger Logger, hub *skills.Hub, browserMgr *browser.Manager, queue taskqueue.Queue) *Router {
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
		fallback:  askH,
		store:     store,
		tasks:     queue,
		skillsHub: hub,
	}

	if queue != nil {
		r.handlers["/tasks"] = &tasksHandler{queue: queue}
		r.handlers["/status"] = &statusHandler{queue: queue}
		r.handlers["/cancel"] = &cancelHandler{queue: queue}
	}
	if hub != nil {
		r.handlers["/skill"] = &skillHandler{store: hub.Store()}
	}
	if browserMgr != nil {
		r.handlers["/browse"] = &browseHandler{
			manager:  browserMgr,
			runner:   runner,
			selector: selector,
			store:    store,
			logger:   logger,
		}
	}
	return r
}

func (r *Router) Route(ctx context.Context, msg *IncomingMessage) (string, error) {
	if model, ok := r.detectModelSwitch(msg.Content); ok {
		if err := r.store.SetModelPreference(msg.UserID, model); err != nil {
			return "模型切换失败，请稍后重试。", nil
		}
		return fmt.Sprintf("已切换模型为 %s", modelDisplayNames[model]), nil
	}

	if !strings.HasPrefix(msg.Content, "/") {
		h := r.fallback
		if r.skillsHub != nil {
			if askH, ok := h.(*askHandler); ok {
				augmented := r.skillsHub.Augment(askH.systemPrompt, msg.Content)
				h = askH.withSystemPrompt(augmented)
			}
		}
		return h.Handle(ctx, msg)
	}

	parts := strings.SplitN(msg.Content, " ", 2)
	cmd := strings.ToLower(parts[0])
	if cmd == "/ask" {
		h := r.handlers["/ask"]
		if r.skillsHub != nil {
			if askH, ok := h.(*askHandler); ok {
				content := msg.Content
				if len(parts) > 1 {
					content = parts[1]
				}
				augmented := r.skillsHub.Augment(askH.systemPrompt, content)
				h = askH.withSystemPrompt(augmented)
			}
		}
		return h.Handle(ctx, msg)
	}

	h, ok := r.handlers[cmd]
	if !ok {
		return fmt.Sprintf("未知指令 %q，输入 /help 查看可用指令", cmd), nil
	}
	return h.Handle(ctx, msg)
}

func (r *Router) Tasks() taskqueue.Queue {
	return r.tasks
}

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
