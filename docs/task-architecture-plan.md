# 单入口多任务并发架构改造方案

> 基于仓库现状（2026-03-22）整理，目标不是空谈重构，而是给出一版能直接按阶段落地的实施方案。

---

## 1. 先说结论

当前仓库已经有几块能直接复用的底座：

- `internal/claude/runner.go`：Claude CLI 调用与流式输出解析，基本稳定
- `internal/skills/*`：Skills Hub / PromptMiddleware 思路已落地
- `internal/memory/store.go`：SQLite 存储层稳定，可继续扩展表
- `internal/httpbridge/server.go`：可作为第一阶段切异步任务化的最小切入点

真正需要改的，不是 runner 本体，而是：

1. **把“消息”抽象成“任务”**
2. **把同步执行链路改成可回流的异步执行链路**
3. **明确 session 推进策略，避免同一 user 并发任务互相污染上下文**

---

## 2. 当前真实代码链路

### 2.1 Feishu 主链路

```text
Feishu WebSocket event
  -> internal/feishu/feishu.go:onMessage()
  -> internal/command/router.go:Route()
  -> internal/command/handler_ask.go:Handle()
  -> internal/claude/runner.go:RunWithModel()
  -> memory/store.go: SaveSession / SaveHistory / RecordUsage
```

特点：

- 已支持 Skills Hub 注入
- 已支持流式 progressFn
- 仍然是 **单次消息直接执行**，没有 task 抽象

### 2.2 HTTP Bridge 链路

```text
POST /chat
  -> internal/httpbridge/server.go:handleChat()
  -> router.Route()
  -> 同步等待完整 reply
  -> 返回 {ok, reply}
```

特点：

- 接口简单，但本质是同步 RPC
- 长任务会卡住 HTTP 请求生命周期
- 不适合后续多 agent / 并发任务场景

---

## 3. 当前问题，不只是“慢”

### 3.1 没有任务身份

现在系统里只有“某个用户发来一条消息”，没有 task id。
这会直接导致：

- 无法查询“刚才那条现在做到哪了”
- 无法取消“第二个任务”
- 无法对结果做可靠归属
- 无法自然支持多任务并发

### 3.2 `sessions` 表语义过载

现在 `sessions` 表表示：

- 某个用户当前对应的 Claude session_id

这在单串行问答时成立；但在并发任务下会出问题：

- A 任务读取旧 session S1
- B 任务也读取旧 session S1
- A 先完成，更新到 S2
- B 后完成，更新到 S3

此时数据库层面虽然没报错，但语义已经混乱：

- B 是否应该覆盖 A？
- A / B 是否本来就不该共享同一会话继续？

所以真正要解决的不是“SQLite 能不能并发写”，而是：

> **并发任务下，谁有资格推进用户主会话。**

### 3.3 文档严重落后于现状

已确认：

- `.kiro/specs/qq-claude-bot/design.md` 属于 QQ bot 旧阶段文档
- 文中提到的 `internal/bot/` 目录在现仓库已不存在
- README 缺少 HTTP Bridge 与 Skills Hub 说明

这会直接误导后续开发与 review。

---

## 4. 目标边界

这次不追求“一步到位做完所有多 agent 能力”，而是分两层目标。

### 第一阶段目标

先把下面这 3 件事做出来：

1. **引入 taskqueue，建立任务身份**
2. **让 HTTP Bridge 先变成异步任务接口**
3. **保留 Feishu 主链路现状，先不大改入口**

这样可以最小代价得到：

- 可提交任务
- 可查任务状态
- 可取消任务
- 可观测执行结果
- 为后续 Feishu 接入 taskqueue 铺路

### 第二阶段目标

再把 Feishu 主入口切到 taskqueue，并加：

- `/tasks`
- `/status`
- `/cancel`
- 完成回流通知
- 更细的任务寻址能力

---

## 5. 第一阶段推荐设计

## 5.1 新模块：`internal/taskqueue/`

建议拆 3 个文件：

- `types.go`：任务结构与状态枚举
- `store.go`：任务表 SQL 与 CRUD
- `queue.go`：提交 / worker / 取消 / 回调

### 核心结构

```go
type Status string

const (
    StatusPending   Status = "pending"
    StatusRunning   Status = "running"
    StatusDone      Status = "done"
    StatusFailed    Status = "failed"
    StatusCancelled Status = "cancelled"
)

type Task struct {
    ID              string
    UserID          string
    GroupID         string
    Content         string
    Status          Status
    Result          string
    Error           string
    SessionID       string
    ContinueSession bool
    CreatedAt       time.Time
    StartedAt       *time.Time
    DoneAt          *time.Time
}
```

### 第一阶段策略：先引入 `ContinueSession`

这是这次最关键的一个语义位。

建议：

- `ContinueSession=true`：允许读取/推进用户主 session
- `ContinueSession=false`：作为独立任务运行，不写回用户主 session

这样第一阶段就先把风险收住：

- HTTP Bridge 默认可以先设为 `false`
- Feishu 主问答暂时维持旧逻辑，不接 taskqueue
- 后面再逐步决定哪些任务可推进主对话

这比一上来让所有任务共用 `sessions` 表稳得多。

---

## 5.2 数据表建议

```sql
CREATE TABLE IF NOT EXISTS tasks (
    id               TEXT PRIMARY KEY,
    user_id          TEXT NOT NULL,
    group_id         TEXT NOT NULL DEFAULT '',
    content          TEXT NOT NULL,
    status           TEXT NOT NULL DEFAULT 'pending',
    result           TEXT NOT NULL DEFAULT '',
    error            TEXT NOT NULL DEFAULT '',
    session_id       TEXT NOT NULL DEFAULT '',
    continue_session INTEGER NOT NULL DEFAULT 0,
    created_at       DATETIME DEFAULT CURRENT_TIMESTAMP,
    started_at       DATETIME,
    done_at          DATETIME
);

CREATE INDEX IF NOT EXISTS idx_tasks_user_created ON tasks(user_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_tasks_user_status ON tasks(user_id, status);
```

说明：

- 第一阶段不用急着上 `task_events`
- 先用单表把状态跑通
- 真要补时间线/日志，再在第二阶段加 `task_events`

---

## 5.3 Queue 接口

```go
type Queue interface {
    Submit(ctx context.Context, req SubmitRequest) (*Task, error)
    Get(taskID string) (*Task, error)
    ListByUser(userID string, limit int) ([]*Task, error)
    Cancel(taskID string) error
}
```

其中：

```go
type SubmitRequest struct {
    UserID          string
    GroupID         string
    Content         string
    ContinueSession bool
    ProgressFn      func(string)
}
```

---

## 6. HTTP Bridge 的第一阶段落地

把 `/chat` 从同步返回 reply，改成：

### `POST /chat`

请求：

```json
{
  "chat_id": "oc_xxx",
  "user_id": "ou_xxx",
  "content": "帮我整理一下这段日志"
}
```

返回：

```json
{
  "ok": true,
  "task_id": "task_xxx",
  "status": "pending"
}
```

### `GET /task?id=task_xxx`

返回：

```json
{
  "ok": true,
  "task": {
    "id": "task_xxx",
    "status": "running",
    "result": "",
    "error": ""
  }
}
```

### `POST /cancel`

请求：

```json
{
  "task_id": "task_xxx"
}
```

返回：

```json
{
  "ok": true
}
```

这一步落完，就已经能支撑外部系统按异步任务模式接 myselfClow。

---

## 7. Feishu 为什么先不一起大改

因为 Feishu 主链路上不只是 ask：

- 群聊 @ 判定
- 引用消息
- 流式卡片
- reaction
- 历史上下文补强

如果第一阶段一口气把 Feishu 也一起切到 taskqueue，会把改动面放大太多。

所以更稳的节奏是：

1. 先在 HTTP Bridge 上验证 taskqueue
2. 跑通任务生命周期
3. 再把 Feishu `/ask` 接进 queue

这符合“最小切口，逐步迁移”的原则。

---

## 8. 文档要同步修什么

### 8.1 `.kiro/specs/qq-claude-bot/design.md`

处理建议：

- 不直接删除
- 在文档顶部显式标记：**历史文档，已不代表当前实现**
- 指向 `docs/task-architecture-plan.md` 与 README

### 8.2 README / README.zh.md

至少补：

- HTTP Bridge：`/health`、`/chat`、`/task`、`/cancel`
- Skills Hub：用途、触发方式、持久化位置
- 当前 channel 只有 Feishu
- 当前并发任务能力的阶段性状态

### 8.3 `CLAUDE.md`

补当前开发上下文：

- 旧 QQ 文档是历史资产
- 新链路以 Feishu + HTTP Bridge 为准
- 第一阶段 taskqueue 是当前重点

---

## 9. 实施优先级

### P0

- [ ] 新增 `internal/taskqueue/`
- [ ] 新增 `tasks` 表
- [ ] `/chat` 异步化
- [ ] 新增 `/task`、`/cancel`
- [ ] README / README.zh.md 补现状
- [ ] 历史 QQ 文档加醒目标记

### P1

- [ ] Router / command 增加 `/tasks` `/status` `/cancel`
- [ ] Feishu 接入 taskqueue
- [ ] 任务完成主动回流飞书

### P2

- [ ] `task_events` 表
- [ ] 更细粒度任务寻址
- [ ] agent adapter 抽象
- [ ] 多 agent 调度

---

## 10. 这轮开发的推荐切法

就按下面顺序落：

1. **先修文档**，让现状收敛
2. **再加 taskqueue 骨架**
3. **再改 HTTP Bridge**
4. **编译 + 测试**
5. **最后再决定 Feishu 要不要继续接入**

这样不会把整个仓库一下子搅乱，也最符合当前需求：

> 先自己整理，然后直接开发，不等确认。
