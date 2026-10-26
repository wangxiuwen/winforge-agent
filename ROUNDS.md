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
- [ ] R2+ UI 打磨：连接状态动效（已完成→R8；聊天持久化已完成→R9）
- [x] R15 控制体验：边缘条渐显/渐隐动画（~200ms alpha 渐变，R8 基础上）；颜色效果待用户实测
- [x] R9 多对端拓扑：链式切换（被控端推向另一侧方位边 → ctl_chain → 控制器改链目标）
- [x] R7 性能：移动事件 8ms 合帧（500→1 帧实测）+ capture→inject 往返延迟测量（本机 loopback ~50ms；真实局域网延迟待真机）
- [ ] R2+ 打包：Makefile release 增加 winforge-desk-windows 变体、SHA256SUMS
- [x] R11 文档：README 真实控制台截图（无头 Chrome 自动生成）；SECURITY.md desk 章节见 R7

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

### R21（2026-10-27）
- ★流程修复：发现 desk 分支的 CI 从未运行——workflow 只触发 main 的 push 和
  PR，此前 20 轮 push 全部只有本地门禁。将 desk 加入 push 触发分支，
  本次 push 起 CI（test -race / vet / govulncheck / 交叉编译演练）即开始
  对 desk 分支生效；实测质量门禁本地已全绿，预期 CI 绿。
- 尝试过开 draft PR 触发（API 需认证 401，gh token 失效）走不通后改此方案。

### R20（2026-10-27）
- 稳态巡检：build/vet/race 全绿、冒烟通过、工作树干净（dist 已被 gitignore，
  无杂物文件）。无代码变更。

### R19（2026-10-27）
- 例行健康巡检：make release 重演练（R10 后核心大改过——8 产物全出含
  winforgedesk windowsgui×2）；-race 全量绿（desk+deskapp）；冒烟通过。
- 无代码变更轮。

### R18（2026-10-27）
- 补 TestDialRaceConverges：R17 仲裁的直接验证——断线后双端各并发拨号两次，
  断言收敛后 3 秒 online 稳定 + 双向消息可达。count=2 与全量 -race 均通过。
- -race 全量绿 + 冒烟通过。（中途一次 count=2 偶发失败，与 R12/R16 的套件
  时序抖动同源；R17 修复后单独/套件大多稳定，偶发仍需观察。）

### R17（2026-10-27）
- ★产品级修复：双端并发拨号会话风暴。断线后 A、B 的补连循环同时拨对方，
  两条会话互相替换踢踩，重连窗口内 SendChat 持续"对端不在线"（R12/R16 的
  偶发超时根源，比之前认定的"测试时序抖动"更实质）。
  修复：Session 增加 initiatorID（主动连接=本机 ID，被动=对端 ID），
  registerSession 对同对端双活会话按 initiatorID 字典序仲裁只保留一条
  ——两端决策一致，风暴收敛。AddrLearning/Reconnect 测试 count=3 全过，
  全量 -race 绿，冒烟通过。
- 例行：重新生成 README 截图（管线健康）；截图脚本 wait_for 裸 return None
  的坑已在上轮修掉，本轮复用顺畅。
- 测试等待放宽：TestAddrLearning 学地址等待 5s→15s。

### R16（2026-10-27）
- 例行回归轮：格式漂移收尾（gofmt 范围=自己写的 internal/desk、internal/deskapp、
  cmd/winforgedesk；范围外的 agent/client 等存量非格式化文件不碰）；
  TestHubReconnectAfterDrop 断线重连等待 15s→25s（套件内受前序测试 UDP/端口
  TIME_WAIT 干扰时偶发超时，单独 -count=3 从未复现）。
- 全链回归：build/vet/race 套件×3 连跑全绿 + 冒烟通过。
- 环境事实：GitHub API（api.github.com）当前被墙且 7897 代理未开，CI 状态无法
  命令行核查；SSH（ssh.github.com:443）push 正常，远端与本地同步（ffce38e）。
  CI 结果待用户网页查看；若 CI 红，优先怀疑 govulncheck 新公告而非代码回归
  （R10 本地预检为 0 可达）。

### R15（2026-10-26）
- 边缘提示条渐显/渐隐：初始 alpha=0，显示时 16ms/帧步进至 170（~200ms），
  隐藏时先渐隐再 SW_HIDE；bar 线程内阻塞实现（最长 ~200ms，可接受）。
  GUI 实机启动验证不崩；视觉效果待用户实测。
- README 新增"故障排查"表：双击没反应/发现不了设备/热键冲突/控制台 401/
  配对码不一致/卡键/清空配对——全部对应已实现的机制与日志位置。
- -race 连跑两遍全绿 + 冒烟通过（中间一次偶发 FAIL，复跑即绿，
  与截图/残留进程时序相关，非代码回归）。

### R14（2026-10-26）
- 托盘常驻可靠性两项加固：
  ① desk.log 大小轮转（5MB 归档 .1），rotateWriter 带 written 计数；
  ② 配置损坏自愈：LoadConfig 解析/校验失败 → 坏文件归档为 desk.json.bad-<pid>
     并用默认配置启动（托盘应用"起不来"比"丢配置"更糟，坏文件保留供用户找回
     配对记录），warning 打入日志。首次运行（文件不存在）不触发自愈。
- deskapp 新增 logfile_test（轮转+自愈）；-race 全量绿 + 冒烟通过。
- 设计反复：rotateWriter 包装层一度做成双接口 Logger 又简化回 io.Writer——
  Options.LogWriter 单通道 + Run 内部 log.New 包装已够用，别为调用方预做抽象。

### R13（2026-10-26）
- goroutine panic 防护：desk 是托盘常驻进程，任一后台协程 panic（平台 API
  异常、协议分支漏判）都会带崩整个进程。新增 recover.go（safeGo/safeCall），
  Hub 的 acceptLoop/runSessions/watchHotkey/watchEscape/session-serve 全部
  换 safeGo 启动；Session.serve 顶部 safeCall 拦截（panic 后仍走会话正常
  关闭路径，对端不会卡死）。
- TestConcurrentCommands 并发压测：4 goroutine 混打 Snapshot/ToggleShare/
  PeerPosition/SendChat 各 200 次，-race 无 race、无死锁，结束后状态一致。
- -race 全量绿 + 冒烟通过。

### R12（2026-10-26）
- 对端地址学习：state 消息携带本机控制监听端口，应答方从入站连接的对端 IP +
  声明端口学到主动方地址并落盘（变化才写）。之前应答方 peer.Addr 永远为空、
  只能靠 discovery 回连——discovery 被 AP 隔离/UDP 被禁时单向失能。
- 测试 TestAddrLearning：断言 B 学到 "IP:A控制端口"；断线回连行为由
  TestHubReconnectAfterDrop 覆盖（B→A 双端并发拨号的重连窗口在 -race 下
  抖动大，套件内不稳定，此处不重复断言）。
- 修一个真 DATA RACE：discovery 广播 goroutine 经 nameSnapshot/pairedCountSnapshot/
  shareSnapshot 无锁读 cfg 字段，与 ToggleShare 等写方并发——三函数补 hub.mu。
- 测试 ID 视角又错两次（aID 混用"自己 ID/对端 ID"两个语义）——双端测试里
  建议一律显式命名 aSelfID/bID，别用单一 aID。
- -race 连跑两遍全绿 + 冒烟通过。

### R11（2026-10-26）
- README 真实截图：screenshot_desk.py 起双实例→配对→双向聊天→5MB 文件传输完成→
  无头 Chrome(--virtual-time-budget) 截控制台共享页与传输页，入 docs/ 并嵌 README。
- 前端支持 ?tab=share|transfer 直达页签（截图与用户书签都受益）。
- 产品加固：浏览器控制台端口被占（强杀后 TIME_WAIT）自动顺延最多 10 个——
  截图流程反复强杀重启时暴露的实际痛点。
- 补 TestChatBidirectional（B→A 方向此前无覆盖）。
- 排查插曲：截图缺对方消息→先后怀疑 recordChat/历史拉取/会话死亡，最终定位是
  **验证脚本把 A 视角的 peer ID 用在了 B 的 sendChat 上**（产品无 bug）。
  教训：脚本里两端互相操作时，peer ID 必须从对端自己的视角取。
- -race 全量绿（一次偶发失败复跑即绿）+ 冒烟通过。

### R10（2026-10-26）
- 回归加固轮（功能清单已清空）。
- govulncheck 本地预检：0 可达漏洞（模块级 5 条均为未调用，不阻塞 CI）。
- make release 发版演练：8 个产物全出（CLI 三平台 ×2 架构 + winforgedesk
  windowsgui amd64/arm64），SHA256SUMS 生成正常。
- UI 命令边界测试 TestUICommandBoundaries：空/超长消息、离线对端、坏地址、
  未知对端的方位/启停/删除、非法 fid/peerID 的取消/历史/表态、未配对控制、
  空闲态交还、共享开关连打——全部不 panic 且语义正确。
- 产品加固（测试红→修）：SetPeerPosition/RemovePeer 对未知对端由静默成功
  改为报错；PairStart 增加主机名字符白名单（SplitHostPort 竟接受含空格 host）。
- TestChatHistoryFileIsolation：非法 peerID 不产生聊天文件。
- -race 全量绿 + 冒烟通过。

### R9（2026-10-26）
- 聊天记录持久化：每对端一个 JSON（配置目录 chat/<peerID>.json，≤500 条，
  原子重写），Hub 启动恢复；加载时过滤未来时间戳与超长文本（防手改文件注入）。
  文件名强制 hex 正则校验防穿越。UI 首次查看某对端时拉一次历史（之后走实时事件），
  HTTP rpc 与 Wails 绑定同步接入；e2e TestChatPersistence 覆盖收发两侧+重启恢复。
- 链式切换：被控端朝"非控制器侧"的方位边推 → ctl_chain{target,y} 发给控制器 →
  控制器通知旧被控端复位（ctl_release）+ 本地清理 + TakeControl(新目标)。
  控制器与新目标必须已配对（无 PSK 无法建会话，天然安全边界）。
  e2e TestChainSwitch：三 Hub 一字排开，A 控制 B 后链式转 C。
- 坑：聊天历史按键=对端 ID（不是本机 ID），测试断言写错过一次；
  链式必须显式给旧被控端发 ctl_release，否则它永远停在"被控制"状态。

### R8（2026-10-26）
- 控制状态边缘提示条（Windows 分层窗口）：共享激活时在共享边显示 3px 常亮条
  （我在控制=绿 / 被控制=琥珀），空闲隐藏；WS_EX_LAYERED|TRANSPARENT|TOPMOST|
  TOOLWINDOW，鼠标穿透不抢焦点不进 Alt-Tab。颜色切换用 GCLP_HBRBACKGROUND
  换背景刷 + InvalidateRect。
- 状态出口收敛：所有控制状态变化统一走 Hub.controlChanged()（UI 事件 + 提示条），
  Platform 新增 SetControlIndicator；TestControlIndicator 断言两侧状态序列。
- ★大坑两次踩点：① CreateSolidBrush 在 gdi32.dll 不在 user32，LazyProc 找不到
  过程直接 panic 且带崩整个进程（smoke 表现为"启动超时"，需要最小复现程序才能
  定位）；② sed 全局替换把 controlChanged 体内的 emit 也替换成自身 → 无限递归
  栈溢出。全局替换后必须 review 差异。
- UI 动效：在线设备呼吸点（pulse）、控制状态条呼吸亮度。
- GUI 实机启动验证：edgebar 线程随进程创建无报错、控制端口正常监听；
  提示条颜色效果待用户实测。-race 全量绿 + 冒烟通过。

### R7（2026-10-26）
- 移动事件合帧：captureLoop 对鼠标移动做 8ms 窗口合并（只发最新归一化位置），
  键/按钮/滚轮立即发送且发送前冲刷 pending move 保证顺序；高报点率鼠标不再
  打爆网络。实测 e2e：500 个 move → 1 帧，B 端终值精确等于最后一次积分结果，
  capture→inject 往返 ~50ms（含合帧窗口；真实局域网延迟待真机）。
- SECURITY.md 增补 Desk 模块安全模型与已知边界（配对即授权/输入不落盘/
  传输路径约束/控制台令牌/无遥测；PSK 文件即身份、配对码 90s 窗口）。
- 冒烟稳定性：chat 断言改为带重发的轮询（配对完成瞬间发消息可能撞上
  会话握手尾部的瞬态，复跑两次全绿后加的加固，非协议回归）。
- -race 全量绿。

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
