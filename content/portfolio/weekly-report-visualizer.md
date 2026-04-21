---
title: 周报数据可视化仪表盘
slug: weekly-report-visualizer
description: 把枯燥的周报表格变成可交互的热力图，一眼看出瓶颈
category: 可视化
tags: [dataviz, d3]
order: 1
demo_url: https://example.com/demo/weekly
source_url: https://github.com/example/weekly-viz
created: 2026-03-05
updated: 2026-03-18
status: published
featured: true
---
<!-- portfolio:intro -->
一个**团队周报**的可视化工具。把每周提交的纯文本周报解析成结构化数据，用 D3 画交互式热力图。高亮**连续投入的专题**和**突然掉线的同学**，让 leader 一眼看出团队的注意力分配。
<!-- /portfolio:intro -->

# 项目背景

团队十几个人的周报堆在 Wiki 里，谁也没耐心翻。我写了个小工具：每周扫描所有周报，抽取关键词和时长，渲染成一张按人 × 主题的热力网格。

## 核心特性

- **解析器**：用正则 + 关键词白名单把中文周报切片到"主题/时长/进度"三元组
- **可视化**：D3 + Canvas 画热力图，悬停看原文
- **回溯**：时间轴拖动对比任意两周

## 技术栈

- 前端：Vite + D3.js + Canvas API
- 后端：FastAPI 解析服务
- 部署：静态 + Serverless

用了大概两周下班时间做完，团队反响还不错。
