package browser

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/launcher"
	"github.com/go-rod/rod/lib/proto"
)

const (
	sessionTimeout  = 5 * time.Minute
	navigateTimeout = 30 * time.Second
)

// AriaNode represents a single interactive element extracted from the DOM.
type AriaNode struct {
	Ref      string // e1, e2, ...
	Role     string // button, link, input, textarea, select, checkbox, ...
	Name     string // accessible name / text
	Value    string // current value (for inputs)
	Selector string // JS path used internally
}

// Session holds a single user's browser page.
type Session struct {
	page     *rod.Page
	refMap   map[string]string // ref -> JS selector
	lastUsed time.Time
	mu       sync.Mutex
}

func (s *Session) touch() {
	s.mu.Lock()
	s.lastUsed = time.Now()
	s.mu.Unlock()
}

func (s *Session) close() {
	_ = s.page.Close()
}

// Manager owns a single rod.Browser and manages per-user pages.
type Manager struct {
	browser  *rod.Browser
	sessions map[string]*Session
	mu       sync.Mutex
	cacheDir string
	stopGC   chan struct{}
}

// NewManager creates a Manager. cacheDir stores screenshots.
func NewManager(cacheDir string) (*Manager, error) {
	if err := os.MkdirAll(cacheDir, 0755); err != nil {
		return nil, fmt.Errorf("create browser cache dir: %w", err)
	}

	u, err := launcher.New().
		Headless(true).
		Set("no-sandbox").
		Set("disable-gpu").
		Set("disable-dev-shm-usage").
		Set("lang", "zh-CN").
		Launch()
	if err != nil {
		return nil, fmt.Errorf("launch browser: %w", err)
	}

	b := rod.New().ControlURL(u).MustConnect()

	m := &Manager{
		browser:  b,
		sessions: make(map[string]*Session),
		cacheDir: cacheDir,
		stopGC:   make(chan struct{}),
	}
	go m.gcLoop()
	return m, nil
}

// Close shuts down all sessions and the underlying browser.
func (m *Manager) Close() {
	close(m.stopGC)
	m.mu.Lock()
	defer m.mu.Unlock()
	for id, s := range m.sessions {
		s.close()
		delete(m.sessions, id)
	}
	_ = m.browser.Close()
}

func (m *Manager) gcLoop() {
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			m.evictIdle()
		case <-m.stopGC:
			return
		}
	}
}

func (m *Manager) evictIdle() {
	m.mu.Lock()
	defer m.mu.Unlock()
	now := time.Now()
	for id, s := range m.sessions {
		s.mu.Lock()
		idle := now.Sub(s.lastUsed)
		s.mu.Unlock()
		if idle > sessionTimeout {
			s.close()
			delete(m.sessions, id)
		}
	}
}

func (m *Manager) getOrCreate(userID string) (*Session, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if s, ok := m.sessions[userID]; ok {
		s.touch()
		return s, nil
	}

	page, err := m.browser.Page(proto.TargetCreateTarget{URL: "about:blank"})
	if err != nil {
		return nil, fmt.Errorf("create page: %w", err)
	}
	if err := page.SetViewport(&proto.EmulationSetDeviceMetricsOverride{
		Width: 1280, Height: 800,
	}); err != nil {
		return nil, fmt.Errorf("set viewport: %w", err)
	}

	s := &Session{
		page:     page,
		refMap:   make(map[string]string),
		lastUsed: time.Now(),
	}
	m.sessions[userID] = s
	return s, nil
}

// Navigate opens url and waits for the page to load.
func (m *Manager) Navigate(userID, url string) error {
	s, err := m.getOrCreate(userID)
	if err != nil {
		return err
	}
	defer s.touch()

	if err := s.page.Navigate(url); err != nil {
		return fmt.Errorf("navigate: %w", err)
	}
	return s.page.WaitLoad()
}

// Screenshot takes a full-page screenshot and saves it to cacheDir.
func (m *Manager) Screenshot(userID string) (string, error) {
	s, err := m.getOrCreate(userID)
	if err != nil {
		return "", err
	}
	defer s.touch()

	quality := 90
	buf, err := s.page.Screenshot(true, &proto.PageCaptureScreenshot{
		Format:  proto.PageCaptureScreenshotFormatPng,
		Quality: &quality,
	})
	if err != nil {
		return "", fmt.Errorf("screenshot: %w", err)
	}

	name := fmt.Sprintf("screenshot_%d.png", time.Now().UnixNano())
	path := filepath.Join(m.cacheDir, name)
	if err := os.WriteFile(path, buf, 0644); err != nil {
		return "", fmt.Errorf("save screenshot: %w", err)
	}
	return path, nil
}

// GetText returns visible body text.
func (m *Manager) GetText(userID string) (string, error) {
	return m.EvalJS(userID, `document.body.innerText`)
}

// GetPageSource returns the full outer HTML.
func (m *Manager) GetPageSource(userID string) (string, error) {
	return m.EvalJS(userID, `document.documentElement.outerHTML`)
}

// GetCurrentURL returns the current page URL.
func (m *Manager) GetCurrentURL(userID string) (string, error) {
	return m.EvalJS(userID, `location.href`)
}

// EvalJS evaluates JS and returns the string result.
func (m *Manager) EvalJS(userID, js string) (string, error) {
	s, err := m.getOrCreate(userID)
	if err != nil {
		return "", err
	}
	defer s.touch()

	res, err := s.page.Eval(js)
	if err != nil {
		return "", fmt.Errorf("eval js: %w", err)
	}
	return res.Value.String(), nil
}

// --- Aria Snapshot ---

// ariaJS is the JS injected to extract interactive elements from the DOM.
const ariaJS = `(() => {
	const roles = ["button","link","input","textarea","select","checkbox","radio","menuitem","tab","option","combobox","listbox","slider","spinbutton","switch","textbox","searchbox"];
	const elements = [];
	const walker = document.createTreeWalker(document.body, NodeFilter.SHOW_ELEMENT);
	let node;
	while ((node = walker.nextNode())) {
		const tag = node.tagName.toLowerCase();
		const role = node.getAttribute("role") || "";
		const isInteractive = ["a","button","input","textarea","select"].includes(tag) || roles.includes(role);
		if (!isInteractive) continue;
		const rect = node.getBoundingClientRect();
		if (rect.width === 0 && rect.height === 0) continue;
		const name = (node.getAttribute("aria-label") || node.getAttribute("placeholder") || node.getAttribute("title") || node.textContent || "").trim().slice(0, 80);
		const value = node.value !== undefined ? String(node.value) : "";
		// build a simple unique selector: tag + index among same-tag siblings
		let idx = 0;
		let sib = node;
		while ((sib = sib.previousElementSibling)) { if (sib.tagName === node.tagName) idx++; }
		// Use xpath-style path
		const path = (() => {
			const parts = [];
			let el = node;
			while (el && el !== document.body) {
				let i = 1, s = el;
				while ((s = s.previousElementSibling)) i++;
				parts.unshift(el.tagName.toLowerCase() + ":nth-of-type(" + i + ")");
				el = el.parentElement;
			}
			return "body > " + parts.join(" > ");
		})();
		elements.push({ tag, role: role || tag, name, value, path });
	}
	return JSON.stringify(elements);
})()`

// AriaSnapshot extracts interactive elements, assigns e1/e2... refs, returns list.
func (m *Manager) AriaSnapshot(userID string) ([]AriaNode, error) {
	s, err := m.getOrCreate(userID)
	if err != nil {
		return nil, err
	}
	defer s.touch()

	res, err := s.page.Eval(ariaJS)
	if err != nil {
		return nil, fmt.Errorf("aria snapshot eval: %w", err)
	}

	// parse the JSON array
	var raw []struct {
		Tag   string `json:"tag"`
		Role  string `json:"role"`
		Name  string `json:"name"`
		Value string `json:"value"`
		Path  string `json:"path"`
	}
	if err := res.Value.Unmarshal(&raw); err != nil {
		return nil, fmt.Errorf("aria snapshot unmarshal: %w", err)
	}

	// reset refMap for this user
	s.mu.Lock()
	s.refMap = make(map[string]string)
	s.mu.Unlock()

	nodes := make([]AriaNode, 0, len(raw))
	for i, item := range raw {
		ref := fmt.Sprintf("e%d", i+1)
		s.mu.Lock()
		s.refMap[ref] = item.Path
		s.mu.Unlock()
		nodes = append(nodes, AriaNode{
			Ref:      ref,
			Role:     item.Role,
			Name:     item.Name,
			Value:    item.Value,
			Selector: item.Path,
		})
	}
	return nodes, nil
}

// FormatAriaSnapshot formats nodes as a concise text tree for AI/human consumption.
func FormatAriaSnapshot(nodes []AriaNode) string {
	var sb strings.Builder
	for _, n := range nodes {
		line := fmt.Sprintf("[%s] %s", n.Ref, n.Role)
		if n.Name != "" {
			line += ` "` + n.Name + `"`
		}
		if n.Value != "" {
			line += ` = "` + n.Value + `"`
		}
		sb.WriteString(line + "\n")
	}
	return sb.String()
}

// ClickRef clicks the element identified by a ref (e.g., "e3").
func (m *Manager) ClickRef(userID, ref string) error {
	s, err := m.getOrCreate(userID)
	if err != nil {
		return err
	}
	s.mu.Lock()
	selector, ok := s.refMap[ref]
	s.mu.Unlock()
	if !ok {
		return fmt.Errorf("ref %s not found — run AriaSnapshot first", ref)
	}
	defer s.touch()

	el, err := s.page.Element(selector)
	if err != nil {
		return fmt.Errorf("find element %s: %w", ref, err)
	}
	return el.Click(proto.InputMouseButtonLeft, 1)
}

// TypeRef clears and types text into the element identified by ref.
func (m *Manager) TypeRef(userID, ref, text string) error {
	s, err := m.getOrCreate(userID)
	if err != nil {
		return err
	}
	s.mu.Lock()
	selector, ok := s.refMap[ref]
	s.mu.Unlock()
	if !ok {
		return fmt.Errorf("ref %s not found — run AriaSnapshot first", ref)
	}
	defer s.touch()

	el, err := s.page.Element(selector)
	if err != nil {
		return fmt.Errorf("find element %s: %w", ref, err)
	}
	if err := el.SelectAllText(); err != nil {
		return fmt.Errorf("select all text in %s: %w", ref, err)
	}
	return el.Input(text)
}

// CloseSession forcibly removes a user's session.
func (m *Manager) CloseSession(userID string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if s, ok := m.sessions[userID]; ok {
		s.close()
		delete(m.sessions, userID)
	}
}
