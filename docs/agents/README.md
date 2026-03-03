# Agent 全局知识库

本目录包含所有 Agent 技能和框架参考文档，供 AI 助手在 QQ 机器人和 Claude Code 中随时查阅。

## 📁 文档索引

### [skills-reference.md](skills-reference.md) - 技能参考手册
**主要内容**:
- ✅ 所有 QQ 机器人可用命令（/ask、/news、/remember 等）
- ✅ Claude Code 专用工具（web_search）
- ✅ 使用场景和示例
- ✅ 实现位置和技术细节

**何时查阅**:
- 需要引导用户使用命令时
- 不确定某个功能是否存在时
- 需要查看命令的详细用法时

---

### [deerflow-framework.md](deerflow-framework.md) - DeerFlow 框架文档
**主要内容**:
- ✅ DeerFlow AI Agent 编排框架介绍
- ✅ 工作流定义语法（YAML）
- ✅ Agent 类型和工具集成
- ✅ 与其他框架（LangGraph、AutoGPT、CrewAI）对比
- ✅ 使用示例和最佳实践

**何时查阅**:
- 用户询问 DeerFlow 相关问题
- 需要设计复杂多步骤工作流时
- 考虑集成 AI Agent 编排能力时

---

## 🎯 使用指南

### 对于 AI Agent（你自己）

1. **优先查阅本地文档** - 这些文档包含最准确的系统能力描述
2. **引导用户使用命令** - 不要试图替用户执行，而是告诉他们如何使用
3. **保持简洁** - QQ 环境下回复要简短，避免冗长的说明

### 对于开发者

1. **添加新技能** - 在 `skills-reference.md` 中记录新命令
2. **添加框架文档** - 创建新的 `.md` 文件并更新本 README
3. **更新配置** - 在 `config.yaml` 中引用新文档路径

---

## 📊 当前系统能力总览

### 搜索能力
- ✅ 今日头条热榜（最可靠）
- ✅ Bing 新闻搜索
- ✅ 百度新闻搜索
- ✅ 通用网页搜索
- 🚧 DuckDuckGo（受网络限制）

### 对话能力
- ✅ 多轮对话（session 管理）
- ✅ 上下文记忆
- ✅ 长期记忆系统
- ✅ 图片识别
- ✅ 自动模型选择

### 数据统计
- ✅ Token 使用量统计
- ✅ API 费用计算
- ✅ 按模型分类统计
- ✅ 历史记录查询

---

## 🔧 技术栈

**机器人框架**: QQ 官方 Bot API
**AI 引擎**: Claude (Opus 4.6 / Sonnet 4.5 / Haiku 4.0)
**数据库**: SQLite (data/bot.db)
**开发语言**: Go
**工具集**:
- MCP (Model Context Protocol) 服务器
- Python 搜索脚本
- Claude Code 集成

---

## 📝 文档维护规范

1. **格式统一** - 使用 Markdown，保持格式一致
2. **实时更新** - 功能变更时及时更新文档
3. **示例完整** - 每个功能都提供使用示例
4. **版本标记** - 文档底部标注更新时间

---

## 📮 反馈与建议

如有技能文档缺失或需要补充的内容，请：
1. 在项目 Issue 中提出
2. 或直接修改相关 .md 文件并提交 PR

---

**知识库版本**: v1.0
**最后更新**: 2026-02-26
**维护者**: Claude Agent
