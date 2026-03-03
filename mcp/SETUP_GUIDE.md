# MCP Search Server 配置指南

## 快速开始

### 1. 安装依赖

```bash
cd D:\myselfClow\mcp-server
pip install -r requirements.txt
```

或者运行：
```bash
setup.bat
```

### 2. 测试功能

```bash
python test_v2.py
```

预期输出：
```
✅ PASS Hot News
❌ FAIL News Search
❌ FAIL Web Search

Total: 1/3 tests passed
✅ At least one search method is working!
```

只要热点新闻（Hot News）测试通过，服务就可以正常使用。

### 3. 配置 Claude Code

找到你的 Claude Code 配置文件，通常在以下位置之一：

**Windows:**
- `%APPDATA%\Claude\claude_desktop_config.json`
- `C:\Users\<用户名>\AppData\Roaming\Claude\claude_desktop_config.json`

**如果文件不存在，创建它。**

添加以下配置：

```json
{
  "mcpServers": {
    "search": {
      "command": "python",
      "args": [
        "D:\\myselfClow\\mcp-server\\search_server_v2.py"
      ],
      "description": "Web search with hot news, news search, and general web search"
    }
  }
}
```

**重要提示：**
- 路径必须使用双反斜杠 `\\` 或单正斜杠 `/`
- 确保 Python 在系统 PATH 中可用，可以通过 `python --version` 验证
- 如果 Claude Code 已经有其他 MCP servers，将 "search" 配置添加到现有的 "mcpServers" 对象中

### 4. 重启 Claude Code

保存配置后，**完全退出并重启 Claude Code**。

## 验证配置

### 方法 1：查看可用工具

在 Claude Code 中输入：

```
列出所有可用的工具
```

Claude 应该会列出包含 `web_search` 工具的列表。

### 方法 2：直接测试搜索

在 Claude Code 中输入：

```
帮我搜索一下今天的热点新闻
```

Claude 应该会调用 `web_search` 工具并返回今日头条的热榜。

## 使用示例

### 示例 1：获取热点新闻

```
用户：今天有什么热点新闻？
Claude：[调用 web_search(query="热点", search_type="hot")]
       [显示今日头条热榜 Top 10]
```

### 示例 2：搜索特定主题

```
用户：搜索一下最新的 AI 技术
Claude：[调用 web_search(query="最新AI技术", search_type="news")]
       [显示相关新闻]
```

### 示例 3：一般网络搜索

```
用户：Python 编程教程
Claude：[调用 web_search(query="Python 编程教程", search_type="web")]
       [显示搜索结果]
```

## 功能说明

### 支持的搜索类型

1. **热点新闻 (hot)**
   - 来源：今日头条热榜
   - 特点：实时更新，数据可靠
   - 状态：✅ 已测试，完全可用
   - 推荐度：⭐⭐⭐⭐⭐

2. **新闻搜索 (news)**
   - 来源：Bing News RSS
   - 特点：支持关键词搜索
   - 状态：⚠️ 可能被限流
   - 推荐度：⭐⭐⭐

3. **网页搜索 (web)**
   - 来源：DuckDuckGo
   - 特点：通用搜索
   - 状态：⚠️ 不稳定
   - 推荐度：⭐⭐

**建议：优先使用 search_type="hot" 获取热点新闻，最稳定可靠。**

## 常见问题

### Q1: Claude Code 提示找不到工具

**可能原因：**
- 配置文件路径错误
- Python 路径不正确
- MCP server 脚本路径错误

**解决方案：**
1. 确认配置文件位置正确
2. 运行 `python --version` 确认 Python 可用
3. 手动运行 MCP server 测试：
   ```bash
   python D:\myselfClow\mcp-server\search_server_v2.py
   ```
   （应该不会有输出，但也不应该报错）

### Q2: 搜索返回空结果

**可能原因：**
- 网络连接问题
- 搜索引擎限流

**解决方案：**
1. 运行 `python test_v2.py` 检查哪些搜索引擎可用
2. 尝试使用 search_type="hot" 获取热点新闻（最可靠）
3. 检查网络连接和防火墙设置

### Q3: Claude Code 无法启动 MCP server

**可能原因：**
- 依赖未安装
- Python 环境问题

**解决方案：**
```bash
pip install mcp httpx
python test_v2.py  # 测试是否能正常运行
```

### Q4: 配置文件在哪里？

**查找方法：**

1. **在 Claude Code 中查找：**
   - 打开 Claude Code
   - 查看设置/配置选项
   - 查找 "MCP" 或 "配置文件路径"

2. **手动查找：**
   ```bash
   # Windows
   dir %APPDATA%\Claude\*.json /s

   # 或者
   dir C:\Users\%USERNAME%\AppData\Roaming\Claude\*.json /s
   ```

3. **如果找不到，创建：**
   ```bash
   mkdir %APPDATA%\Claude
   notepad %APPDATA%\Claude\claude_desktop_config.json
   ```
   然后粘贴配置内容。

## 高级配置

### 自定义返回结果数量

编辑 `search_server_v2.py`，修改默认值：

```python
"count": {
    "type": "integer",
    "description": "Number of results",
    "minimum": 1,
    "maximum": 50,  # 改为 50
    "default": 20   # 默认改为 20
}
```

### 添加代理支持

在 `NewsSearcher` 和 `WebSearcher` 的 `__init__` 方法中：

```python
self.client = httpx.AsyncClient(
    timeout=15.0,
    proxies="http://proxy.example.com:8080",  # 添加这一行
    headers={...}
)
```

### 调试 MCP Server

如果需要调试，可以添加日志：

```python
import logging
logging.basicConfig(level=logging.DEBUG)
```

然后查看 Claude Code 的日志输出。

## 性能优化

### 1. 增加超时时间

如果网络慢，可以增加超时：

```python
self.client = httpx.AsyncClient(
    timeout=30.0,  # 改为 30 秒
    ...
)
```

### 2. 缓存搜索结果

可以添加简单的缓存机制（在 `search_server_v2.py` 中）：

```python
cache = {}

async def get_hot_news(self, count: int = 10) -> list[dict]:
    cache_key = f"hot_{count}"
    if cache_key in cache:
        cached_time, results = cache[cache_key]
        if time.time() - cached_time < 300:  # 5 分钟缓存
            return results

    # ... 原有代码 ...

    cache[cache_key] = (time.time(), results)
    return results
```

## 与 QQ 机器人集成

系统提示词已经配置了两种搜索方式：

1. **在 QQ 机器人中：** 推荐用户使用 `/news` 命令
2. **在 Claude Code 中：** 使用 `web_search` 工具

这样用户在不同环境下都能获取新闻信息。

## 安全性说明

- MCP Server 只进行只读操作（HTTP GET/POST 请求）
- 不会修改本地文件或系统设置
- 所有网络请求都是公开的搜索引擎 API
- 不会收集或上传用户数据

## 卸载

如果需要移除 MCP Server：

1. 从 Claude Code 配置文件中删除 "search" 配置
2. 重启 Claude Code
3. 可选：删除 `D:\myselfClow\mcp-server` 目录

## 获取帮助

遇到问题？

1. 运行 `python test_v2.py` 诊断问题
2. 检查 Claude Code 日志
3. 查看本文档的"常见问题"部分
4. 确保网络连接正常

## 更新日志

### v2 (当前版本)
- ✅ 使用 API 而非 HTML 解析，更可靠
- ✅ 支持今日头条热榜（主要功能）
- ✅ 支持 Bing News RSS 新闻搜索
- ⚠️ DuckDuckGo 搜索（实验性）

### v1 (已弃用)
- ❌ 使用 HTML 解析，不稳定
- ❌ 百度、搜狗搜索解析复杂
