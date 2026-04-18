---
title: Go 写 Web 服务的几个小模式
slug: go-http-patterns
tags: [Go, 工程化, 后端]
category: 工程笔记
created: 2026-04-11
updated: 2026-04-11
status: published
featured: false
excerpt: 从 blog-server 项目里抽几个反复出现的 handler-level 模式——中间件链、context 传值、错误容忍的监测。
---

过去几周写 blog-server 时反复用到几个 Go 的 HTTP 小模式，值得记下来。

## 1. 中间件链的组合

```go
func Chain(mws ...func(http.Handler) http.Handler) func(http.Handler) http.Handler {
    return func(final http.Handler) http.Handler {
        h := final
        for i := len(mws) - 1; i >= 0; i-- {
            h = mws[i](h)
        }
        return h
    }
}

chain := Chain(
    PanicRecover(logger),
    RequestID,
    AccessLog(logger),
    SecurityHeaders,
    Gzip,
)
http.Handle("/", chain(mux))
```

比 chi/mux 的 `Use()` 链更显式，能看出来谁是最外层、谁是最内层。

## 2. Context 传请求级标志

把"是否 banner"、"request_id" 这类请求级状态放 context，handler 和 template 都能取到：

```go
ctx := context.WithValue(r.Context(), ctxKeyBanner, shouldBanner())
next.ServeHTTP(w, r.WithContext(ctx))

// 下游取：
func BannerFrom(ctx context.Context) bool {
    v, _ := ctx.Value(ctxKeyBanner).(bool)
    return v
}
```

**不要在 context 里塞业务数据**——只放请求级的元信息。

## 3. 监测 API 的"吞错不返 error"约定

阅读计数、访问日志这类"记了更好、记不上也不影响页面"的调用，签名应该长这样：

```go
// 不返回 error，内部吞掉并 log
func (s *Store) RecordRead(ctx context.Context, slug, ip, ua string) {
    if err := s.doRecord(ctx, slug, ip, ua); err != nil {
        s.logger.Error("stats.record", slog.String("err", err.Error()))
    }
}
```

对比之下 **业务 API 永远要返回 error**：保存文档失败、密码修改失败，这些必须让调用方 handle。

区分的关键问题：**这次失败如果无声丢掉，用户能察觉吗？** 察觉不到就用"吞错"；察觉得到就必须返。

## 4. 原子文件写

并发安全的配置/内容写入：

```go
tmp, _ := os.CreateTemp(dir, ".atomic-*")
tmp.Write(data)
tmp.Sync()
tmp.Close()
os.Chmod(tmp.Name(), perm)
os.Rename(tmp.Name(), path)
```

`rename(2)` 是原子的——读者要么看到旧文件要么看到新文件，不会看到半写入。配合一个 per-path 的 `sync.Mutex` 串行化同进程内的并发写。

---

这些都是 Go 标准库就能搞定的东西，不需要任何框架。有时候"不用框架"本身就是一种审美表达。
