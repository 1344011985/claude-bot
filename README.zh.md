# claude-bot

基于 Go 语言开发的机器人，使用 [Claude Code](https://claude.ai/code) 作为 AI 后端，当前支持飞书（Lark）平台，并内置本地 HTTP Bridge 与 Skills Hub 提示词注入能力。

> 注意：`.kiro/specs/` 下可能仍保留早期 QQ bot 设计稿，但那已经不是当前实现。

## 快速部署（Linux）

无需编译，直接下载预构建二进制：

```bash
# 1. 下载二进制
wget https://github.com/1344011985/claude-bot/releases/download/v0.1.0/claude-bot-linux-amd64
chmod +x claude-bot-linux-amd64
sudo mv claude-bot-linux-amd64 /usr/local/bin/claude-bot

# 2. 创建配置目录和配置文件
mkdir -p ~/.claude-bot
cat > ~/.claude-bot/claude-bot.json << 'EOF'
{
  "channel": "feishu",
  "feishu": {
    "app_id": "cli_xxxxxxxxxxxxxxxx",
    "app_secret": "xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx"
  }
}
EOF

# 3. 编辑配置，填入你的飞书 App ID 和 App Secret
nano ~/.claude-bot/claude-bot.json

# 4. 启动
claude-bot -channel feishu
```

> 前提：服务器上已安装并登录 [Claude Code CLI](https://claude.ai/code)（`claude` 命令可用）

## 环境要求

- Go 1.21+（源码构建时需要）
- 已安装并登录的 [Claude Code CLI](https://claude.ai/code)

## 构建

```bash
git clone https://github.com/1344011985/claude-bot.git
cd claude-bot

# Linux / macOS
go build -o dist/feishu-bot ./cmd/bot/

# Windows
go build -o dist/feishu-bot.exe ./cmd/bot/
```

注入版本信息构建：

```bash
go build -ldflags "-X main.GitCommit=$(git rev-parse --short HEAD) -X main.BuildDate=$(date -u +%Y-%m-%dT%H:%M:%SZ)" -o dist/feishu-bot ./cmd/bot/
```

## 配置文件

程序启动时会自动从以下路径读取 `claude-bot.json`：

| 平台 | 路径 |
|------|------|
| Windows | `C:\Users\<用户名>\.claude-bot\claude-bot.json` |
| macOS | `/Users/<用户名>/.claude-bot/claude-bot.json` |
| Linux | `~/.claude-bot/claude-bot.json` |

首次运行前需要手动创建目录和配置文件。

### 最简配置（飞书）

```json
{
  "feishu": {
    "app_id": "cli_xxxxxxxxxxxxxxxx",
    "app_secret": "xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx"
  }
}
```

### 完整配置说明

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

| 字段 | 默认值 | 说明 |
|------|--------|------|
| `channel` | `feishu` | 启动的平台 |
| `claude.bin_path` | `claude` | Claude Code CLI 可执行文件路径 |
| `claude.timeout_seconds` | `0` | 单次请求超时秒数，0 表示不限制 |
| `claude.default_model` | `haiku` | 默认模型：haiku / sonnet / opus / vip-gpt5.4 |
| `claude.auto_select` | `true` | 根据消息复杂度自动选择模型 |
| `memory.db_path` | `~/.claude-bot/data/bot.db` | SQLite 数据库路径 |
| `images.cache_dir` | （禁用）| 图片缓存本地目录，留空不处理图片 |
| `allowlist` | （所有用户）| 白名单，填写飞书 open_id 限制可用用户 |
| `log_level` | `info` | 日志级别：debug / info / warn / error |
| `system_prompt` | （内置）| 覆盖默认系统提示词 |

## 运行

```bash
./dist/feishu-bot

# 指定平台
./dist/feishu-bot -channel feishu

# 指定配置文件路径
./dist/feishu-bot -config /path/to/claude-bot.json
```

## 目录结构

首次运行后，配置目录结构如下：

```
~/.claude-bot/
  claude-bot.json       配置文件
  data/
    bot.db              SQLite 数据库（会话、记忆、历史记录）
  logs/
    2026-03-04.json     按日期存储的日志文件（JSON Lines 格式）
```

可执行文件可放在任意位置，无需与配置目录同级。

## 可用命令

机器人运行后，用户可在飞书中发送以下命令：

| 命令 | 说明 |
|------|------|
| `/ask <问题>` | 向 Claude 提问（续接当前会话） |
| `/new` | 开启新对话，清除当前上下文 |
| `/remember <内容>` | 保存长期记忆，每次对话自动注入 |
| `/forget` | 清除所有长期记忆 |
| `/history [n]` | 查看最近 n 条对话，默认 5 条 |
| `/news [关键词]` | 搜索最新新闻，不带关键词则显示热点 |
| `/skill <名称> [参数]` | 触发已注册的技能（关键词匹配提示词注入） |
| `/tasks` | 查看最近的异步任务列表 |
| `/status <task_id>` | 查看异步任务状态 |
| `/cancel <task_id>` | 取消正在执行的异步任务 |
| `/browse <url> [问题]` | 抓取网页并让 Claude 分析其内容 |
| `/help` | 显示帮助信息 |
| `/version` | 显示版本和构建信息 |

不以 `/` 开头的消息默认等同于 `/ask`。

模型切换支持自然语言，例如发送"切换模型为 sonnet"或"使用 opus"即可切换。

## 飞书应用配置

1. 在 [open.feishu.cn](https://open.feishu.cn) 创建自定义应用
2. 开启以下权限：
   - `im:message` — 读取和发送消息
   - `im:message.reaction:write` — 添加/移除消息表情回应
   - `contact:user.base:readonly` — 获取发送者昵称
3. 将应用添加到企业并启用 WebSocket 长连接事件订阅
4. 将 `App ID` 和 `App Secret` 填入 `claude-bot.json`

## 测试

```bash
go test ./...
```
