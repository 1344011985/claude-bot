package command

import "context"

// --- /help handler ---

type helpHandler struct{}

func (h *helpHandler) Handle(ctx context.Context, msg *IncomingMessage) (string, error) {
	return `/ask <问题>      — 向 Claude 提问（续接上下文）
/new             — 开启新对话，清除当前 session
/remember <内容>  — 保存长期记忆，每次对话自动注入
/forget          — 清除所有长期记忆
/history [n]     — 查看最近 n 条对话（默认 5）
/news [关键词]   — 搜索最新新闻（不带关键词则显示热点）
/browse <url> [指令] — 打开网页，AI 分析（加截图关键词可截图）
/skill list      — 查看所有 skill
/skill add <名称> | <prompt> [| 触发词1,触发词2]
/skill show/enable/disable/delete <id>
/help            — 显示此帮助
/version         — 显示版本信息
直接发消息等同于 /ask

模型切换：发送"切换模型为sonnet"、"使用opus"等即可切换
可选模型：haiku(快速) / sonnet(均衡) / opus(最强) / auto(自动)`, nil
}