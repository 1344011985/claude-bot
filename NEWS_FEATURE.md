# 新闻搜索功能说明

## 功能概述

qq-claude-bot 现已集成新闻搜索功能，可以通过 `/news` 命令获取最新的新闻和热点信息。

## 使用方法

### 1. 查看热点新闻

直接使用 `/news` 命令（不带参数）可以获取今日头条热榜的前 10 条热点新闻：

```
/news
```

**示例输出：**
```
找到 10 条新闻：

1. 媒体：彻底击碎日本"军国梦"
   热度: 13788725
   链接: https://www.toutiao.com/trending/7609522532311515178/

2. 刘强东宣布投资50亿进军游艇行业
   热度: 12476554
   链接: https://www.toutiao.com/trending/7610380823962959898/

...
```

### 2. 搜索特定主题的新闻

使用 `/news 关键词` 可以搜索包含特定关键词的新闻：

```
/news 科技
/news 体育
/news 财经
```

## 技术实现

### 新闻源

1. **今日头条热榜（主要）**
   - API: `https://www.toutiao.com/hot-event/hot-board/`
   - 特点：实时更新，数据准确，返回 JSON 格式
   - 状态：✅ 已测试可用

2. **Bing 新闻搜索（备用）**
   - API: Bing News RSS Feed
   - 特点：支持关键词搜索
   - 状态：⚠️ 可能被限流

3. **百度新闻搜索（备用）**
   - API: 百度新闻搜索
   - 特点：中文新闻丰富
   - 状态：⚠️ 需要 HTML 解析

### 代码结构

```
internal/newsearch/
├── newsearch.go        # 新闻搜索核心代码
└── newsearch_test.go   # 单元测试

internal/command/
└── router.go           # /news 命令处理器集成
```

### 核心组件

1. **Searcher** - 新闻搜索器
   - 聚合多个搜索引擎
   - 自动降级到可用的备用源

2. **SearchEngine** 接口
   - BingNewsSearcher
   - BaiduNewsSearcher
   - ToutiaoAPISearcher

3. **NewsItem** 结构
   ```go
   type NewsItem struct {
       Title       string  // 新闻标题
       Description string  // 新闻描述
       URL         string  // 新闻链接
       Source      string  // 新闻来源
       PublishedAt string  // 发布时间
   }
   ```

## 配置说明

系统提示词已更新，在 `config.yaml` 中注入了 `/news` 命令的使用说明：

```yaml
system_prompt: |
  ## 可用命令
  ### /news 命令 - 获取最新新闻
  - `/news` - 获取当前热点新闻
  - `/news 关键词` - 搜索特定新闻
```

当用户询问新闻相关问题时，Claude 会自动提示用户使用 `/news` 命令。

## 测试

### 运行测试

```bash
# 使用 Make
make test

# 直接使用 Go
go test ./internal/newsearch/... -v

# 使用 Python 测试脚本
python test_news.py
python get_news_now.py
```

### 测试结果

```
Testing Bing News search...
  Status: 200
  ✗ Response doesn't look like RSS feed

Testing Toutiao hot news API...
  Status: 200
  ✓ Toutiao API accessible
  Found 50 hot topics

Testing Baidu News search...
  Status: 200
  ? Response received but unclear if it contains news

Total: 1/3 tests passed
✓ At least one news source is working!
```

## 重新构建

添加新功能后需要重新编译项目：

```bash
# Windows
make build

# 或者手动编译
go build -ldflags "-X qq-claude-bot/internal/command.GitCommit=$(git rev-parse --short HEAD) -X qq-claude-bot/internal/command.BuildDate=$(date -u +%Y-%m-%dT%H:%M:%SZ)" -o dist/qq-claude-bot.exe ./cmd/bot/
```

编译成功后，新的 `dist/qq-claude-bot.exe` 将包含 `/news` 命令功能。

## 使用示例

### 场景 1：用户询问今天的新闻

**用户：** 帮我看看今天的新闻

**机器人：** 好的！你可以使用 `/news` 命令查看今日头条热榜，或者使用 `/news 关键词` 搜索特定主题的新闻。比如 `/news 科技` 可以搜索科技新闻。

**用户：** /news

**机器人：** [显示今日头条热榜前 10 条新闻]

### 场景 2：搜索特定主题

**用户：** 有什么科技新闻吗

**机器人：** 你可以使用 `/news 科技` 来搜索最新的科技新闻。

**用户：** /news 科技

**机器人：** [显示科技相关的新闻]

## 故障排除

### 问题 1：无法获取新闻

**可能原因：**
- 网络连接问题
- 防火墙/代理拦截
- 新闻源 API 限流或不可用

**解决方案：**
1. 检查网络连接
2. 运行 `python test_news.py` 测试哪些新闻源可用
3. 如果所有源都不可用，考虑添加代理配置

### 问题 2：编译错误

**可能原因：**
- Go 模块依赖问题

**解决方案：**
```bash
go mod tidy
go build ./cmd/bot/
```

## 未来改进

- [ ] 添加更多新闻源（如微博热搜、知乎热榜）
- [ ] 支持新闻分类（科技、财经、娱乐等）
- [ ] 添加新闻缓存机制，减少 API 调用
- [ ] 支持自定义新闻源配置
- [ ] 添加新闻摘要功能（使用 Claude 总结）

## 相关文件

- `internal/newsearch/newsearch.go` - 新闻搜索核心实现
- `internal/newsearch/newsearch_test.go` - 单元测试
- `internal/command/router.go` - 命令路由和处理
- `config.yaml` - 系统提示词配置
- `test_news.py` - Python 测试脚本
- `get_news_now.py` - 快速获取新闻脚本

## 许可证

与主项目相同
