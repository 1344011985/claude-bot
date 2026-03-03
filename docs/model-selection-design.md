# 模型选择和成本优化设计方案

## 目标
按任务复杂度自动选择合适的模型（haiku/sonnet/opus），降低 API 成本。

## 核心功能

### 1. 模型定价配置
```yaml
claude:
  models:
    haiku:
      name: "claude-3-5-haiku-20241022"
      input_price_mtok: 1.0      # $1 per million tokens
      output_price_mtok: 5.0     # $5 per million tokens
      cache_write_mtok: 1.25
      cache_read_mtok: 0.1
    sonnet:
      name: "claude-sonnet-4-5"
      input_price_mtok: 15.0     # $15 per million tokens
      output_price_mtok: 75.0    # $75 per million tokens
      cache_write_mtok: 18.75
      cache_read_mtok: 1.5
    opus:
      name: "claude-opus-4-6"
      input_price_mtok: 30.0     # $30 per million tokens
      output_price_mtok: 150.0   # $150 per million tokens
      cache_write_mtok: 37.5
      cache_read_mtok: 3.0

  # 自动选择策略
  auto_select: true              # 启用自动模型选择
  default_model: "haiku"         # 默认模型
  user_preferences: {}           # 用户偏好模型 {user_id: model}
```

### 2. 任务复杂度判断规则

#### 简单任务（使用 haiku - 成本最低）
- 对话轮次少（< 3 轮）
- 消息长度短（< 100 字）
- 无图片附件
- 关键词：打招呼、简单问答、/news、/help、/version、/history

示例：
- "你好"
- "/news"
- "什么是 Go 语言"

#### 中等任务（使用 sonnet - 平衡性能和成本）
- 对话轮次中等（3-10 轮）
- 消息长度中等（100-500 字）
- 包含图片但不复杂
- 关键词：代码问题、解释、分析、优化建议

示例：
- "这段代码有什么问题"
- "分析下这个项目结构"
- "帮我优化这个函数"

#### 复杂任务（使用 opus - 最强性能）
- 对话轮次多（> 10 轮）
- 消息长度长（> 500 字）
- 包含多张图片
- 明确要求高质量输出
- 关键词：架构设计、重构、代码生成、debug 复杂问题

示例：
- "设计一个完整的用户认证系统"
- "重构这个项目的数据库层"
- "帮我找出这个复杂 bug 的根源"
- 用户明确说"详细"、"完整"、"深入"

### 3. 用户控制选项

#### 手动切换模型
```
/model haiku     # 切换到 haiku（省钱模式）
/model sonnet    # 切换到 sonnet（平衡模式）
/model opus      # 切换到 opus（性能模式）
/model auto      # 恢复自动选择
/model status    # 查看当前模型设置
```

#### 查看使用统计
```
/usage           # 查看 token 使用统计
/usage today     # 今天的使用量
/usage week      # 本周的使用量
/usage month     # 本月的使用量

/cost            # 查看费用明细
/cost today      # 今天的费用
/cost by-model   # 按模型分组的费用
```

### 4. 数据库 Schema

#### 新增 model_usage 表
```sql
CREATE TABLE model_usage (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    user_id TEXT NOT NULL,
    session_id TEXT NOT NULL,
    model TEXT NOT NULL,              -- haiku/sonnet/opus
    prompt_tokens INTEGER NOT NULL,
    completion_tokens INTEGER NOT NULL,
    cache_creation_tokens INTEGER DEFAULT 0,
    cache_read_tokens INTEGER DEFAULT 0,
    total_cost_usd REAL NOT NULL,     -- 计算后的总费用
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    INDEX idx_user_time (user_id, created_at),
    INDEX idx_model_time (model, created_at)
);

CREATE TABLE daily_usage_summary (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    date TEXT NOT NULL,               -- YYYY-MM-DD
    model TEXT NOT NULL,
    total_requests INTEGER DEFAULT 0,
    total_tokens INTEGER DEFAULT 0,
    total_cost_usd REAL DEFAULT 0,
    UNIQUE(date, model)
);
```

#### 新增 user_model_preference 表
```sql
CREATE TABLE user_model_preference (
    user_id TEXT PRIMARY KEY,
    preferred_model TEXT NOT NULL,    -- haiku/sonnet/opus/auto
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);
```

### 5. 实现架构

#### 5.1 配置扩展（config/config.go）
```go
type ModelConfig struct {
    Name             string  `yaml:"name"`
    InputPriceMTok   float64 `yaml:"input_price_mtok"`
    OutputPriceMTok  float64 `yaml:"output_price_mtok"`
    CacheWriteMTok   float64 `yaml:"cache_write_mtok"`
    CacheReadMTok    float64 `yaml:"cache_read_mtok"`
}

type ClaudeConfig struct {
    Models          map[string]ModelConfig `yaml:"models"`
    AutoSelect      bool                   `yaml:"auto_select"`
    DefaultModel    string                 `yaml:"default_model"`
    // ... 现有字段
}
```

#### 5.2 模型选择器（claude/selector.go）
```go
type TaskComplexity int
const (
    ComplexitySimple TaskComplexity = iota
    ComplexityMedium
    ComplexityComplex
)

type ModelSelector struct {
    config      *config.ClaudeConfig
    store       memory.Store
}

func (s *ModelSelector) SelectModel(
    userID string,
    content string,
    imageCount int,
    conversationTurns int,
) string {
    // 1. 检查用户偏好
    if pref := s.getUserPreference(userID); pref != "auto" {
        return pref
    }

    // 2. 自动判断复杂度
    complexity := s.analyzeComplexity(content, imageCount, conversationTurns)

    // 3. 返回对应模型
    switch complexity {
    case ComplexitySimple:
        return "haiku"
    case ComplexityMedium:
        return "sonnet"
    case ComplexityComplex:
        return "opus"
    }
}
```

#### 5.3 Runner 扩展（claude/runner.go）
```go
func (r *Runner) RunWithModel(
    ctx context.Context,
    prompt, sessionID, systemPrompt string,
    imagePaths []string,
    modelName string,  // 新增：指定模型
    progressFn func(),
) (*RunResult, error) {
    // 添加 --model 参数到 args
    args := []string{"-p", prompt, "--model", modelName, "--output-format", "json"}
    // ...
}
```

#### 5.4 使用追踪（memory/usage.go）
```go
type UsageTracker interface {
    RecordUsage(ctx context.Context, record *UsageRecord) error
    GetDailyUsage(ctx context.Context, userID string, date time.Time) (*DailyUsage, error)
    GetCostSummary(ctx context.Context, userID string, period string) (*CostSummary, error)
}

type UsageRecord struct {
    UserID              string
    SessionID           string
    Model               string
    PromptTokens        int
    CompletionTokens    int
    CacheCreationTokens int
    CacheReadTokens     int
    TotalCostUSD        float64
}
```

### 6. 成本对比示例

假设一次典型对话：
- Input: 2000 tokens
- Output: 500 tokens
- Cache read: 10000 tokens

| 模型 | Input | Output | Cache | 总费用 | 相对成本 |
|------|-------|--------|-------|--------|----------|
| Haiku | $0.002 | $0.0025 | $0.001 | $0.0055 | 1x |
| Sonnet | $0.03 | $0.0375 | $0.015 | $0.0825 | 15x |
| Opus | $0.06 | $0.075 | $0.03 | $0.165 | 30x |

**通过智能选择模型，可以节省 50-80% 的成本！**

### 7. 实施步骤

1. ✅ 完成设计文档
2. 扩展配置结构支持多模型
3. 实现模型选择器
4. 修改 Runner 支持模型参数
5. 更新数据库 schema
6. 实现 usage 追踪
7. 添加 /model 命令
8. 添加 /usage 和 /cost 命令
9. 测试和验证

### 8. 配置示例

更新 config.yaml：
```yaml
claude:
  bin_path: "C:\\Users\\13440\\.local\\bin\\claude.exe"
  timeout_seconds: 240
  max_timeout_seconds: 7200

  # 模型配置
  models:
    haiku:
      name: "claude-3-5-haiku-20241022"
      input_price_mtok: 1.0
      output_price_mtok: 5.0
      cache_write_mtok: 1.25
      cache_read_mtok: 0.1
    sonnet:
      name: "claude-sonnet-4-5"
      input_price_mtok: 15.0
      output_price_mtok: 75.0
      cache_write_mtok: 18.75
      cache_read_mtok: 1.5
    opus:
      name: "claude-opus-4-6"
      input_price_mtok: 30.0
      output_price_mtok: 150.0
      cache_write_mtok: 37.5
      cache_read_mtok: 3.0

  auto_select: true
  default_model: "haiku"
```

## 预期效果

### 成本节省
- 简单对话（占比 60%）：使用 haiku，节省 93%
- 中等对话（占比 30%）：使用 sonnet，节省 0%（基准）
- 复杂对话（占比 10%）：使用 opus，额外开销 100%

**总体预期节省：约 55% 的成本**

### 用户体验
- 简单任务响应更快（haiku 速度快）
- 复杂任务质量更高（opus 能力强）
- 用户可随时切换模型
- 透明的使用统计和费用追踪
