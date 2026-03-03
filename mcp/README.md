# Web Search MCP Server

这是一个为 Claude Code 提供网络搜索功能的 MCP (Model Context Protocol) Server。

## 功能状态

### ✅ 已测试可用

#### 热点新闻（今日头条热榜）
- **状态**: 稳定可用
- **数据源**: 今日头条官方 API
- **更新频率**: 实时
- **可靠性**: 非常高 ⭐⭐⭐⭐⭐

这是目前最稳定的功能，可以获取当前最热门的新闻话题和热搜榜。

### ⚠️ 受限功能

由于网络环境限制，以下功能可能不稳定：
- 新闻搜索（Bing News）
- 网页搜索（Bing、DuckDuckGo）

**推荐**: 主要使用热点新闻功能，这是最可靠的搜索方式。

## 功能特性

- 🔥 今日头条热榜 - 获取实时热点话题
- 🌐 专注于中文内容
- 🚀 异步处理，性能优秀
- 📊 格式化输出

## 安装

### 1. 安装依赖

```bash
cd D:\myselfClow\mcp-server
pip install -r requirements.txt
```

或者在 Windows 上运行：
```bash
setup.bat
```

### 2. 配置 Claude Code

将以下配置添加到你的 Claude Code 配置文件中（通常在 `%APPDATA%\Claude\claude_desktop_config.json` 或类似位置）：

```json
{
  "mcpServers": {
    "search": {
      "command": "python",
      "args": [
        "D:\\myselfClow\\mcp-server\\search_server.py"
      ],
      "description": "Web search using Baidu, Bing, and Sogou"
    }
  }
}
```

**重要提示：**
- 确保路径使用双反斜杠 `\\` 或单正斜杠 `/`
- 确保 Python 在系统 PATH 中可用
- 如果 Claude Code 配置文件不存在，创建它

### 3. 重启 Claude Code

配置更改后，需要重启 Claude Code 才能生效。

## 使用方法

### 在 Claude Code 中使用

配置完成后，Claude Code 会自动检测到这个工具。你可以通过自然语言请求搜索：

**示例 1：基本搜索**
```
用户：搜索一下"Python 编程教程"
Claude：[自动调用 web_search 工具]
```

**示例 2：指定搜索引擎**
```
用户：用百度搜索"人工智能最新进展"
Claude：[调用 web_search 工具，engine="baidu"]
```

**示例 3：搜索最新信息**
```
用户：今天有什么科技新闻？
Claude：[调用 web_search 工具搜索"今日科技新闻"]
```

### 工具参数

`web_search` 工具接受以下参数：

- **query** (必需): 搜索查询词
- **search_type** (可选): 搜索类型：
  - `"hot"` - 热点新闻（**推荐，最稳定**）
  - `"news"` - 新闻搜索（不稳定）
  - `"web"` - 网页搜索（不稳定）
- **count** (可选): 返回结果数量，范围 1-20，默认 10

**推荐用法**:
```python
web_search(query="热点", search_type="hot")  # 获取今日热点
```

### 输出格式

搜索结果包含：
- 标题
- 来源（搜索引擎）
- 链接
- 描述（摘要）

示例输出：
```
Search results for 'Python 编程' (10 results):

1. Python 官方教程
   来源: Baidu
   链接: https://docs.python.org/zh-cn/
   描述: Python 官方中文文档和教程...

2. Python 入门指南
   来源: Bing
   链接: https://example.com/python-guide
   描述: 适合初学者的 Python 编程指南...

...
```

## 测试

### 运行测试脚本

```bash
cd D:\myselfClow\mcp-server
python test_search.py
```

测试脚本会：
1. 测试所有搜索引擎
2. 使用多个查询测试
3. 显示搜索结果
4. 生成测试报告

### 预期输出

```
🔍 MCP Search Server Test Suite
======================================================================

Testing Baidu with query: 'Python 编程'
======================================================================
✅ Baidu returned 5 results:

1. Python官方教程
   链接: https://docs.python.org/zh-cn/
   描述: Python是一种易于学习又功能强大的编程语言...

...

📊 Test Summary
======================================================================
✅ Baidu      3/3 queries successful
✅ Bing       2/3 queries successful
⚠️  Sogou     1/3 queries successful

✅ Overall: 6/9 tests passed
🎉 At least one search engine is working!
```

## 故障排除

### 问题 1: "mcp package not found"

**解决方案：**
```bash
pip install mcp
```

### 问题 2: "httpx package not found"

**解决方案：**
```bash
pip install httpx
```

### 问题 3: Claude Code 无法连接到 MCP Server

**可能原因：**
- Python 路径不正确
- MCP server 脚本路径不正确
- 权限问题

**解决方案：**
1. 检查 Python 是否在 PATH 中：`python --version`
2. 检查 MCP server 路径是否正确
3. 尝试手动运行：`python D:\myselfClow\mcp-server\search_server.py`
4. 查看 Claude Code 的日志文件

### 问题 4: 搜索结果为空

**可能原因：**
- 网络连接问题
- 搜索引擎限流
- 防火墙/代理拦截

**解决方案：**
1. 运行 `test_search.py` 测试哪些引擎可用
2. 检查网络连接
3. 尝试使用不同的搜索引擎
4. 如果在公司网络，可能需要配置代理

### 问题 5: 编码错误

**解决方案：**
确保文件以 UTF-8 编码保存，在文件开头添加：
```python
# -*- coding: utf-8 -*-
```

## 系统提示词配置

为了让 Claude 更好地使用搜索工具，建议在 `config.yaml` 的系统提示词中添加：

```yaml
system_prompt: |
  ## 可用工具
  ### web_search - 网络搜索工具
  你可以使用 web_search 工具在互联网上搜索信息。

  **使用场景：**
  - 用户询问最新信息、新闻、实时数据
  - 用户询问你不知道的信息
  - 用户明确要求搜索

  **使用方法：**
  当用户询问需要搜索的内容时，主动使用 web_search 工具。

  **示例：**
  - 用户："今天的新闻是什么"
    → 使用 web_search(query="今日新闻")

  - 用户："搜索 Python 教程"
    → 使用 web_search(query="Python 教程")

  - 用户："最新的 AI 技术有哪些"
    → 使用 web_search(query="最新AI技术")
```

## 工作原理

### 架构

```
Claude Code
    ↓
MCP Protocol (stdio)
    ↓
search_server.py
    ↓
┌─────────┬────────┬────────┐
│ Baidu   │ Bing   │ Sogou  │
└─────────┴────────┴────────┘
```

### 搜索流程

1. Claude Code 调用 `web_search` 工具
2. MCP Server 接收请求
3. 并发向多个搜索引擎发送请求
4. 解析 HTML 响应，提取结果
5. 去重和格式化
6. 返回给 Claude Code

### 技术栈

- **MCP SDK**: Model Context Protocol Python SDK
- **httpx**: 现代的异步 HTTP 客户端
- **asyncio**: Python 异步编程
- **正则表达式**: HTML 解析

## 高级配置

### 自定义搜索引擎

可以在 `search_server.py` 中添加新的搜索引擎：

```python
class MySearchEngine(SearchEngine):
    async def search(self, query: str, count: int = 10) -> list[dict]:
        # 实现搜索逻辑
        ...
```

### 配置代理

在 `SearchEngine.__init__` 中添加代理配置：

```python
self.client = httpx.AsyncClient(
    timeout=15.0,
    proxies="http://proxy.example.com:8080"
)
```

### 增加搜索结果数量

修改工具定义中的 `maximum` 值：

```python
"count": {
    "type": "integer",
    "description": "Number of results",
    "minimum": 1,
    "maximum": 50,  # 改为 50
    "default": 10
}
```

## 限制

- 搜索引擎可能会限流或阻止自动化请求
- HTML 结构变化可能导致解析失败
- 某些搜索引擎可能需要验证码
- 搜索结果质量取决于搜索引擎

## 未来改进

- [ ] 添加更多搜索引擎
- [ ] 实现搜索结果缓存
- [ ] 支持图片搜索
- [ ] 添加搜索历史记录
- [ ] 实现更健壮的 HTML 解析
- [ ] 添加代理池支持
- [ ] 支持搜索结果过滤和排序

## 相关文件

- `search_server.py` - MCP Server 主程序
- `test_search.py` - 测试脚本
- `requirements.txt` - Python 依赖
- `setup.bat` - Windows 安装脚本
- `claude_code_config.json` - Claude Code 配置示例
- `README.md` - 本文档

## 许可证

与主项目相同

## 支持

如有问题，请检查：
1. Python 和依赖是否正确安装
2. 网络连接是否正常
3. Claude Code 配置是否正确
4. 运行测试脚本诊断问题
