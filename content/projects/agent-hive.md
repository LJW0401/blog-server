---
slug: agent-hive
repo: LJW0401/agent_hive
display_name: agent-hive
display_desc: 一个多 Agent 协作编排框架，探索如何让多个 LLM agent 像"蜂群"一样协同完成复杂任务。
category: 开发者工具
stack: [Python, LLM, Agent]
status: active
featured: true
created: 2026-03-20
updated: 2026-04-17
---

# agent-hive

> Repo: [LJW0401/agent_hive](https://github.com/LJW0401/agent_hive)

一个 **多 Agent 协作编排** 的实验性框架——目标是让多个具有不同角色（规划者 / 执行者 / 评审者 / 记忆管理者）的 LLM agent 协同工作完成单个 agent 做不好的任务。

## 为什么做

单一 agent 有两个天然天花板：
1. **上下文窗口有限**——长任务会丢失早期状态
2. **角色混杂**——既做"规划"又做"执行"时容易跑偏

把任务拆给多个专职 agent，再用一个协调层让它们交换信息，是目前业界比较有希望的解法（参见 AutoGen、CrewAI 等）。

## 和已有框架的差异

- **协调层可插拔**：不绑死某种"群聊"模式；也支持流水线、黑板模式、主从模式
- **失败可追溯**：每一步决策带完整的提示 + 回复 + 代价，方便事后复盘
- **成本可见**：每个 agent 的 token 消耗、调用次数、延迟都有结构化统计

## 现状

- 核心运行时稳定，可跑通典型的 "规划→执行→评审" 三角形
- Lark（飞书）集成还在开发中——这是另一个项目 [lark-agent-bridge](/projects/lark-agent-bridge) 的目标

欢迎 issue / PR。
