# WebSocket 连接优化方案

## 优化内容

### 1. 连接健康监控（Connection Monitor）
新增了 `connectionMonitor` 模块，功能包括：

#### 核心功能
- **活动追踪** - 记录每次收到消息的时间
- **定期健康检查** - 每 30 秒检查一次连接状态
- **超时告警** - 超过 90 秒无活动时记录警告
- **错误追踪** - 记录连接错误次数和时间
- **自动恢复检测** - 收到消息后自动重置错误计数

#### 日志输出
```
[INFO]  connection monitor started, check_interval=30s
[DEBUG] connection healthy, last_activity_ago=15s, error_count=0
[WARN]  connection inactive, inactive_duration=120s, last_activity=2024-...
[WARN]  frequent connection errors, error_count=5, last_error_ago=2m
```

### 2. 消息活动追踪
在两个消息处理器中添加了活动记录：
- `OnGroupATMessage` - 群聊 @ 消息
- `OnC2CMessage` - 私聊消息

每次收到消息时自动调用 `monitor.recordActivity()`，更新连接活动时间。

### 3. 错误追踪
在 WebSocket 错误回调中添加错误记录：
```go
event.ErrorNotifyHandler(func(err error) {
    logger.Error("websocket error", "err", err)
    monitor.recordError() // 记录错误
})
```

## 工作原理

```
用户消息到达
    ↓
OnGroupATMessage / OnC2CMessage
    ↓
monitor.recordActivity() ← 更新活动时间
    ↓
    └→ 重置错误计数（如果之前有错误）


后台定时检查（每30秒）
    ↓
monitor.checkHealth()
    ↓
    ├─ 检查活动时间差
    │  └→ > 90秒 → 记录警告
    │
    └─ 检查错误次数
       └→ ≥ 3次 → 记录警告
```

## 优化效果

### 可见性提升
- ✅ 实时监控连接健康状态
- ✅ 清晰的日志输出，便于排查问题
- ✅ 超时和错误的早期预警

### 问题诊断
通过日志可以快速判断：
- 是否因长时间无消息导致连接失活
- 是否频繁出现网络错误
- 连接恢复是否正常

### 不影响现有逻辑
- ✅ 不改变原有的 SessionManager 重连机制
- ✅ 纯监控，不干预 SDK 的连接管理
- ✅ 即使 monitor 为 nil 也不影响正常运行

## 后续可优化方向

### 如果仍有超时问题
1. **主动心跳**
   - SDK 的 SessionManager 已经处理心跳
   - 如果还不够，可以考虑定期调用 API（如 `api.Me()`）保持连接

2. **更激进的重连策略**
   - 检测到超时时主动触发重连（需要修改 SDK 或使用反射）

3. **切换到 Webhook**
   - 如果 WebSocket 仍然不稳定，按之前的方案切换到 Webhook

## 使用方法

### 编译运行
```bash
go build -o dist/qq-claude-bot.exe cmd/bot/main.go
dist/qq-claude-bot.exe
```

### 查看监控日志
正常日志：
```
[INFO]  connection monitor started
[DEBUG] connection healthy, last_activity_ago=25s
```

异常日志（需注意）：
```
[WARN]  connection inactive, inactive_duration=120s
[WARN]  frequent connection errors, error_count=5
```

### 验证优化效果
1. 启动机器人
2. 长时间不发消息（超过 90 秒）
3. 查看是否有 "connection inactive" 警告
4. 发送一条消息
5. 查看是否显示 "connection recovered"

## 代码变更

### 新增文件
无（所有代码集成到现有文件中）

### 修改文件
1. `internal/bot/bot.go`
   - 新增 `connectionMonitor` 结构体
   - 新增 `start()`, `checkHealth()`, `recordActivity()`, `recordError()` 方法
   - `Start()` 函数中启动监控器

2. `internal/bot/handler.go`
   - 新增 `activityRecorder` 接口
   - 新增 `monitor` 字段和 `setMonitor()` 方法
   - `OnGroupATMessage()` 和 `OnC2CMessage()` 中添加活动记录

### 行数统计
- 新增代码：约 80 行
- 修改代码：约 10 行
- 总计：约 90 行

## 配置说明

### 监控参数（硬编码）
```go
检查间隔：30 秒
超时阈值：90 秒
错误阈值：3 次（5 分钟内）
```

### 如需调整
在 `internal/bot/bot.go` 中修改：
```go
// 修改检查间隔
ticker := time.NewTicker(30 * time.Second) // 改为你想要的值

// 修改超时阈值
if inactiveDuration > 90*time.Second { // 改为你想要的值

// 修改错误阈值
if errorCount >= 3 && time.Since(lastError) < 5*time.Minute { // 调整次数和时间
```

## 总结

这个优化方案：
- ✅ **安装成本低** - 约 90 行代码，30 分钟完成
- ✅ **风险小** - 纯监控，不改变原有逻辑
- ✅ **可见性好** - 清晰的日志输出
- ✅ **效果明显** - 可以快速发现和诊断超时问题

如果这个优化后仍有频繁超时，可以考虑：
1. 进一步优化重连策略
2. 切换到 Webhook 方案
3. 研究其他对话平台（如飞书、钉钉、企业微信等）
