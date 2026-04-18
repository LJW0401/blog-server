---
slug: remote-term
repo: LJW0401/remote_term
display_name: remote-term
display_desc: 一个基于 WebSocket 的远程终端前端，让浏览器直接以体面的交互接管远程 Shell。
category: 开发者工具
stack: [TypeScript, WebSocket, xterm.js]
status: developing
featured: false
created: 2026-02-05
updated: 2026-04-12
---

# remote-term

> Repo: [LJW0401/remote_term](https://github.com/LJW0401/remote_term)

**基于 WebSocket 的浏览器远程终端**——给"我要在手机上临时进一下服务器看日志"这种场景提供体面的交互层。

## 核心诉求

1. **不走 SSH 原始协议**——太重、移动端难处理
2. **不走第三方云端中转**——隐私和延迟都不合适
3. **自托管、可控、最小依赖**

方案：服务端一个 Go 进程，一端连 PTY，另一端开 WebSocket；前端 xterm.js 直接对接。

## 为什么 xterm.js

终端里的 ANSI、光标控制、Vim 的行为……自己实现渲染这些本身就是一年的工作量。xterm.js 是 VSCode 内置终端的底层引擎，兼容性经过真实场景打磨。

## 现状

- 基础会话功能能用（键盘输入、ANSI 颜色、光标移动）
- 正在做的：多标签、链接检测、复制选中自动处理、上传/下载
- 计划但未开始：双向剪贴板、窗口 resize 自动通知后端

## 安全

- 必须走 HTTPS + 认证（目前只接入了 Basic Auth，后续计划 OIDC 或 SSH key 签名认证）
- 连接数限制、空闲自动断开

## 对比 Cloud Shell / tmate

都是"不在本机装 SSH 客户端也能远程干活"的思路，但 remote-term 是 **全链路自托管** 的版本——不走任何第三方。
