#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""
MCP Server for Web Search - Version 3
Enhanced version with multiple reliable search engines and fallback mechanism
"""

import asyncio
import json
import sys
from typing import Any, Sequence, Optional
import httpx
from urllib.parse import quote, urlencode
import re
from datetime import datetime

# MCP SDK imports
try:
    from mcp.server import Server
    from mcp.server.stdio import stdio_server
    from mcp.types import Tool, TextContent
except ImportError:
    print("Error: mcp package not found. Install with: pip install mcp", file=sys.stderr)
    sys.exit(1)

# Initialize MCP server
app = Server("search-server-v3")


class SearchResult:
    """Unified search result structure"""
    def __init__(self, title: str, url: str, description: str = "", source: str = "", date: str = ""):
        self.title = title
        self.url = url
        self.description = description
        self.source = source
        self.date = date

    def to_dict(self) -> dict:
        return {
            'title': self.title,
            'url': self.url,
            'description': self.description,
            'source': self.source,
            'date': self.date
        }


class BaseSearchEngine:
    """Base class for search engines"""

    def __init__(self):
        self.client = httpx.AsyncClient(
            timeout=15.0,
            follow_redirects=True,
            headers={
                'User-Agent': 'Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36'
            }
        )

    async def search(self, query: str, count: int = 10) -> list[SearchResult]:
        """Override this method in subclasses"""
        raise NotImplementedError

    async def close(self):
        """Close the HTTP client"""
        await self.client.aclose()


class ToutiaoHotSearcher(BaseSearchEngine):
    """今日头条热榜搜索"""

    def name(self) -> str:
        return "今日头条热榜"

    async def get_hot_news(self, count: int = 10) -> list[SearchResult]:
        """Get hot news from Toutiao"""
        url = "https://www.toutiao.com/hot-event/hot-board/?origin=toutiao_pc"

        try:
            response = await self.client.get(url)
            response.raise_for_status()
            data = response.json()

            if 'data' not in data:
                return []

            results = []
            for item in data['data'][:count]:
                title = item.get('Title', '')
                cluster_id = item.get('ClusterId', '')
                hot_value = item.get('HotValue', '0')

                if title:
                    results.append(SearchResult(
                        title=title,
                        url=f'https://www.toutiao.com/trending/{cluster_id}/' if cluster_id else '',
                        description=f'热度: {hot_value}',
                        source='今日头条热榜'
                    ))

            return results

        except Exception as e:
            print(f"Toutiao error: {e}", file=sys.stderr)
            return []


class BingNewsSearcher(BaseSearchEngine):
    """Bing News 搜索"""

    def name(self) -> str:
        return "Bing News"

    async def search(self, query: str, count: int = 10) -> list[SearchResult]:
        """Search news using Bing News RSS"""
        url = f"https://www.bing.com/news/search?q={quote(query)}&format=rss"

        try:
            response = await self.client.get(url)
            response.raise_for_status()
            content = response.text

            results = []
            items = content.split('<item>')

            for item_text in items[1:count+1]:
                # Extract title
                title_match = re.search(r'<title><!\[CDATA\[(.*?)\]\]></title>', item_text)
                if not title_match:
                    title_match = re.search(r'<title>(.*?)</title>', item_text)

                # Extract link
                link_match = re.search(r'<link>(.*?)</link>', item_text)

                # Extract description
                desc_match = re.search(r'<description><!\[CDATA\[(.*?)\]\]></description>', item_text)
                if not desc_match:
                    desc_match = re.search(r'<description>(.*?)</description>', item_text)

                # Extract pubDate
                date_match = re.search(r'<pubDate>(.*?)</pubDate>', item_text)

                if title_match:
                    title = title_match.group(1).strip()
                    url_str = link_match.group(1).strip() if link_match else ''
                    description = desc_match.group(1).strip() if desc_match else ''
                    pub_date = date_match.group(1).strip() if date_match else ''

                    # Clean HTML tags
                    description = re.sub(r'<[^>]+>', '', description)

                    results.append(SearchResult(
                        title=title,
                        url=url_str,
                        description=description[:200],
                        source='Bing News',
                        date=pub_date
                    ))

            return results

        except Exception as e:
            print(f"Bing News error: {e}", file=sys.stderr)
            return []


class BaiduSearcher(BaseSearchEngine):
    """百度搜索"""

    def name(self) -> str:
        return "百度搜索"

    async def search(self, query: str, count: int = 10) -> list[SearchResult]:
        """Search using Baidu"""
        url = f"https://www.baidu.com/s?wd={quote(query)}&rn={count}"

        try:
            response = await self.client.get(url)
            response.raise_for_status()
            html = response.text

            results = []

            # Parse Baidu search results
            # Look for result containers
            pattern = r'<div[^>]*class="result[^"]*"[^>]*>.*?<h3[^>]*>.*?<a[^>]*href="([^"]*)"[^>]*>(.*?)</a>.*?</h3>.*?<span[^>]*class="content-right_[^"]*"[^>]*>(.*?)</span>'
            matches = re.findall(pattern, html, re.DOTALL)

            for match in matches[:count]:
                url_str, title, description = match

                # Clean HTML tags
                title = re.sub(r'<[^>]+>', '', title).strip()
                description = re.sub(r'<[^>]+>', '', description).strip()

                # Decode HTML entities
                title = self._decode_html_entities(title)
                description = self._decode_html_entities(description)

                if title and url_str:
                    results.append(SearchResult(
                        title=title,
                        url=url_str,
                        description=description[:200],
                        source='百度搜索'
                    ))

            # Fallback: simpler pattern if the above doesn't work
            if not results:
                pattern2 = r'<h3[^>]*class="[^"]*t[^"]*"[^>]*>.*?<a[^>]*href="([^"]*)"[^>]*>(.*?)</a>'
                matches2 = re.findall(pattern2, html, re.DOTALL)

                for match in matches2[:count]:
                    url_str, title = match
                    title = re.sub(r'<[^>]+>', '', title).strip()
                    title = self._decode_html_entities(title)

                    if title and url_str:
                        results.append(SearchResult(
                            title=title,
                            url=url_str,
                            description='',
                            source='百度搜索'
                        ))

            return results

        except Exception as e:
            print(f"Baidu search error: {e}", file=sys.stderr)
            return []

    def _decode_html_entities(self, text: str) -> str:
        """Decode common HTML entities"""
        text = text.replace('&lt;', '<')
        text = text.replace('&gt;', '>')
        text = text.replace('&amp;', '&')
        text = text.replace('&quot;', '"')
        text = text.replace('&#39;', "'")
        text = text.replace('&nbsp;', ' ')
        return text


class DuckDuckGoSearcher(BaseSearchEngine):
    """DuckDuckGo 搜索"""

    def name(self) -> str:
        return "DuckDuckGo"

    async def search(self, query: str, count: int = 10) -> list[SearchResult]:
        """Search using DuckDuckGo"""
        url = "https://html.duckduckgo.com/html/"

        try:
            # DuckDuckGo requires POST
            response = await self.client.post(
                url,
                data={'q': query, 'kl': 'cn-zh'},
                headers={
                    'Content-Type': 'application/x-www-form-urlencoded',
                    'User-Agent': 'Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36'
                }
            )
            response.raise_for_status()
            html = response.text

            results = []

            # Pattern 1: result__a and result__snippet
            pattern1 = r'<a[^>]*class="result__a"[^>]*href="([^"]*)"[^>]*>(.*?)</a>.*?<a[^>]*class="result__snippet"[^>]*>(.*?)</a>'
            matches = re.findall(pattern1, html, re.DOTALL)

            for match in matches[:count]:
                url_str, title, description = match

                # Clean HTML tags
                title = re.sub(r'<[^>]+>', '', title).strip()
                description = re.sub(r'<[^>]+>', '', description).strip()

                if title:
                    results.append(SearchResult(
                        title=title,
                        url=url_str,
                        description=description[:200],
                        source='DuckDuckGo'
                    ))

            return results

        except Exception as e:
            print(f"DuckDuckGo error: {e}", file=sys.stderr)
            return []


class GoogleCustomSearcher(BaseSearchEngine):
    """Google 自定义搜索 (需要 API Key)"""

    def __init__(self, api_key: Optional[str] = None, cx: Optional[str] = None):
        super().__init__()
        self.api_key = api_key
        self.cx = cx

    def name(self) -> str:
        return "Google Custom Search"

    async def search(self, query: str, count: int = 10) -> list[SearchResult]:
        """Search using Google Custom Search API"""
        if not self.api_key or not self.cx:
            print("Google Custom Search: API key or CX not configured", file=sys.stderr)
            return []

        url = "https://www.googleapis.com/customsearch/v1"
        params = {
            'key': self.api_key,
            'cx': self.cx,
            'q': query,
            'num': min(count, 10)  # Google API max is 10
        }

        try:
            response = await self.client.get(url, params=params)
            response.raise_for_status()
            data = response.json()

            results = []

            if 'items' in data:
                for item in data['items']:
                    results.append(SearchResult(
                        title=item.get('title', ''),
                        url=item.get('link', ''),
                        description=item.get('snippet', ''),
                        source='Google'
                    ))

            return results

        except Exception as e:
            print(f"Google Custom Search error: {e}", file=sys.stderr)
            return []


class MultiSearchEngine:
    """聚合多个搜索引擎，提供降级和重试机制"""

    def __init__(self):
        self.engines = {
            'toutiao': ToutiaoHotSearcher(),
            'bing': BingNewsSearcher(),
            'baidu': BaiduSearcher(),
            'duckduckgo': DuckDuckGoSearcher(),
        }

        # Add Google if API keys are configured
        # google_api_key = os.getenv('GOOGLE_API_KEY')
        # google_cx = os.getenv('GOOGLE_CX')
        # if google_api_key and google_cx:
        #     self.engines['google'] = GoogleCustomSearcher(google_api_key, google_cx)

    async def search(self, query: str, search_type: str = 'web', count: int = 10) -> list[SearchResult]:
        """
        执行搜索，自动尝试多个引擎直到成功

        Args:
            query: 搜索关键词
            search_type: 搜索类型 ('hot', 'news', 'web')
            count: 返回结果数量

        Returns:
            搜索结果列表
        """
        if search_type == 'hot':
            # 只使用今日头条热榜
            return await self.engines['toutiao'].get_hot_news(count)

        elif search_type == 'news':
            # 优先使用新闻引擎
            engine_order = ['bing', 'baidu', 'duckduckgo']

        else:  # web
            # 通用网页搜索
            engine_order = ['baidu', 'duckduckgo', 'bing']

        # 尝试每个引擎
        for engine_name in engine_order:
            engine = self.engines.get(engine_name)
            if not engine:
                continue

            try:
                print(f"Trying search engine: {engine_name}", file=sys.stderr)
                results = await engine.search(query, count)

                if results and len(results) > 0:
                    print(f"Success with {engine_name}: {len(results)} results", file=sys.stderr)
                    return results

            except Exception as e:
                print(f"Engine {engine_name} failed: {e}", file=sys.stderr)
                continue

        # 所有引擎都失败
        return []

    async def close_all(self):
        """关闭所有搜索引擎的连接"""
        for engine in self.engines.values():
            try:
                await engine.close()
            except:
                pass


# Global multi-engine searcher
multi_searcher = MultiSearchEngine()


@app.list_tools()
async def list_tools() -> list[Tool]:
    """List available tools"""
    return [
        Tool(
            name="web_search",
            description="""搜索网络信息，支持多种搜索类型和引擎。

功能：
- 获取实时热点新闻（今日头条热榜）
- 搜索新闻资讯（Bing News、百度新闻）
- 通用网页搜索（百度、DuckDuckGo）

搜索引擎自动降级：如果首选引擎失败，会自动尝试备用引擎。

参数：
- query (必需): 搜索关键词
- search_type (可选): 搜索类型
  * "hot" - 获取今日头条热榜（忽略 query）
  * "news" - 搜索新闻
  * "web" - 网页搜索
  默认: "web"
- count (可选): 返回结果数量 (1-20)，默认: 10

示例：
- 获取热点: web_search(query="热点", search_type="hot")
- 搜索新闻: web_search(query="科技新闻", search_type="news")
- 网页搜索: web_search(query="Python教程", search_type="web")
""",
            inputSchema={
                "type": "object",
                "properties": {
                    "query": {
                        "type": "string",
                        "description": "搜索关键词"
                    },
                    "search_type": {
                        "type": "string",
                        "enum": ["web", "news", "hot"],
                        "description": "搜索类型：web=网页搜索, news=新闻搜索, hot=热点新闻",
                        "default": "web"
                    },
                    "count": {
                        "type": "integer",
                        "description": "返回结果数量",
                        "minimum": 1,
                        "maximum": 20,
                        "default": 10
                    }
                },
                "required": ["query"]
            }
        )
    ]


@app.call_tool()
async def call_tool(name: str, arguments: Any) -> Sequence[TextContent]:
    """Handle tool calls"""

    if name != "web_search":
        return [TextContent(type="text", text=f"Unknown tool: {name}")]

    query = arguments.get("query", "")
    if not query:
        return [TextContent(type="text", text="Error: query parameter is required")]

    search_type = arguments.get("search_type", "web").lower()
    count = arguments.get("count", 10)

    # Validate count
    count = max(1, min(20, count))

    # Perform search
    try:
        results = await multi_searcher.search(query, search_type, count)
    except Exception as e:
        return [TextContent(
            type="text",
            text=f"搜索失败: {str(e)}"
        )]

    # Format results
    if not results:
        return [TextContent(
            type="text",
            text=f"未找到相关结果: {query}"
        )]

    # Format output
    search_type_name = {
        'hot': '热点新闻',
        'news': '新闻搜索',
        'web': '网页搜索'
    }.get(search_type, '搜索')

    output = f"【{search_type_name}】查询: {query}\n"
    output += f"找到 {len(results)} 条结果\n\n"

    for i, result in enumerate(results, 1):
        result_dict = result.to_dict()
        output += f"{i}. {result_dict['title']}\n"

        if result_dict.get('source'):
            output += f"   来源: {result_dict['source']}\n"

        if result_dict.get('description'):
            desc = result_dict['description']
            if len(desc) > 150:
                desc = desc[:150] + "..."
            output += f"   {desc}\n"

        if result_dict.get('date'):
            output += f"   时间: {result_dict['date']}\n"

        if result_dict.get('url'):
            output += f"   链接: {result_dict['url']}\n"

        output += "\n"

    return [TextContent(type="text", text=output.strip())]


async def main():
    """Run the MCP server"""
    try:
        async with stdio_server() as (read_stream, write_stream):
            await app.run(
                read_stream,
                write_stream,
                app.create_initialization_options()
            )
    finally:
        await multi_searcher.close_all()


if __name__ == "__main__":
    asyncio.run(main())
