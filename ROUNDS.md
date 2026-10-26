# WinForge Desk 迭代日志（R 轮）

目标：`winforge desk` = 局域网多机键鼠共享托盘程序，三大能力 **共享（键鼠）/ 传输（消息+文件）/ 控制（互相控制）**，
配对 = 双端确认同一 6 位配对码。分支 `desk`，全部完成后合回 main。

## 功能清单

- [x] R1 核心全链路：UDP 发现 / 双确认码配对（X25519+ECDH 派生 PSK）/ AES-GCM 会话 /
      消息 / 文件传输（SHA-256 校验）/ 控制状态机（take/deny/release）/ 托盘 /
      Wails 桌面窗口 / 浏览器控制台 / go test -race 全绿 / 双实例真进程冒烟通过
- [x] R3 Wails 桌面窗口实机启动验证（进程存活/端口监听/热键注册/无报错；前端绑定交互待用户实测）
- [ ] R2+ 托盘实机验证：图标显示、菜单动作（需真人看托盘点菜单，待用户实测；R3 已确认托盘随窗口模式启动无报错）
- [ ] R2+ 真机键鼠共享实测：钩子捕获→对端注入（同机双实例无法测钩子，需真机或驱动级模拟）
- [ ] R2+ 热键 RegisterHotKey 实测（Ctrl+Alt+D 切换）
- [x] R2 断线重连（e2e：强制断开 TCP → 自动重连 → 消息恢复）；杀进程级+控制态不卡键待真机
- [x] R2 同机双实例发现：UDP SO_REUSEADDR（参考 internal/discovery/reuse_windows.go）
- [x] R3 `winforge desk autostart on|off|status`（HKCU Run，实测全周期）+ --start-hidden
- [x] R4 独立 GUI 入口 cmd/winforgedesk（-H=windowsgui，无控制台黑窗；日志落盘）
- [x] R5 传输增强：取消（收发双侧+UI 按钮+对端联动）、接收进度 200ms 限频、32MB 冒烟实测（1GB 靠同一机制，未单独实测）
- [ ] R2+ UI 打磨：聊天记录持久化（会话级）、连接状态动效（拖拽发送与多对端消息隔离已完成→R6）
- [ ] R2+ 控制体验：控制时对端屏幕边缘提示条、光标进入动画（Esc 快捷交还已完成→见 R5）
- [ ] R2+ 多对端拓扑（>2 台）：链式切换（方位互斥与控制互斥已由 TestMultiPeerTopology 覆盖→R6）
- [ ] R2+ 性能：输入延迟测量（目标 <30ms 局域网）、移动事件合帧
- [ ] R2+ 打包：Makefile release 增加 winforge-desk-windows 变体、SHA256SUMS
- [ ] R2+ 文档：README 截图位、SECURITY.md 补 desk 章节 threat model

## 每轮规则（给迭代轮）

1. 开工先 `git log --oneline -3` + `git status`：确认在 `desk` 分支、无别人未提交改动；
   发现有未提交的别人改动 → 只提交自己改的文件，绝不 `git add -A` 吸收。
2. 每轮只做清单里 1-2 项，做完必须：`go build ./...` + `go test ./internal/desk/ -count=1 -timeout 180s`
   （涉改共享代码再跑 `go test ./...`）+ 冒烟 `py -3 smoke_desk.py`（涉及传输/协议时）。
3. 本机 vet 对 input_windows.go 的 unsafe.Pointer 告警是 Win32 钩子惯用写法，CI(Linux) 不编译该文件，忽略。
4. `internal/agent` 存量测试在 Windows 本地失败（与 desk 无关，CI Linux 通过为准），不要顺手去修范围外代码。
5. GOPROXY 必须设 `https://mirrors.aliyun.com/goproxy/,direct`（本机 goproxy.cn DNS 不通，7897 代理不常开）。
6. 一轮一个 commit：`desk(R<N>): <做了什么>`，并把本轮记录追加到本文件"轮次记录"。
7. 完成一轮 push 一次：`git push origin desk`。不 force push、不 rebase、不动 main。
8. 用户插话优先于清单顺序；构建失败超过 3 次尝试就记录现状收尾，不硬提交坏状态。

## 轮次记录

### R6（2026-10-26）
- ★结构性修复：dialSession 末尾同步调用 s.serve()（永不返回的读循环），把
  runSessions 补连循环永久卡死——多对端场景下每个 Hub 只能有第一条主动拨号会话，
  后续对端永远拨不出去。此前单对场景全部"恰好"靠它活着。修复：拨号侧会话
  `go s.serve()`（accept 侧本就运行在自己的 accept 协程里，不受影响）。
- 多对端拓扑 e2e TestMultiPeerTopology：三 Hub 全互联（A↔B、A↔C），方位设置、
  控制互斥（A 控制 B 时 C 的 take 被 deny）、交还后换向控制，全断言通过。
  pairTwo 泛化为按对端 ID 等待（支持同一 Hub 已有其他配对）。
- 拖拽文件发送：浏览器形态 HTML5 拖放（全屏提示层 + dataTransfer.files 上传）；
  Wails 形态升级 wails v2.5.0→v2.17.0 启用 DragAndDrop+OnFileDrop（v2.5 无此 API），
  Go 侧收路径事件交前端按当前选中对端 SendFile。
- 多对端消息隔离：chatLog 改按 peerID 分桶，切换下拉只看当前对端（每桶 500 条上限）。
- 补交 R5 漏提交的 internal/desk/input_windows.go（Esc 的平台层部分——R5 提交后
  HEAD 上 Esc 实际是死功能，本轮修复；教训：提交前 git status 全量核对清单内文件）。
- -race 全量绿 + 冒烟（32MB）通过。

### R5（2026-10-26）
- 传输取消全链路：Hub.CancelTransfer（发送侧 CAS 抢取消权后同步清理——offer 阶段
  发送循环还没启动，必须就地处理；接收侧 abort 半成品+通知对端）；
  对端 file_abort 事件补发 UI（此前静默清理）；UI 传输条在 sending/receiving
  状态显示"取消"按钮；HTTP rpc 与 Wails 绑定同步接入。
- 接收进度事件 200ms 限频（大文件不再刷爆 UI）。
- Esc 快捷交还：Windows 键盘钩子在非捕获模式下检测物理 Esc（注入键跳过）→
  Platform.Escape 通道 → 被控端 ReleaseControl + toast。控制器端按 Esc 无副作用。
- 冒烟升级到 32MB 校验一致（进度限频下的完整传输）。
- 新增 e2e：TestCancelTransfer（offer 阶段取消，双方 cancelled 且无半成品——
  回环上"传输中途"取消有时序竞争，取确定性阶段）、TestEscapeReleasesControl。
- 坑：回环 TCP 上 8MB 瞬间传完，任何"恰好在传输中"的断言都不可靠；
  取消权用 CompareAndSwap 防止 CancelTransfer 与发送循环双重清理/双发事件。
- -race 全量绿 + 冒烟全通过。

### R4（2026-10-26）
- 桌面运行体抽到 internal/deskapp（不 import Wails）：Options{WindowUI/WindowShow}
  由 GUI 入口注入；`winforge desk` 回归纯 CLI（浏览器控制台），stdout 不再被污染
  ——`winforge version` / `desk autostart status` 实测零 DEB 行。
- 新增独立入口 cmd/winforgedesk：Wails 窗口 + 托盘 + autostart 子命令；日志固定写
  %APPDATA%\winforge\desk.log（O_APPEND），排障"双击没反应"有据可查。
- Makefile release 增加 winforgedesk-windows-amd64/arm64.exe，-ldflags -H=windowsgui
  （本机构建通过；真机双击无黑窗待用户实测）。
- 实机验证：GUI 入口（控制台版）启动 9s——进程存活、日志落盘、控制端口 41831 监听。
  -race 全绿，冒烟全通过（走新 deskapp 路径）。
- autostart 命令行生成升级：同目录存在 winforgedesk.exe 时自启指向它，
  否则回退 `<CLI> desk --start-hidden`。
- 结构坑：ListenPacket/registry 这些平台差异都要成对写 build-tag 文件；
  cmd 下两个 main 不能共享代码，公共逻辑必须下沉 internal 包（deskapp 即为此而生）。

### R3（2026-10-26）
- 新增 `winforge desk autostart on|off|status`（HKCU Run 键，值名 WinForgeDesk，
  命令行带 --start-hidden 登录后只留托盘不弹窗）+ `--start-hidden` 旗标接到
  Wails StartHidden；实测 on→status(开,读回命令行)→off→status(关) 全周期通过。
- Wails 桌面窗口实机验证：真进程跑窗口模式 9 秒——进程存活、控制端口监听、
  RegisterHotKey(Ctrl+Alt+D) 成功（无失败日志）、内嵌 go webview2loader 正常、
  无崩溃无错误输出。托盘图标/菜单点击、窗口内绑定调用需真人观察，待用户实测。
- ★新发现：Wails 的 webview2loader 在包 init 阶段就往 **stdout** 打
  "DEB | Using go webview2loader"，污染所有 winforge 子命令输出（含 version/init/serve，
  会破坏脚本解析）。R4 首项：新建独立 GUI 入口 cmd/winforgedesk（windowsgui 变体
  也顺势解决），主 CLI cmd/winforge 去掉 wails import。
- 坑：Git Bash 的 heredoc 会吃 JSON 里的双反斜杠（写临时配置要用 python）；
  MSYS 会把 reg query 的 /v 转成路径，验证注册表要走程序自身 status 或 powershell。

### R2（2026-10-26）
- 发现服务改用 net.ListenConfig{Control: reuseControl}：Windows 只设 SO_REUSEADDR，
  unix 加 SO_REUSEPORT（照搬 internal/discovery 的成熟做法）→ 同机双实例可共用
  41832/UDP 互相广播发现。
- 冒烟升级：两个实例都用 41832，新增第 0 步"UDP 广播发现互通"断言，全流程通过
  （发现→配对→会话→消息→3MB 文件→控制状态机）。
- 新增 TestHubReconnectAfterDrop：强制断 TCP → 3s 退避重连 → 重连后消息可达。
- 注意点：ListenPacket 返回 net.PacketConn，要断言成 *net.UDPConn 才有 ReadFromUDP。
- 追加修复（同轮二批）：重连切换窗口期调用方可能拿到"已关未清理"的旧会话
  （conn 已 Close 但 sessionClosed 还没跑完）→ 新增 Session.Alive()，所有会话
  查找点（SendChat/SendFile/TakeControl/connectMissing）先验存活；重连测试改为
  容忍一次瞬态失败（20×250ms 重试），-race 单跑与 -count=2 全绿。

### R1（2026-10-26）
- 新增 internal/desk（~3200 行）：proto/crypto/pairing/session/hub(_msg/_ctl/_ui)/discovery/
  transfer/input(_windows/_other)/config/tray/web UI + cmd/winforge `desk` 子命令。
- 安全模型：配对码绑定 ECDH、PSK 会话证明、AES-256-GCM 双向计数器、UI 令牌鉴权、
  下载目录路径约束（穿越/保留名拒绝）、大小上限。
- 测试：crypto(HKDF RFC 向量/MITM 配对码/重放)/transfer(穿越)/discovery/ui(鉴权)/
  config + 双 Hub e2e（配对码一致、消息、3MB 文件、take/deny/release、删对端断连），
  `go test -race` 全绿。
- 真进程双实例冒烟 `smoke_desk.py`：配对码一致→配对→会话→消息→3MB 文件校验→
  关共享拒绝控制→take/release 状态机，全部通过。
- 修掉的坑：会话帧没套外层 [type][len] 头（nonce 被当帧头）；首帧计数器 0 被判重放；
  ToggleShare 持锁调加锁函数死锁；cleanup 重复 close 捕获通道 panic 死锁；
  发起方用 confirm 帧 ID（空）存对端；darwin 托盘 cgo 破坏交叉编译（拆 tray_systray/tray_stub）。
- 已知边界：键鼠钩子/注入仅 Windows 实机可验（本轮未测）；同机 UDP 发现端口冲突未做
  SO_REUSEADDR；`internal/agent` 存量 Windows 测试失败与本模块无关。
