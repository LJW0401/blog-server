---
title: 暗色模式设计系统
slug: dark-mode-design-system
description: 从 CSS 变量到 tokens — 一次性整理整个站点的配色
category: 设计
tags: [design, css]
order: 2
source_url: https://github.com/example/dark-mode
created: 2026-02-14
updated: 2026-02-28
status: published
featured: true
---
<!-- portfolio:intro -->
给本站点整理的**统一暗色主题**。所有颜色走 CSS 变量，跟随 `prefers-color-scheme` 自动切换，同时支持用户手动强制。核心是把硬编码的十六进制值归纳成一套**语义 token**（`--fg-soft` / `--bg-card` / `--accent-hover`），改主题只需改一层。
<!-- /portfolio:intro -->

# 为什么重做

之前散落在各处的颜色值加起来 40+ 个，改暗色模式等于每个文件都要翻一遍。一次长期维护的代价高得吓人。

## 重构步骤

1. 审计所有颜色值（`grep -E "#[0-9a-f]{6}"` + 人工归类）
2. 归纳成 12 个语义 token
3. 替换所有硬编码为 `var(--*)`
4. 写两套配色（light/dark），用 `[data-theme]` 切换
5. 记录决策到 ARCHITECTURE.md

整体上一周搞定，后续加新页面再也不用重复考虑配色。
