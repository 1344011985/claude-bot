# claude-bot

Go-based bot powered by [Claude Code](https://claude.ai/code) as the AI backend. Currently supports Feishu (Lark).

## Requirements

- Go 1.21+
- [Claude Code CLI](https://claude.ai/code) installed and authenticated

## Build

```bash
git clone https://github.com/1344011985/claude-bot.git
cd claude-bot

# Linux / macOS
go build -o dist/feishu-bot ./cmd/bot/

# Windows
go build -o dist/feishu-bot.exe ./cmd/bot/
```

Build with version info:

```bash
go build -ldflags "-X main.GitCommit=$(git rev-parse --short HEAD) -X main.BuildDate=$(date -u +%Y-%m-%dT%H:%M:%SZ)" -o dist/feishu-bot ./cmd/bot/
```

## Configuration

The bot reads `claude-bot.json` from a platform-specific directory:

| Platform | Path |
|----------|------|
| Windows  | `C:\Users\<user>\.claude-bot\claude-bot.json` |
| macOS    | `/Users/<user>/.claude-bot/claude-bot.json` |
| Linux    | `/root/.claude-bot/claude-bot.json` |

Create the directory and config file manually before running.

### Minimal config (Feishu)

```json
{
  "feishu": {
    "app_id": "cli_xxxxxxxxxxxxxxxx",
    "app_secret": "xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx"
  }
}
```

### Full config reference

```json
{
  "channel": "feishu",
  "feishu": {
    "app_id": "cli_xxxxxxxxxxxxxxxx",
    "app_secret": "xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx"
  },
  "claude": {
    "bin_path": "claude",
    "timeout_seconds": 0,
    "max_timeout_seconds": 0,
    "default_model": "haiku",
    "auto_select": true
  },
  "memory": {
    "db_path": ""
  },
  "images": {
    "cache_dir": "",
    "max_size_mb": 10
  },
  "allowlist": [],
  "log_level": "info",
  "system_prompt": ""
}
```

| Field | Default | Description |
|-------|---------|-------------|
| `channel` | `feishu` | Which platform to start |
| `claude.bin_path` | `claude` | Path to Claude Code CLI binary |
| `claude.timeout_seconds` | `0` | Per-request timeout, 0 = unlimited |
| `claude.default_model` | `haiku` | Default model: haiku / sonnet / opus |
| `claude.auto_select` | `true` | Auto-select model based on message complexity |
| `memory.db_path` | `~/.claude-bot/data/bot.db` | SQLite database path |
| `images.cache_dir` | (disabled) | Local directory for downloaded images |
| `allowlist` | (all users) | Restrict to specific Feishu open_ids |
| `log_level` | `info` | Log level: debug / info / warn / error |
| `system_prompt` | (built-in) | Override the default system prompt |

## Run

```bash
./dist/feishu-bot

# Override channel
./dist/feishu-bot -channel feishu

# Override config path
./dist/feishu-bot -config /path/to/claude-bot.json
```

## Directory structure

After first run, the config directory will contain:

```
~/.claude-bot/
  claude-bot.json       config file
  data/
    bot.db              SQLite database (sessions, memories, history)
  logs/
    2026-03-04.json     daily log file (JSON lines)
```

The binary can be placed anywhere; it does not need to be in the config directory.

## Commands

Once the bot is running, users can send the following commands in Feishu:

| Command | Description |
|---------|-------------|
| `/ask <question>` | Ask Claude (continues existing session) |
| `/new` | Start a new session, clear current context |
| `/remember <content>` | Save a long-term memory injected into every session |
| `/forget` | Clear all long-term memories |
| `/history [n]` | Show last n conversations (default 5) |
| `/news [keyword]` | Search latest news, or show hot topics if no keyword |
| `/help` | Show available commands |
| `/version` | Show build version and commit |

Direct messages (without a `/` prefix) are treated as `/ask`.

Model switching is also supported via natural language, e.g. "切换模型为 sonnet" or "使用 opus".

## Feishu app setup

1. Create a custom app at [open.feishu.cn](https://open.feishu.cn)
2. Enable the following permissions:
   - `im:message` — read and send messages
   - `im:message.reaction:write` — add/remove reactions
   - `contact:user.base:readonly` — resolve sender display names
3. Add the bot to your workspace and enable WebSocket event subscription
4. Copy `App ID` and `App Secret` into `claude-bot.json`

## Tests

```bash
go test ./...
```
