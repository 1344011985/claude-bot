package claude

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"testing"

	"pgregory.net/rapid"
)

// Feature: qq-claude-bot, Property 5: Runner 命令构建正确性
func TestProperty5_BuildCmdCorrectness(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		r := New("claude", 300)

		sessionID := rapid.StringMatching(`[a-zA-Z0-9]{1,40}`).Draw(rt, "sessionID")
		systemPrompt := rapid.StringMatching(`[a-zA-Z0-9 ]{0,50}`).Draw(rt, "systemPrompt")

		args := []string{"-p", "hello", "--output-format", "json"}
		if sessionID != "" {
			args = append(args, "--resume", sessionID)
		}
		if systemPrompt != "" {
			args = append(args, "--append-system-prompt", systemPrompt)
		}

		cmd := r.buildCmd(context.Background(), args)
		cmdStr := strings.Join(cmd.Args, " ")

		if sessionID != "" {
			if !strings.Contains(cmdStr, "--resume") || !strings.Contains(cmdStr, sessionID) {
				rt.Fatalf("expected --resume %s in cmd args, got: %s", sessionID, cmdStr)
			}
		}
		if systemPrompt != "" {
			if !strings.Contains(cmdStr, "--append-system-prompt") {
				rt.Fatalf("expected --append-system-prompt in cmd args, got: %s", cmdStr)
			}
		}

		// On Windows the first arg should be "cmd"
		if runtime.GOOS == "windows" {
			if cmd.Args[0] != "cmd" {
				rt.Fatalf("expected cmd.Args[0]='cmd' on Windows, got %q", cmd.Args[0])
			}
		} else {
			if cmd.Args[0] != "claude" {
				rt.Fatalf("expected cmd.Args[0]='claude' on non-Windows, got %q", cmd.Args[0])
			}
		}
	})
}

// --- Unit tests for Run() ---

// writeFakeClaudeBin writes a small script/exe that outputs the given JSON and exits with exitCode.
// Returns the path to the binary.
func writeFakeClaudeScript(t *testing.T, output string, exitCode int) string {
	t.Helper()
	dir := t.TempDir()

	if runtime.GOOS == "windows" {
		// Write a .bat file
		bat := dir + "\\claude.bat"
		content := "@echo off\n"
		if output != "" {
			content += "echo " + output + "\n"
		}
		content += "exit /b " + string(rune('0'+exitCode)) + "\n"
		if err := os.WriteFile(bat, []byte(content), 0755); err != nil {
			t.Fatalf("write bat: %v", err)
		}
		return bat
	}

	// Write a shell script
	sh := dir + "/claude"
	content := "#!/bin/sh\n"
	if output != "" {
		content += "printf '%s' '" + output + "'\n"
	}
	content += "exit " + string(rune('0'+exitCode)) + "\n"
	if err := os.WriteFile(sh, []byte(content), 0755); err != nil {
		t.Fatalf("write sh: %v", err)
	}
	return sh
}

// TestRun_JSONParsing verifies that Run() correctly extracts result and session_id.
func TestRun_JSONParsing(t *testing.T) {
	want := claudeOutput{
		Type:      "result",
		Result:    "Hello from Claude",
		SessionID: "sess-abc123",
	}
	jsonBytes, _ := json.Marshal(want)

	bin := writeFakeClaudeScript(t, string(jsonBytes), 0)
	r := New(bin, 30)

	res, err := r.Run(context.Background(), "hi", "", "", nil, nil)
	if err != nil {
		t.Fatalf("Run() error: %v", err)
	}
	if res.Text != want.Result {
		t.Errorf("Text: want %q got %q", want.Result, res.Text)
	}
	if res.SessionID != want.SessionID {
		t.Errorf("SessionID: want %q got %q", want.SessionID, res.SessionID)
	}
}

// TestRun_NonZeroExit verifies that a non-zero exit code returns an error.
func TestRun_NonZeroExit(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake script exit codes unreliable in this test setup on Windows")
	}
	bin := writeFakeClaudeScript(t, "", 1)
	r := New(bin, 30)

	_, err := r.Run(context.Background(), "hi", "", "", nil, nil)
	if err == nil {
		t.Fatal("expected error for non-zero exit, got nil")
	}
}

// TestRun_Timeout verifies that exceeding timeout returns a timeout error.
func TestRun_Timeout(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("sleep command differs on Windows")
	}
	// Use a script that sleeps longer than the timeout
	dir := t.TempDir()
	sh := dir + "/claude"
	if err := os.WriteFile(sh, []byte("#!/bin/sh\nsleep 10\n"), 0755); err != nil {
		t.Fatalf("write sh: %v", err)
	}

	r := New(sh, 1) // 1 second timeout
	_, err := r.Run(context.Background(), "hi", "", "", nil, nil)
	if err == nil {
		t.Fatal("expected timeout error, got nil")
	}
	if !strings.Contains(err.Error(), "timed out") {
		t.Errorf("expected 'timed out' in error, got: %v", err)
	}
}

// Ensure buildCmd is importable (compile check)
var _ = (*exec.Cmd)(nil)
