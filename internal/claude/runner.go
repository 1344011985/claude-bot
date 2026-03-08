package claude

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
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

// claudeOutput matches the JSON structure emitted by `claude --output-format json`.
type claudeOutput struct {
	Type      string     `json:"type"`
	Result    string     `json:"result"`
	SessionID string     `json:"session_id"`
	Usage     *UsageInfo `json:"usage"`
}

// New creates a Runner with the given binary path and timeout.
func New(binPath string, timeoutSeconds int) *Runner {
	return &Runner{
		binPath: binPath,
		timeout: time.Duration(timeoutSeconds) * time.Second,
	}
}

// Run invokes the Claude CLI with the given prompt, optional sessionID, systemPrompt, and image paths.
// progressFn is called after 10 seconds if the subprocess is still running.
func (r *Runner) Run(ctx context.Context, prompt, sessionID, systemPrompt string, imagePaths []string, progressFn func(string)) (*RunResult, error) {
	return r.RunWithModel(ctx, prompt, sessionID, systemPrompt, imagePaths, "", progressFn)
}

// RunWithModel invokes the Claude CLI with a specific model.
// The prompt is passed via stdin (--print flag reads from stdin) to avoid shell
// encoding issues on Windows (cmd.exe defaults to GBK/CP936).
func (r *Runner) RunWithModel(ctx context.Context, prompt, sessionID, systemPrompt string, imagePaths []string, modelName string, progressFn func(string)) (*RunResult, error) {
	// Use --print to read prompt from stdin, avoiding shell encoding issues on Windows.
	args := []string{"--print", "--output-format", "json"}

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

	// Invoke the binary directly (not via cmd /c) so UTF-8 is preserved on all platforms.
	cmd := exec.CommandContext(runCtx, r.binPath, args...)
	cmd.Stdin = strings.NewReader(prompt)

	// Fire progress callback after 10 seconds
	progressTimer := time.AfterFunc(10*time.Second, func() {
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

	if err != nil {
		if runCtx.Err() == context.DeadlineExceeded {
			return nil, fmt.Errorf("claude request timed out after %s", r.timeout)
		}
		// Even on non-zero exit, try to parse stdout — claude may have written
		// a valid JSON result before exiting with an error code.
		if len(out) > 0 {
			var result claudeOutput
			if jsonErr := json.Unmarshal(out, &result); jsonErr == nil && result.Result != "" {
				return &RunResult{
					SessionID: result.SessionID,
					Text:      result.Result,
					Usage:     result.Usage,
				}, nil
			}
		}
		errMsg := strings.TrimSpace(stderr.String())
		if len(errMsg) > 200 {
			errMsg = errMsg[:200]
		}
		return nil, fmt.Errorf("claude exited with error: %s", errMsg)
	}

	var result claudeOutput
	if err := json.Unmarshal(out, &result); err != nil {
		return nil, fmt.Errorf("parse claude output: %w", err)
	}

	return &RunResult{
		SessionID: result.SessionID,
		Text:      result.Result,
		Usage:     result.Usage,
	}, nil
}