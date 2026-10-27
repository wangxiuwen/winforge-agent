# WinForge Desk 架构说明

`winforge desk` 是局域网多机键鼠共享托盘程序：**共享（键鼠）/ 传输（消息+文件）/
控制（互相控制）**，配对 = 双端确认同一 6 位配对码。本文面向贡献者，讲清模块
分层、关键流程与设计取舍。改代码前建议同时读 `ROUNDS.md`（56+ 轮迭代的踩坑实录）。

## 模块分层

```
cmd/winforge          CLI 入口（desk 子命令：浏览器控制台 / autostart）
cmd/winforgedesk      GUI 入口（Wails 窗口 + 托盘；仅此包加载 Wails）
internal/deskapp      GUI/CLI 共用的启动编排（配置加载自愈、日志轮转）
internal/desk         核心包（平台无关部分全部下沉在此）：
  proto.go            线路帧 [1B type][4B BE len][payload]，长度上限拒绝
  crypto.go           HKDF/配对码派生/AES-256-GCM 会话加密（双向独立计数器）
  pairing.go          配对握手（发起方/应答方两角色，X25519 + PSK 证明）
  session.go          单条 TCP 会话：握手、帧读写（sendMu 串行化）、keepalive
  hub.go              Hub 编排：会话表、拨号退避、仲裁、safeGo
  hub_msg.go          协议消息分发（chat/文件 offer/块/控制/状态/地址学习）
  hub_ctl.go          控制状态机（take/deny/release/链式/边缘推挤/热键/Esc）
  hub_ui.go           UI 命令面（配对流程/Snapshot/共享开关）
  discovery.go        UDP 广播发现（SO_REUSEADDR/SO_REUSEPORT 同机多实例）
  transfer 相关       收发两侧状态机（offer→accept→chunks→SHA 校验→done）
  chat.go             聊天持久化（每对端一个 JSON 文件，上限 500 条）
  ui.go               浏览器控制台 HTTP 服务（RPC/事件流/上传，token 鉴权）
  web/                前端（微信风三栏；Wails 与浏览器双宿主适配）
  input*.go           平台层接口 + Windows 钩子/注入实现（低级钩子/SendInput）
  tray_systray.go     系统托盘（Windows/Linux；darwin 走 stub）
  edgebar_windows.go  控制状态边缘色条（渐显渐隐动画）
```

## 信任边界与安全模型

- 配对码由双端临时 X25519 ECDH 派生，两边不一致即存在中间人；PSK 后续
  会话证明身份，握手失败即断。
- 会话流量 AES-256-GCM，双向独立计数器 nonce（首帧计数器 0 不判重放）。
- 浏览器控制台只绑 127.0.0.1 + 启动期随机 token（日志里取）。
- 接收文件强制文件名清洗、只写下载目录、SHA-256 校验、大小上限。

## 关键流程

### 配对
发起方 `PairStart(addr)` 连对端控制端口 → 双方各显示 6 位码 → 接受方点
"确认配对码一致"（pairRespond，**事件必须带 reqID**）→ 发起方点"确认配对"
（pairOutConfirm）→ 双方落盘 Peer 并建会话。注意两侧 UI 角色不同：
发起方的确认按钮是"确认配对"，接受方是"确认配对码一致"。

### 会话与重连
配对后由 connectMissing 周期补连（指数退避 3s→30s），双端同时拨号由
`initiatorID` 字典序仲裁保留一条。**重连收敛窗口（数秒到数十秒）内发送的
消息不补投**——SendChat 有 Alive 检查，调用方（UI）负责重试。

### 控制与边缘推挤
对端设为屏幕左/右方位后，鼠标推过对应边缘即发起控制（onEdge）；被控端
朝控制器方位的边推 = 拿回，朝另一侧 = 链式切换到第三个对端。移动事件
8ms 合帧。热键 Ctrl+Alt+D 在任意状态切换。Esc 让被控端立即拿回。

### 事件流（浏览器控制台）
`/api/events?after=seq` 长轮询 + 服务端 512 条回放环（`desk:state` 高频
快照不入环），客户端断线重连按 seq 补缺口；聊天按 `peer|ts` 去重。
Wails 窗口走 runtime.EventsEmit 同一套事件名。

## 已知边界

- 重连收敛窗口内消息会丢（见上），后续可选 kick 消息方案加速收敛。
- 键鼠钩子/注入仅 Windows 实机可验（合成注入会真劫持鼠标，不做自动化）。
- darwin 托盘未支持（tray_stub）。
- Linux 变体本地复现：见 ROUNDS.md 每轮规则 5b（WSL 方法）。

## 自测

见 README"开发"节的脚本清单表；CI 跑 `go test -race ./...` + vet +
govulncheck + 跨平台 release。
