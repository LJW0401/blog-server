---
title: 从零搭建一个博客服务端
slug: blog-server-from-scratch
tags: [工程化, Go, 作品集]
category: 工程笔记
created: 2026-04-15
updated: 2026-04-18
status: published
featured: true
excerpt: 作品集型站点与普通博客的取舍，以及为什么选 Go + SSR 而不是 Next.js。
---

# 开篇：为什么自己造轮子

市面上的博客托管服务能解决"写 + 发"这件事，但没法给你一个 **审美统一的个人站点**——主页、文章、项目展示都由不同工具拼起来，接缝处永远有违和感。

这个项目的根本问题不是"做博客"，而是：**把一个开发者能拿出手的所有东西装进同一个壳子**。

## 取舍清单

| 选项 | 放弃 | 理由 |
|-|-|-|
| Next.js + Vercel | ❌ | 双份工作（API + SSR）；不想要第三方托管锁 |
| Hexo / Hugo 静态站 | ❌ | 管理后台要另外搭，整合项目展示很别扭 |
| Go SSR + 单二进制 | ✅ | 一个二进制 + 一个目录 = 整个站点 |

## 关键决定

1. **内容源**：Markdown 文件 + YAML frontmatter，而不是数据库——保证内容可迁移
2. **鉴权**：服务端渲染的登录表单 + HttpOnly Cookie，而不是前端校验
3. **项目数据**：本地 MD 长文 + GitHub API 合并——把"我为什么这么做"的叙事和客观指标分开
4. **部署形态**：Caddy 反代 + systemd 守护；应用内部跑 30 分钟 GitHub 同步、每日冷备份

## 技术栈

- Go 1.22+，chi 路由，`html/template`
- goldmark + chroma（Markdown + 代码高亮）
- modernc.org/sqlite（纯 Go，无 CGO）
- 总共一个二进制 + 一个数据目录

没有 Node，没有 React，没有 npm。

## 一些代码瞬间

```go
// 原子写入：temp + rename，避免半写入
func AtomicWrite(path string, data []byte, perm os.FileMode) error {
    tmp, _ := os.CreateTemp(filepath.Dir(path), ".atomic-*")
    tmp.Write(data)
    tmp.Close()
    return os.Rename(tmp.Name(), path)
}
```

```go
// Session cookie 签名：HMAC-SHA256 + base64
payload := base64.RawURLEncoding.EncodeToString(body)
sig := hmac.New(sha256.New, secret)
sig.Write([]byte(payload))
value := payload + "." + base64.RawURLEncoding.EncodeToString(sig.Sum(nil))
```

## 回头看

从需求文档开始整套流程走了 7 个阶段、118 个工作项。最大的意外不是哪个功能难写，而是 **"先搭原型再写代码"这件事真的有价值**——P2 渲染管道接手原型时，我只需要把三份 HTML 拆进 `html/template`，视觉基本没变。

如果重来一次，我会更早把 Node 工具链加回来——CodeMirror 6 完整打包只能推迟到后续阶段，这是唯一明显的技术债。

---

这个站本身就是它要展示的作品。
