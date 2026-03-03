# Token 使用量追踪系统设计

## 1. 数据库设计

### 新增表结构

```sql
-- Token 使用记录表
CREATE TABLE IF NOT EXISTS token_usage (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    user_id TEXT NOT NULL,
    session_id TEXT,
    model TEXT NOT NULL,              -- claude-sonnet-4.5, claude-opus-4-6 等
    input_tokens INTEGER NOT NULL,
    output_tokens INTEGER NOT NULL,
    total_tokens INTEGER NOT NULL,
    request_type TEXT,                -- chat, tool_call, search 等
    cost_usd REAL,                    -- 费用（美元）
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX IF NOT EXISTS idx_token_usage_user_id ON token_usage(user_id);
CREATE INDEX IF NOT EXISTS idx_token_usage_created_at ON token_usage(created_at);

-- Token 使用统计表（预聚合，提升查询性能）
CREATE TABLE IF NOT EXISTS token_stats (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    user_id TEXT NOT NULL,
    date TEXT NOT NULL,               -- YYYY-MM-DD 格式
    model TEXT NOT NULL,
    total_input_tokens INTEGER DEFAULT 0,
    total_output_tokens INTEGER DEFAULT 0,
    total_tokens INTEGER DEFAULT 0,
    total_requests INTEGER DEFAULT 0,
    total_cost_usd REAL DEFAULT 0,
    updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    UNIQUE(user_id, date, model)
);
CREATE INDEX IF NOT EXISTS idx_token_stats_user_date ON token_stats(user_id, date);

-- 费用配置表（不同模型的定价）
CREATE TABLE IF NOT EXISTS pricing_config (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    model TEXT UNIQUE NOT NULL,
    input_price_per_1m REAL NOT NULL,  -- 每百万 input tokens 价格（美元）
    output_price_per_1m REAL NOT NULL, -- 每百万 output tokens 价格（美元）
    updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

-- 初始化默认定价（2026年2月 Anthropic 官方定价）
INSERT OR REPLACE INTO pricing_config (model, input_price_per_1m, output_price_per_1m) VALUES
    ('claude-opus-4-6', 15.00, 75.00),
    ('claude-sonnet-4.5', 3.00, 15.00),
    ('claude-sonnet-4', 3.00, 15.00),
    ('claude-haiku-4', 0.80, 4.00);
```

## 2. Go 代码实现

### Store 接口扩展

```go
type Store interface {
    // ... 现有方法 ...

    // Token 追踪
    SaveTokenUsage(userID, sessionID, model string, inputTokens, outputTokens int, requestType string) error
    GetTokenUsage(userID string, startDate, endDate time.Time) ([]TokenUsageEntry, error)
    GetTokenStats(userID string, date string) ([]TokenStatsEntry, error)
    GetTotalCost(userID string, startDate, endDate time.Time) (float64, error)

    // 费用配置
    GetPricing(model string) (inputPrice, outputPrice float64, err error)
    UpdatePricing(model string, inputPrice, outputPrice float64) error
}

type TokenUsageEntry struct {
    ID           int
    UserID       string
    SessionID    string
    Model        string
    InputTokens  int
    OutputTokens int
    TotalTokens  int
    RequestType  string
    CostUSD      float64
    CreatedAt    time.Time
}

type TokenStatsEntry struct {
    Date              string
    Model             string
    TotalInputTokens  int
    TotalOutputTokens int
    TotalTokens       int
    TotalRequests     int
    TotalCostUSD      float64
}
```

## 3. 集成点

### 在 Claude API 调用后记录

```go
// internal/claude/runner.go
func (r *Runner) recordTokenUsage(userID, sessionID string, resp *anthropic.Response) error {
    if resp.Usage == nil {
        return nil
    }

    // 获取定价
    inputPrice, outputPrice, err := r.store.GetPricing(resp.Model)
    if err != nil {
        return err
    }

    // 计算费用
    cost := (float64(resp.Usage.InputTokens) * inputPrice / 1_000_000) +
            (float64(resp.Usage.OutputTokens) * outputPrice / 1_000_000)

    // 保存记录
    return r.store.SaveTokenUsage(
        userID,
        sessionID,
        resp.Model,
        resp.Usage.InputTokens,
        resp.Usage.OutputTokens,
        "chat",
    )
}
```

## 4. 新增命令

### /usage - 查看使用量

```go
// 今日使用量
/usage

// 指定日期范围
/usage 2026-02-01 2026-02-28

// 本周使用量
/usage week

// 本月使用量
/usage month
```

### /cost - 查看费用

```go
// 今日费用
/cost

// 本月费用
/cost month

// 总费用
/cost total
```

### /pricing - 查看/更新定价

```go
// 查看当前定价
/pricing

// 更新定价（管理员）
/pricing update claude-sonnet-4.5 3.0 15.0
```

## 5. 输出示例

```
📊 Token 使用统计 (2026-02-25)

模型: claude-sonnet-4.5
━━━━━━━━━━━━━━━━━━━━━━━
输入 Token:   12,450 个
输出 Token:   8,320 个
总计 Token:   20,770 个
请求次数:     15 次
今日费用:     $0.16

模型: claude-opus-4-6
━━━━━━━━━━━━━━━━━━━━━━━
输入 Token:   5,230 个
输出 Token:   3,140 个
总计 Token:   8,370 个
请求次数:     3 次
今日费用:     $0.31

━━━━━━━━━━━━━━━━━━━━━━━
💰 今日总费用: $0.47
📈 本月累计: $12.34
```

## 6. 优化建议

### 自动控制和警告

```go
// 每日限额检查
func (r *Runner) checkDailyLimit(userID string) error {
    today := time.Now().Format("2006-01-02")
    cost, err := r.store.GetTotalCost(userID, today, today)
    if err != nil {
        return err
    }

    // 超过每日限额
    if cost > dailyLimit {
        return fmt.Errorf("已达每日限额 $%.2f", dailyLimit)
    }

    // 警告阈值（80%）
    if cost > dailyLimit*0.8 {
        log.Printf("警告: 用户 %s 今日费用已达 $%.2f (限额: $%.2f)", userID, cost, dailyLimit)
    }

    return nil
}
```

### 定期汇总任务

```go
// 每天凌晨汇总昨日数据到 token_stats 表
func (s *sqliteStore) AggregateYesterdayStats() error {
    yesterday := time.Now().AddDate(0, 0, -1).Format("2006-01-02")

    _, err := s.db.Exec(`
        INSERT INTO token_stats (user_id, date, model, total_input_tokens, total_output_tokens, total_tokens, total_requests, total_cost_usd)
        SELECT
            user_id,
            DATE(created_at) as date,
            model,
            SUM(input_tokens),
            SUM(output_tokens),
            SUM(total_tokens),
            COUNT(*),
            SUM(cost_usd)
        FROM token_usage
        WHERE DATE(created_at) = ?
        GROUP BY user_id, date, model
        ON CONFLICT(user_id, date, model) DO UPDATE SET
            total_input_tokens = excluded.total_input_tokens,
            total_output_tokens = excluded.total_output_tokens,
            total_tokens = excluded.total_tokens,
            total_requests = excluded.total_requests,
            total_cost_usd = excluded.total_cost_usd,
            updated_at = CURRENT_TIMESTAMP
    `, yesterday)

    return err
}
```

## 7. 数据可视化（可选）

### 导出 CSV

```go
// /export usage
// 导出到 data/usage_export_20260225.csv
```

### Web 面板（可选）

- 使用 Go HTML template 生成简单的 Web 页面
- 图表展示（Chart.js）
- 实时监控面板

## 8. 实施步骤

1. ✅ 更新数据库 schema
2. ✅ 扩展 Store 接口和实现
3. ✅ 在 Claude Runner 中集成记录
4. ✅ 实现新命令（/usage, /cost, /pricing）
5. ✅ 添加限额检查和警告
6. ✅ 实现定期汇总任务
7. ✅ 测试和验证
8. 🔄 （可选）Web 可视化面板

## 9. 测试计划

```bash
# 单元测试
go test ./internal/memory/... -v

# 集成测试
# 1. 发送多条消息，检查 token_usage 表
# 2. 检查费用计算是否正确
# 3. 测试限额警告
# 4. 测试统计命令输出

# 性能测试
# 1. 插入 10000 条记录
# 2. 测试查询性能
# 3. 测试聚合查询性能
```
