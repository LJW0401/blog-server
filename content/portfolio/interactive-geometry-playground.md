---
title: 交互式几何演算场
slug: interactive-geometry-playground
description: 拖拽点和线，实时看定理怎么运作
category: 可视化
tags: [dataviz, math]
order: 4
demo_url: https://example.com/demo/geom
created: 2026-02-01
updated: 2026-02-10
status: published
featured: false
---
<!-- portfolio:intro -->
一个**几何定理**的交互式演示网页。点击三角形顶点拖动，观察外接圆、重心、欧拉线如何随形状变化。做这个的初衷是教小孩子"定理不是死记硬背的公式，是图形的稳定规律"。
<!-- /portfolio:intro -->

# 为什么做

给五年级的小侄女讲欧拉定理时，靠纸笔完全讲不明白。她盯着图说："反正这条线就是在那里"——实际上这条线会随着三角形动，但纸上画不出来。

## 实现

- Canvas 画图元
- 鼠标/触屏拖拽
- 自动重算中点/外心/重心坐标
- 切换显示/隐藏不同元素，让注意力集中

小朋友反馈：**"现在知道它为什么叫定理了。"**
