package config

import (
	"testing"

	"pgregory.net/rapid"
)

// Feature: qq-claude-bot, Property 6: 超时边界强制
func TestProperty6_TimeoutBoundEnforced(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		maxTimeout := rapid.IntRange(1, 7200).Draw(t, "max_timeout")
		// timeout is strictly greater than max
		timeout := rapid.IntRange(maxTimeout+1, maxTimeout+10000).Draw(t, "timeout")

		cfg := defaultConfig()
		cfg.QQ.AppID = "test-app-id"
		cfg.QQ.AppSecret = "test-app-secret"
		cfg.Claude.TimeoutSeconds = timeout
		cfg.Claude.MaxTimeoutSeconds = maxTimeout

		err := cfg.Validate()
		if err == nil {
			t.Fatalf("expected Validate() to return error when timeout_seconds(%d) > max_timeout_seconds(%d), got nil",
				timeout, maxTimeout)
		}
	})
}

// Validate() should succeed when timeout <= max_timeout
func TestValidate_TimeoutWithinBounds(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		maxTimeout := rapid.IntRange(1, 7200).Draw(t, "max_timeout")
		timeout := rapid.IntRange(1, maxTimeout).Draw(t, "timeout")

		cfg := defaultConfig()
		cfg.QQ.AppID = "test-app-id"
		cfg.QQ.AppSecret = "test-app-secret"
		cfg.Claude.TimeoutSeconds = timeout
		cfg.Claude.MaxTimeoutSeconds = maxTimeout

		if err := cfg.Validate(); err != nil {
			t.Fatalf("expected no error when timeout(%d) <= max_timeout(%d), got: %v", timeout, maxTimeout, err)
		}
	})
}

// Validate() should fail when AppID is missing
func TestValidate_MissingAppID(t *testing.T) {
	cfg := defaultConfig()
	cfg.QQ.AppSecret = "secret"
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected error for missing AppID")
	}
}

// Validate() should fail when AppSecret is missing
func TestValidate_MissingAppSecret(t *testing.T) {
	cfg := defaultConfig()
	cfg.QQ.AppID = "id"
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected error for missing AppSecret")
	}
}
