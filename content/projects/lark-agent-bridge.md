---
slug: lark-agent-bridge
repo: LJW0401/lark-agent-bridge
display_name: lark-agent-bridge
display_desc: 把 LLM agent 接入飞书（Lark）生态的桥接层——让 agent 能在群聊、文档、表格里直接协作。
category: 开发者工具
stack: [Python, Lark, LLM, Agent]
status: developing
featured: true
created: 2026-03-08
updated: 2026-04-15
---

# lark-agent-bridge

> Repo: [LJW0401/lark-agent-bridge](https://github.com/LJW0401/lark-agent-bridge)

**把 LLM Agent 接入飞书生态的桥接层**。让 [agent-hive](/projects/agent-hive) 里的 agent 能在飞书群聊、文档、多维表格里像人类成员一样协作。

## 为什么做

AI agent 大部分的"人机交互"还停留在网页对话框，但真实工作场景是：
- 群聊里被 @，在群里直接回答
- 文档被评论，去文档里回评论
- 表格里有新行，按规则处理新行

用户不该为了用 agent 切换到另一个界面——**agent 应该长在现有工作流里**。

## 架构

```
飞书事件回调 ──▶ lark-agent-bridge ──▶ agent-hive (LLM 推理)
                       │                    │
                       ▼                    ▼
               飞书 OpenAPI          Trace + Logs
               （发消息/改文档/更新表格）
```

bridge 层只做两件事：
1. **翻译**：飞书事件 ↔ agent 理解的结构化任务
2. **执行反馈**：agent 的行动通过飞书 API 落地

## 现状

- 群聊 @ 场景已打通，能回简单问答
- 文档评论回复、表格自动化在开发中
- 最大的难点：权限模型——agent 代替用户操作时用哪个 token？这涉及飞书的"应用身份 vs 用户身份"复杂决策

## 相关

- 推理层：[agent-hive](/projects/agent-hive)
- 部署：目前是单进程 + SQLite，支持单租户
