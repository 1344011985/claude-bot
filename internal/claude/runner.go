package claude

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"
)

// Runner invokes the Claude Code CLI as a subprocess.
type Runner struct {
	binPath string
	timeout time.Duration
}

// RunResult holds the parsed output from a Claude CLI invocation.
type RunResult struct {
	SessionID string
	Text      string
	Usage     *UsageInfo // Token usage information
}

// UsageInfo contains token usage statistics from the API response.
type UsageInfo struct {
	InputTokens         int `json:"input_tokens"`
	OutputTokens        int `json:"output_tokens"`
	CacheCreationTokens int `json:"cache_creation_input_tokens"`
	CacheReadTokens     int `json:"cache_read_input_tokens"`
}

// streamEvent matches lines from `claude --output-format stream-json`
type streamEvent struct {
	Type      string     `json:"type"`
	Result    string     `json:"result"`
	SessionID string     `json:"session_id"`
	Usage     *UsageInfo `json:"usage"`
	// For text delta events
	DeltaType string `json:"delta_type"`
	Delta     string `json:"delta"`
}

// New creates a Runner with the given binary path and timeout.
func New(binPath string, timeoutSeconds int) *Runner {
	return &Runner{
		binPath: binPath,
		timeout: time.Duration(timeoutSeconds) * time.Second,
	}
}

// Run invokes the Claude CLI with the given prompt, optional sessionID, systemPrompt, and image paths.
func (r *Runner) Run(ctx context.Context, prompt, sessionID, systemPrompt string, imagePaths []string, progressFn func(string)) (*RunResult, error) {
	return r.RunWithModel(ctx, prompt, sessionID, systemPrompt, imagePaths, "", progressFn)
}

// RunWithModel invokes the Claude CLI with a specific model, using stream-json for real-time progress.
// The prompt is passed via stdin (--print flag reads from stdin) to avoid shell
// encoding issues on Windows (cmd.exe defaults to GBK/CP936).
func (r *Runner) RunWithModel(ctx context.Context, prompt, sessionID, systemPrompt string, imagePaths []string, modelName string, progressFn func(string)) (*RunResult, error) {
	args := []string{"--permission-mode", "bypassPermissions", "--print", "--output-format", "stream-json", "--verbose", "--disallowed-tools", "WebSearch"}

	// Load MCP servers from settings.json if present
	if home, err := os.UserHomeDir(); err == nil {
		mcpConfig := home + "/.claude/settings.json"
		if _, err := os.Stat(mcpConfig); err == nil {
			args = append(args, "--mcp-config", mcpConfig)
		}
	}

	if modelName != "" {
		args = append(args, "--model", modelName)
	}
	if sessionID != "" {
		args = append(args, "--resume", sessionID)
	}
	if systemPrompt != "" {
		args = append(args, "--append-system-prompt", systemPrompt)
	}
	for _, p := range imagePaths {
		args = append(args, "--image", p)
	}

	// Apply timeout only if configured (timeout > 0)
	runCtx := ctx
	var cancel context.CancelFunc
	if r.timeout > 0 {
		runCtx, cancel = context.WithTimeout(ctx, r.timeout)
		defer cancel()
	}

	cmd := exec.CommandContext(runCtx, r.binPath, args...)
	cmd.Stdin = strings.NewReader(prompt)

	// Use pipe for stdout to read stream-json line by line
	stdoutPipe, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("stdout pipe: %w", err)
	}
	var stderrBuf bytes.Buffer
	cmd.Stderr = &stderrBuf

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start claude: %w", err)
	}

	// Read stream-json events line by line
	var (
		accumulated string
		finalResult *RunResult
	)

	scanner := bufio.NewScanner(stdoutPipe)
	scanner.Buffer(make([]byte, 1024*1024), 1024*1024) // 1MB buffer for large lines
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			continue
		}

		var event streamEvent
		if jsonErr := json.Unmarshal([]byte(line), &event); jsonErr != nil {
			continue
		}

		switch event.Type {
		case "assistant":
			// Assistant message with content blocks — extract text deltas
			// stream-json emits content as nested structure; try to parse text
			var raw map[string]json.RawMessage
			if jsonErr := json.Unmarshal([]byte(line), &raw); jsonErr == nil {
				if msgRaw, ok := raw["message"]; ok {
					var msg struct {
						Content []struct {
							Type string `json:"type"`
							Text string `json:"text"`
						} `json:"content"`
					}
					if jsonErr := json.Unmarshal(msgRaw, &msg); jsonErr == nil {
						for _, c := range msg.Content {
							if c.Type == "text" && c.Text != "" {
								accumulated += c.Text
								if progressFn != nil {
									safeProgress(progressFn, accumulated)
								}
							}
						}
					}
				}
			}
		case "result":
			// Final result event
			finalResult = &RunResult{
				SessionID: event.SessionID,
				Text:      event.Result,
				Usage:     event.Usage,
			}
		}
	}

	if err := cmd.Wait(); err != nil {
		if runCtx.Err() == context.DeadlineExceeded {
			return nil, fmt.Errorf("claude request timed out after %s", r.timeout)
		}
		// If we got a final result before the error, use it
		if finalResult != nil && finalResult.Text != "" {
			return finalResult, nil
		}
		errMsg := strings.TrimSpace(stderrBuf.String())
		if len(errMsg) > 1000 {
			errMsg = errMsg[:1000]
		}
		return nil, fmt.Errorf("claude exited with error: %s | args=%v", errMsg, args)
	}

	if finalResult != nil && finalResult.Text != "" {
		return finalResult, nil
	}

	// Fallback: use accumulated text if no result event
	if accumulated != "" {
		return &RunResult{Text: accumulated}, nil
	}

	return nil, fmt.Errorf("no result from claude")
}

// safeProgress calls progressFn with panic recovery.
func safeProgress(fn func(string), text string) {
	defer func() {
		if r := recover(); r != nil {
			_ = r
		}
	}()
	fn(text)
}

// legacyRun is kept for reference — uses --output-format json (non-streaming).
// Not used in production; use RunWithModel instead.
func legacyRun(binPath string, timeout time.Duration, ctx context.Context, prompt, sessionID, systemPrompt string, imagePaths []string, modelName string, progressFn func(string)) (*RunResult, error) {
	args := []string{"--permission-mode", "bypassPermissions", "--print", "--output-format", "json", "--disallowed-tools", "WebSearch"}
	if modelName != "" {
		args = append(args, "--model", modelName)
	}
	if sessionID != "" {
		args = append(args, "--resume", sessionID)
	}
	if systemPrompt != "" {
		args = append(args, "--append-system-prompt", systemPrompt)
	}
	for _, p := range imagePaths {
		args = append(args, "--image", p)
	}

	runCtx := ctx
	var cancel context.CancelFunc
	if timeout > 0 {
		runCtx, cancel = context.WithTimeout(ctx, timeout)
		defer cancel()
	}

	cmd := exec.CommandContext(runCtx, binPath, args...)
	cmd.Stdin = strings.NewReader(prompt)

	progressTimer := time.AfterFunc(10*time.Second, func() {
		defer func() { recover() }()
		if progressFn != nil {
			progressFn("")
		}
	})
	defer progressTimer.Stop()

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	out := stdout.Bytes()

	type claudeOutput struct {
		Type      string     `json:"type"`
		Result    string     `json:"result"`
		SessionID string     `json:"session_id"`
		Usage     *UsageInfo `json:"usage"`
	}

	if err != nil {
		if runCtx.Err() == context.DeadlineExceeded {
			return nil, fmt.Errorf("claude request timed out after %s", timeout)
		}
		if len(out) > 0 {
			var result claudeOutput
			if jsonErr := json.Unmarshal(out, &result); jsonErr == nil && result.Result != "" {
				return &RunResult{SessionID: result.SessionID, Text: result.Result, Usage: result.Usage}, nil
			}
		}
		errMsg := strings.TrimSpace(stderr.String())
		if len(errMsg) > 1000 {
			errMsg = errMsg[:1000]
		}
		return nil, fmt.Errorf("claude exited with error: %s | args=%v", errMsg, args)
	}

	var result claudeOutput
	if err := json.Unmarshal(out, &result); err != nil {
		return nil, fmt.Errorf("parse claude output: %w", err)
	}

	return &RunResult{SessionID: result.SessionID, Text: result.Result, Usage: result.Usage}, nil
}
