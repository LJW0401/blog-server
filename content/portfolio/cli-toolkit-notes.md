---
title: 命令行工具链小记合集
slug: cli-toolkit-notes
description: 这几年积累的 shell / Go / Python 小工具合集，每个都解决一个具体的日常痛点
category: 写作合集
tags: [cli, shell, tools]
order: 10
created: 2026-01-10
updated: 2026-03-22
status: published
featured: true
---
<!-- portfolio:intro -->
过去两三年，每遇到一个需要**重复做的操作**，我都会写一个小工具记下来。这是这些工具的**目录索引**和简短评注。语言混用（Shell / Go / Python），每个都在 50 ~ 300 行之间，都自己在用。
<!-- /portfolio:intro -->

# 工具索引

## `commit-wizard.sh`
把 git 状态、diff、最近 commit 整合成提示喂给 LLM，生成建议的提交信息。懒人福音。

## `port-scout` (Go)
列当前机器监听的所有端口 + 占用进程 + 启动时间，比 `lsof` 输出更紧凑。

## `md2anki.py`
把 Markdown 的二级标题 + 代码块自动转成 Anki 卡牌。用于背 Go 标准库。

## `git-branch-age.sh`
按 `git for-each-ref` 列所有本地分支 + 最后 commit 时间，方便定期打扫。

## `ytm-trim.sh`
从 YouTube URL 下载音频 + 剪裁 + 改 tag 一条龙，给我自己做音乐 archive。

# 心得

小工具不追求通用性，**只服务当下这台机器、这个流程**。每个都能独立替换。长期看，这种小颗粒积累比想"一次做一个大工具"省力得多。
