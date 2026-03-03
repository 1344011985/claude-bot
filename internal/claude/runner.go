package claude

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"runtime"
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

// buildCmd constructs the exec.Cmd for the given args, adapting for the current OS.
// On Windows: cmd /c <binPath> <args...>
// On Linux/macOS: <binPath> <args...>
func (r *Runner) buildCmd(ctx context.Context, args []string) *exec.Cmd {
	if runtime.GOOS == "windows" {
		// Prepend "cmd /c <binPath>" so the binary runs inside CMD environment
		cmdArgs := append([]string{"/c", r.binPath}, args...)
		return exec.CommandContext(ctx, "cmd", cmdArgs...)
	}
	return exec.CommandContext(ctx, r.binPath, args...)
}

// Run invokes the Claude CLI with the given prompt, optional sessionID, systemPrompt, and image paths.
// progressFn is called after 10 seconds if the subprocess is still running.
func (r *Runner) Run(ctx context.Context, prompt, sessionID, systemPrompt string, imagePaths []string, progressFn func(string)) (*RunResult, error) {
	return r.RunWithModel(ctx, prompt, sessionID, systemPrompt, imagePaths, "", progressFn)
}

// RunWithModel invokes the Claude CLI with a specific model.
func (r *Runner) RunWithModel(ctx context.Context, prompt, sessionID, systemPrompt string, imagePaths []string, modelName string, progressFn func(string)) (*RunResult, error) {
	args := []string{"-p", prompt, "--output-format", "json"}

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

	cmd := r.buildCmd(runCtx, args)

	// Fire progress callback after 10 seconds
	progressTimer := time.AfterFunc(10*time.Second, func() {
		if progressFn != nil {
			progressFn("")
		}
	})
	defer progressTimer.Stop()

	out, err := cmd.Output()
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
		// Capture stderr for non-zero exit
		var exitErr *exec.ExitError
		stderr := ""
		if e, ok := err.(*exec.ExitError); ok {
			exitErr = e
			if len(exitErr.Stderr) > 200 {
				stderr = string(exitErr.Stderr[:200])
			} else {
				stderr = string(exitErr.Stderr)
			}
		}
		return nil, fmt.Errorf("claude exited with error: %s", strings.TrimSpace(stderr))
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

