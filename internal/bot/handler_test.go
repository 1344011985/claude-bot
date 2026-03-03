package bot

import (
	"testing"

	"pgregory.net/rapid"
	"qq-claude-bot/internal/config"
)

// Feature: qq-claude-bot, Property 7: Allowlist 过滤一致性
// For any non-empty allowlist, a userID not in the list should be rejected.
func TestProperty7_AllowlistFiltering(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		// Generate a non-empty allowlist of 1-5 user IDs
		n := rapid.IntRange(1, 5).Draw(rt, "n")
		allowlist := make([]string, n)
		for i := 0; i < n; i++ {
			allowlist[i] = rapid.StringMatching(`allowed[0-9]{1,4}`).Draw(rt, "allowed_id")
		}

		// Generate a userID that is NOT in the allowlist
		outsiderID := rapid.StringMatching(`outsider[0-9]{1,4}`).Draw(rt, "outsider_id")

		cfg := &config.Config{}
		cfg.Allowlist = allowlist

		h := &Handler{cfg: cfg}

		// The outsider should not be allowed
		if h.isAllowed(outsiderID) {
			rt.Fatalf("expected outsider %q to be blocked by allowlist %v", outsiderID, allowlist)
		}

		// Every member of the allowlist should be allowed
		for _, id := range allowlist {
			if !h.isAllowed(id) {
				rt.Fatalf("expected allowlist member %q to be allowed", id)
			}
		}
	})
}

// Empty allowlist means all users are allowed.
func TestAllowlist_EmptyAllowsAll(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		userID := rapid.StringMatching(`[a-z0-9]{1,20}`).Draw(rt, "userID")

		cfg := &config.Config{}
		cfg.Allowlist = nil

		h := &Handler{cfg: cfg}
		if !h.isAllowed(userID) {
			rt.Fatalf("expected all users to be allowed with empty allowlist, got blocked for %q", userID)
		}
	})
}
