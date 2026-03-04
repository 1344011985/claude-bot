# CLAUDE.md - myselfClow Project Context

## Project Overview
Go-based Feishu bot powered by Claude Code as the AI backend.
Key directories: `internal/` (feishu/claude/command/config/memory), `cmd/bot/`, `dist/`, `data/`
Build: `go build -o dist/claude-bot ./cmd/bot/`
Run: `./dist/claude-bot -channel feishu`

## Work Style

### Be Proactive, Not Reactive
- When given a vague task ("fix the bug", "improve this"), **explore first, ask later**
- Read relevant source files before asking clarifying questions
- If you can make a reasonable assumption, make it and proceed — mention the assumption, don't block on it
- Only ask when genuinely blocked by missing info you cannot infer

### Progress Reporting
- For multi-step tasks: output a brief progress marker after each meaningful step
  - Format: `[Step X/N] description — done`
- Don't wait until the end to report everything
- If something fails mid-way, report it immediately with what you tried

### Decision Making
- **Prefer action over asking** for reversible changes (code edits, file writes)
- **Ask before acting** for irreversible or high-risk operations (deleting data, external API calls with side effects)
- When multiple approaches exist, pick the most pragmatic one and explain your choice in one line

### Code Quality
- Fix the root cause, not just the symptom
- If you notice a related issue while fixing something else, mention it (but don't fix unsolicited)
- Compile and test after changes: `go build ./...` then `go test ./...`
- Always check build succeeds before declaring done

## Key Technical Context

### Architecture
- **Feishu channel**: `internal/feishu/` — uses larksuite/oapi-sdk-go, WebSocket
- **Claude runner**: `internal/claude/runner.go` — spawns claude CLI subprocess, captures JSON output
- **Command router**: `internal/command/router.go` — dispatches /commands and free-text to handlers
- **Memory store**: `internal/memory/store.go` — SQLite-backed sessions, memories, history, dedup

### Current State (as of 2026-02-28)
- Feishu: streaming card (CardKit), group history context, quoted message context, sender name resolution
- Dedup: persistent SQLite (`message_dedup` table), survives restarts
- Reactions: 👀 on receive, ✅ on complete (requires `im:message.reaction:write` permission)
- CardKit streaming requires `cardkit:card:write` permission (not yet granted)

### Common Gotchas
- Chinese strings in Go files: use Python to write files, not PowerShell (encoding corruption)
- Struct tags with backticks: use PowerShell `[System.IO.File]::WriteAllText` for targeted fixes
- SQLite backtick strings: use double-quotes for SQL inside Go string literals
- Build before running: exe may be locked by running process, kill first

## Communication Style
- Be direct and concise — no filler phrases like "Great question!" or "I'd be happy to help"
- Chinese preferred (user is Chinese)
- When reporting progress, be brief: one line per step
- When done, summarize what changed in bullet points — no padding
## MCP Server (Web Search)

Location: `mcp/search_server_v3.py`
Config: `mcp/claude_code_config.json`

### Setup (one-time)
```bash
cd mcp
pip install -r requirements.txt
```

### Enable in Claude Code
Add to your Claude Code MCP config (`~/.claude/settings.json` or via `/mcp` command):
```json
{
  "mcpServers": {
    "search": {
      "command": "py",
      "args": ["D:\\myselfClow\\mcp\\search_server_v3.py"]
    }
  }
}
```

Or use the bundled config directly:
```bash
claude --mcp-config D:\myselfClow\mcp\claude_code_config.json
```

### Available Tool: `web_search`
- `query` (required): search keywords
- `search_type`: `"hot"` (trending) / `"news"` (news search) / `"web"` (general), default `"web"`
- `count`: number of results 1-20, default 10

### Usage Examples
```
web_search(query="热点新闻", search_type="hot")       # trending news
web_search(query="DeerFlow github", search_type="web") # general search
web_search(query="AI 进展", search_type="news")        # news search
```