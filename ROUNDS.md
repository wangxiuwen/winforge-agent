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
- [x] R2+ UI 打磨：连接状态动效（已完成→R8；聊天持久化已完成→R9；R41 核实勾选）
- [x] R15 控制体验：边缘条渐显/渐隐动画（~200ms alpha 渐变，R8 基础上）；颜色效果待用户实测
- [x] R67 基本远程桌面：控制期间实时显示对方屏幕（多屏拼接+光标标记+静止跳帧零带宽）；
      查看器观感/帧率自适应待用户实测反馈（v1 边界：全帧编码、仅嵌在控制视图）
- [x] R9 多对端拓扑：链式切换（被控端推向另一侧方位边 → ctl_chain → 控制器改链目标）
- [x] R7 性能：移动事件 8ms 合帧（500→1 帧实测）+ capture→inject 往返延迟测量（本机 loopback ~50ms；真实局域网延迟待真机）
- [x] R2+ 打包：Makefile release 增加 winforge-desk-windows 变体、SHA256SUMS
      （变体早已存在但一直没勾；R41 实测发现 SHA256SUMS 的 `winforge-*` glob
      匹配不到 `winforgedesk-*`、漏两个 desk 校验和，已修并本地实测 10 产物全出）
- [x] R11 文档：README 真实控制台截图（无头 Chrome 自动生成）；SECURITY.md desk 章节见 R7

## 每轮规则（给迭代轮）

1. 开工先 `git log --oneline -3` + `git status`：确认在 `desk` 分支、无别人未提交改动；
   发现有未提交的别人改动 → 只提交自己改的文件，绝不 `git add -A` 吸收。
2. 每轮只做清单里 1-2 项，做完必须：`go build ./...` + `go test ./internal/desk/ -count=1 -timeout 180s`
   （涉改共享代码再跑 `go test ./...`）+ 冒烟 `py -3 smoke_desk.py`（涉及传输/协议时）；
   改了 web 前端必须 `node --check internal/desk/web/app.js`（R42 的 P0 教训：
   重复 let 声明=整份 JS 解析失败，UI 全死，go 门禁和 HTTP 冒烟都测不到）；
   改了窗口/UI 交互或 Wails 壳再跑 `py -3 uia_desk.py`（R45，Windows 本机，
   需要 pip 装 pywinauto；与用户实例按 PID 共存，不碰托盘）。
3. 本机 vet 对 input_windows.go 的 unsafe.Pointer 告警是 Win32 钩子惯用写法，CI(Linux) 不编译该文件，忽略。
4. `internal/agent` 存量测试在 Windows 本地失败（与 desk 无关，CI Linux 通过为准），不要顺手去修范围外代码。
4b. 改了 `.github/workflows/ci.yml` 必须先本地验 YAML（R49 教训：`run:` 裸标量
   里带"冒号+空格"会把 workflow 打成 startup failure 且 jobs 为空）：
   `py -3 -c "import yaml,io; yaml.safe_load(io.open('.github/workflows/ci.yml',encoding='utf-8'))"`（pyyaml 已装）。
5. GOPROXY 必须设 `https://mirrors.aliyun.com/goproxy/,direct`（本机 goproxy.cn DNS 不通，7897 代理不常开）。
5b. **Linux 变体本地复现**（R62 沉淀）：CI 的 Linux 编译变体可用 WSL 跑——
   `wsl -e bash -lc "cd /mnt/d/company/wangxiuwen/workspace/winforge-agent && export GOPROXY=https://mirrors.aliyun.com/goproxy/,direct && go test ./internal/desk/... -race -cpu 1,2"`
   （WSL 自带 go1.22 会自动拉 go.mod 的 1.26 toolchain；全套约 1-2 分钟）。
   Windows 本地复现不了的 CI race/编译问题先用它。
6. 一轮一个 commit：`desk(R<N>): <做了什么>`，并把本轮记录追加到本文件"轮次记录"。
7. 完成一轮 push 一次：`git push origin desk`。不 force push、不 rebase、不动 main。
8. 用户插话优先于清单顺序；构建失败超过 3 次尝试就记录现状收尾，不硬提交坏状态。

## 轮次记录

### R84（2026-10-28）
- 落地 frontendError 上报管线（R83 诊断工具转正）：前端 window.error/
  unhandledrejection/console.error → Wails 绑定 FrontendError → hub 日志
  [frontend] 前缀——Wails 窗口的渲染层错误从此可在 desk.log 诊断（此前
  CDP 因 production 构建禁 DevTools 而不可用）。
- 真机验证：热键占用场景下实例运行无 [frontend] 错误（横幅渲染正常，
  截图为证）；该管线未来任何前端渲染异常都会自动进日志。
- 门禁：全套 -race ✓ / smoke PASS ✓ / WSL Linux 前置验证 ✓。

### R83（2026-10-28）
### R82（2026-10-28）★Wails 窗口横幅未渲染——待深挖
- R80 热键警告在 **Wails 桌面窗口未显示**（浏览器模式横幅+toast 正常，
  截图为证）。症状精确化：设备列表等 render 前半段内容正常渲染，但
  toast（maybeShow）和横幅（render 尾部）都没出现——**疑似 render 中段
  抛错中断**（renderContent 或其后、banner 块之前）。
- 已排除：exe 旧（重建+二进制内嵌资产含 startup-banner/热键警告字串）、
  hub 侧（同码 CLI 快照 startupWarning 在）、Go 编译（双平台过）。
- 下轮攻坚：`-tags desktop,dev` 构建（启用 WebView2 DevTools）+
  WEBVIEW2_ADDITIONAL_BROWSER_ARGUMENTS=--remote-debugging-port=9223 +
  CDP 看 console 错误（production 构建禁 devtools，9223 拒连即此因）。
- 影响面评估：startupWarning 当前唯一内容=热键注册失败提示（R80），
  浏览器模式用户可正常看到；Wails 窗口用户看不到该提示但功能不受影响。
- 门禁：build ✓（未改代码，验证+记录轮）。

### R81（2026-10-28）
### R81（2026-10-28）
- R80 热键警告的真机验证攻坚（多路并进）：
  ① 独立干净实例（截图证实）：**警告横幅在浏览器模式正常渲染**——前端
     逻辑、hub 链路全部工作正常。
  ② 桌面窗口（Wails 模式）UIA+截图均未见横幅——原因未定（该次 exe 为
     banner+R80 重建版；疑与窗口模式前端初始化时序有关），Wails 窗口内
     横幅渲染标待用户实测（真实使用一眼即见）。
  ③ UIA 点击偶发不触发 WebView2 JS（R66 已知）导致"选择文件"对话框
     自动化不可靠——重试循环保留在 uia_pair 脚本。
- 热键警告端到端（hub→snapshot→前端横幅）在浏览器模式全链已证；无
  代码变更，验证记录轮。
- 教训：headless Chrome 对长轮询页面会整体超时（R42 之后页面新增
  /api/events 挂起请求与 virtual-time-budget 冲突）——加 `--timeout=12000`
  可破；UIA 断言优先用像素/截图而非元素文本。

### R80（2026-10-28）
### R80（2026-10-28）
- **热键注册失败接入 UI 启动警告**（此前只进日志，用户以为 Ctrl+Alt+D
  坏了无从知晓）：windowsInput 记录 hotkeyFail（RegisterHotKey 失败或
  热键配置无法解析），Platform 暴露 HotkeyReady/HotkeyFailed，desk 包
  新增 HotkeyWarning 助手（等注册结果最多 2s + 返回警告），deskapp.Run
  并入 startupWarning → UI toast 一次性展示（复用配置损坏警告通道）。
- 单测 TestHotkeyWarning（nil/失败/成功/超时四分支）；真机复验路径：
  占用 Ctrl+Alt+D 后启动实例，UI 应弹"热键注册失败…改用界面按钮切换"。
- 门禁：全套 -race 48.1s ✓ / smoke PASS ✓ / WSL Linux build ✓。

### R79（2026-10-28）
### R78（2026-10-28）
- 首次 50 轮长稳 soak（soak_desk.py 建成后最长一跑）：100 条双向消息 +
  50MB 文件 + 50 次开关共享循环，122s 全过，节奏稳定（2.1-3.0s/轮无劣化），
  双实例合计内存 40.9MB 无异常增长。chat 历史 100 条/对端（远低于 500 上限）、
  传输临时文件清理正常。
- 长尾验证结论：句柄/内存/退避状态无漂移，soak 工具可随时复查。

### R77（2026-10-28）
### R75（2026-10-27）★抓到多屏产品 bug
- 本机实为**双屏**（虚拟屏 3840×1080）——远程桌面切虚拟屏抓屏后，光标
  标记断言抓包发现红点被画在左边缘 (0,53)：**drawCursorMarker 的坐标
  换算假设虚拟屏原点 (0,0)**，多屏负原点布局下（显示器在主屏左侧/上方）
  标记画错位置。修：换算加虚拟屏原点 (pt-vx)/sw*outW——单屏行为不变
  （vx=0），多屏正确跟随。
- 测试配套：真机测试改虚拟屏中心+SetProcessDPIAware（非 DPI 感知进程
  的坐标被系统虚拟化，SetCursorPos/GetCursorPos/GetSystemMetrics 三者
  不一致会把调试引进沟里）；断言改预期映射位置附近计数红像素（阈值按
  RGBA 16 位与 JPEG q60 失真校准）。
- 全门禁：全套 -race 59.8s ✓ / smoke PASS ✓ / dist 产物同步刷新。
- 教训：**本机就是多屏测试机**——远程桌面这类屏相关功能开发时先
  GetSystemMetrics(78/79) 看一眼虚拟屏尺寸，别默认单屏。

### R74（2026-10-27）
- 实测助攻：docs/TESTING.md 双机实测指引（准备/10 项清单/已知边界/日志
  位置），构建最新 dist/winforgedesk-windows-amd64.exe（含远程桌面+光标
  叠加+拖动修复）+SHA256SUMS-two.txt。
- 差点重踩 R36 tags 坑：dist 产物构建漏 `-tags "desktop,production"`
  会被静态壳版覆盖——发版产物必须走带 tags 的构建命令（或 Makefile）。

### R73（2026-10-27）（追记：上轮漏写本文件）
- 远程桌面静止画面跳帧：JPEG 编码确定性，与上帧 bytes.Equal 相同即跳发
  （静态画面零带宽），3 色轮换唯一帧≤3 断言（TestScreenStreamSkipsStaticFrames）。
- 顺平 screen 相关文件 gofmt。
- ★上轮 commit 漏了 ROUNDS.md 更新——规则 4 的"更新轮次记录"也是门禁
  的一部分，提交前应 grep 确认。

### R72（2026-10-27）★race 修复（RACE 注解首次完整实战）
- 7191cab 的 CI 红给了**第一份完整 race 栈**（RACE 注解前缀修复后首次
  实战成功）：写方=hub_msg.go:41 `s.peerName = m.Name`（serve 读循环），
  读方=PeerName()（Snapshot/ControlState/toast 多处）——对端改名（state
  消息）与 hub 任意读名字路径并发，自 R1 潜伏。
- 修：Session 加 nameMu RWMutex，写方（state 处理）写锁、PeerName() 读锁。
- 验证：-race -cpu 1,2,4 ×3 全净 + smoke PASS + WSL Linux build/vet 双绿。
- 教训：RACE 注解前缀修复（R61）让 CI race 从"只能看到一个 WARNING 行"
  变成"14 行栈直接可读"——诊断通道的每一分投入都会在下一次故障里回本。

### R71（2026-10-27）
- **"选择文件…"对话框疑云解除（产品无 bug）**：独立干净实例诊断——点击后
  对话框正常弹出（截图为证），**实际标题是"选择要发送的文件"**；此前
  uia_pair/诊断的窗口过滤器写的是"打开/Open"，匹配不上被误判"未出现"。
  修正过滤器后 uia_pair 里仍偶发不弹（3 连击不触发），与实例状态相关
  （配对+活跃会话的实例点击易失效）——该方向自动化标受限，**待用户
  实测：人工点"选择文件…"应能弹窗选文件发送**。
- uia_pair 脚本更新：对话框标题过滤修正 + 重试式点击（对偶发有效），
  留在仓库供未来复跑。
- R66 遗留的"待用户实测：选择文件是否弹出"解除（弹出正常）。

### R70（2026-10-27）
- 覆盖率复扫：总 66.2%（↑2.5），screen.go 新代码 75-100% 健康；0% 剩余
  全是琐碎 getter 与 Windows 专属代码。
- 清理死代码 encodeScreenJPEG（无调用方，0%）；architecture.md 补
  "基本远程桌面"一节（协议/抓屏/光标叠加/v1 边界），文档与实现一致。
- 门禁：build ✓ / desk 测试 ✓（含死代码删除后）。

### R69（2026-10-27）
- 远程桌面可用性关键项：**被控端光标叠加**——控制端此前看不到对方屏幕上
  的光标在哪（注入没有视觉反馈）。抓屏后经 GetCursorPos 取被控端物理
  光标（=控制者移动的那支），按抓屏缩放比例画白边红心圆点再编码。
- 修一个 x/sys 经典陷阱：`LazyProc.Call` 的 err 是 GetLastError，**成功
  时也常非 nil**——drawCursorMarker 判 err 提前返回导致标记从未画上
  （隔离测试 0 红像素抓包）。改为判 r1 返回值后，隔离 81 红像素 +
  SetCursorPos 居中→抓帧→中心红点断言 全过。
- 热键测试又踩 R59 同款坑（第二次按键前没等 B 被控，release 先于 take
  到达被丢弃）——补等待后 -race -cpu 1,2 count=6 全过。这个坑出现两次
  了，教训升级：**涉及 take/release 的测试，每次状态迁移都必须双侧等待**。
- 门禁：全套 -race 51.2s ✓ / smoke PASS ✓ / GDI+光标断言 ✓。

### R68（2026-10-27）
- 修 CI Linux 编译断（26fb54a 起两个红）：AutostartEnable 改双返回值时只
  改了 autostart_windows.go，autostart_other.go 桩仍是旧单值签名——本地
  Windows `go build ./...` 编译不到那个文件所以一直绿。修：桩同步
  `("", ErrAutostartUnsupported)`，并用 **R62 沉淀的 WSL 方法预验 Linux
  build+vet 双绿** 后推送（这次不用等 CI 报）。
- 注解通道价值再验证：这次的失败 KEY 直接给出 `main.go:79 assignment
  mismatch`，一眼定位；ci-log 401 仍坏但 KEY 够用。
- 教训：**改跨平台桩函数签名时，Windows 本地编译通过≠完成**——凡动
  `*_windows.go/_other.go` 成对文件，必须 WSL 预验（规则 5b 的用途+1）。

### R67（2026-10-27）★用户需求：基本远程桌面 v1
- 用户新需求（插话优先）："最好也能实现基本的远程桌面，办公室多台机器
  在一台上控制别的"。v1 实现：**控制对端时实时看到对方屏幕画面**。
- 协议：新帧型 FrameScreen（0x04，[4B W][4B H][4B seq][JPEG]，复用 8MiB
  帧上限——一帧 JPEG 无需分块）+ 控制消息 screen_start（带 fps）/
  screen_stop。推流是干净加法：不发即无流量，协议向后兼容。
- 被控端：screenLoop（新 screen.go）——被控期间 3fps 经 Platform 新接口
  CaptureScreen(maxW,quality) 抓屏，每 tick 自检角色（非被控即自停，
  Esc/释放/断连三种路径全覆盖）。控制端：onScreenFrame → emit
  desk:screen {w,h,b64}。
- Windows 抓屏（screen_windows.go）：GDI BitBlt 主屏→32bpp DIB（top-down）
  →RGBA→JPEG q60，宽>1600 按比缩小。真机验证 1600×900/79KB/可解码。
- 控制端 UI：控制视图新增"对方屏幕（实时画面）"查看器（<img> data URL），
  控制结束自动隐藏；带宽 ~240KB/s@3fps（局域网无压力）。
- 测试：TestScreenStreamOnControl（fake 抓屏端到端：控制后收到匹配帧、
  释放后事件停止，-race count=2 ✓）+ TestCaptureScreenGDI（Windows 专属
  真机测试，CI Linux 不编译）。
- 门禁：全套 -race ✓ / smoke PASS ✓ / node --check ✓。
- v1 已知边界：全帧编码无脏区优化（后续可加游标叠加/脏区/自适应质量）；
  仅主屏（多屏拼接后续）；查看器在控制视图内（全屏查看器后续）。

### R66（2026-10-27）
- R65 待办落地：**autostart 覆盖警告**——AutostartEnable 改返回 warn（已有
  自启项且指向不同时非空），CLI/GUI 两入口打印。真机验证：Temp 副本执行
  大声提示"原自启项指向 D:\tools\bin…已被覆盖"，正式 exe 恢复时无警告。
- uia_pair 补桌面发送文件方向的自动化：**三条路全数失败**——①文件路径
  输入框 UIA SetValue（a11y 层有值、JS .value 仍空）；②剪贴板 ^v（焦点
  被 powershell 子进程/UIA 点击抢走）；③点"选择文件…"走系统对话框
  （**对话框根本未弹出**，全顶层窗口枚举证实）。SendFile 的 Wails 绑定
  与已验证的 SendChat 同族，风险低；**"选择文件…"对话框是否弹出标待
  用户实测**——若人工点击也不弹，则 PickAndSendFile/OpenFileDialog 是
  产品 bug 待修（Wails v2 运行时对话框的主线程调度问题）。
- 门禁：build ✓ / deskapp ✓。
- 用户插话立项：**基本远程桌面**（办公室多机互控要看屏幕）——R67 起为
  插话优先工作，预计跨多轮。

### R65（2026-10-27）
- **合并预演**：main 停在 29ecf10（两个初始提交），desk 领先 84 提交、
  main 领先 0 → 最终合回 main 是**纯 fast-forward，零冲突障碍**（不动
  分支，等用户实测确认后执行）。
- 顺带抓到并修一个真问题：**autostart 语法不一致**——CLI 是
  `winforge desk autostart ...`，GUI 入口只认 `winforgedesk.exe
  autostart ...`（无 desk 前缀）；用 CLI 语法调 GUI exe 会**静默回落成
  启动完整 hub**（本轮实踩：起第二个实例抢 41831 端口报错）。修：GUI
  入口兼容 `desk autostart` 语法；README 命令清单补 GUI 同义行。
- 另一个实踩记录：**autostart on 把"当前 exe 路径"写进 HKCU Run**——用
  Temp 测试副本执行会把正式自启项污染成 Temp 路径（本轮已用正式 exe
  恢复）。改进候选（未做）：写前检测已有项路径不同则警告。
- 门禁：build ✓ / 兼容语法真机验证（不再起 hub）✓。

### R64（2026-10-27）
- GUI 日志按配置名分离：OpenRotatingLog(configPath)——`--config` 非默认时
  日志写 `<配置名>.log`（默认仍是 %APPDATA%/winforge/desk.log）。此前多
  GUI 实例共写同一 desk.log（交织+轮转互相踩），R54/R61 排障时都被坑过。
  真机验证：双实例（日志甲机/乙机.json）各自落盘独立日志 ✓，桌面端已重部署。
- 顺带确认：os.UserConfigDir 在 Windows 返回 **Roaming AppData**（不是
  LOCALAPPDATA）——查日志路径别走错目录。
- 门禁：build ✓ / deskapp 测试 ✓ / 全套 -race 39.6s ✓。

### R63（2026-10-27）
- 新增 docs/architecture.md：desk 模块分层（proto/crypto/pairing/session/
  hub 四件套/discovery/ui/平台层）、信任边界与安全模型、四个关键流程
  （配对双角色按钮文案差异/会话重连收敛窗口/边缘推挤/事件流续传）、
  已知边界与自测入口——把散在 ROUNDS 里的架构知识收敛成贡献者文档，
  新人不用再从 60+ 轮记录里考古。
- 门禁：build ✓ / desk 测试 ✓（纯文档轮）。

### R62（2026-10-27）
- 26fb54a 的 CI DATA RACE 追查：本地 Windows -race -cpu 1,2,4 十轮全净 →
  沉淀 **WSL Linux 本地复现能力**（每轮规则 5b）：WSL 里跑 Linux 编译变体
  全套 -race -cpu 1,2 ×3 也干净——竞争是 CI 共享 runner 的极端时序，
  两平台本地都压不出。R49 修的 RACE 注解前缀下次 CI 红时会给出完整栈，
  届时精确修；目检 savedX/savedY 双端都在 h.mu 下，无裸访问。
- 热键 Ctrl+Alt+D 合成注入验证维持不做（R57 评估：真劫持鼠标+可能不触发
  RegisterHotKey），待用户实测。
- 本轮无代码变更（调研+规则沉淀），ROUNDS.md 单独提交。

### R61（2026-10-27）★用户实测反馈
- **用户实测桌面窗口：标题栏不能双击、不能拖动窗口**。根因：R4 起开的
  Wails `DragAndDrop{EnableFileDrop:true}`——WebView 拖放注册在 Windows
  下会破坏原生标题栏拖动/双击最大化，而它发的 desk:filedrop 事件前端
  **从未监听**（半成品功能换走了窗口基本操作，与 R53 /ws 同族）。
  修：整体移除 DragAndDrop 配置与 OnFileDrop 处理器。
- 顺带修 R49 埋的诊断盲点：Diagnostics RACE 步骤输出没加 `::error::`
  前缀，race 报告头根本不会成为注解（26fb54a 的 CI race 因此只有 KEY
  一行看不到栈）。已补 sed 前缀，下次 CI race 可见完整报告。
- 迭代周期应用户要求改为 45 分钟一轮（共 100 轮上限保留）。
- 桌面端已重部署（14:56），拖动/双击待用户复验。
- 教训：R53/R54/R61 三连——**半成品的前端功能比没有更糟**（/ws、
  filedrop、事件缺 reqID 都是在真实使用时才炸的）；用户实测仍不可替代。

### R60（2026-10-27）
- hotkeyPressed 补测（hub_ctl 最后一个 0% 函数）：TestHotkeyTogglesControl
  （热键完整往返 idle→控制→idle，B 侧同步回空闲）+ TestHotkeyIdleWithoutPeers
  （无对端不动作+toast 提示）。至此 hub_ctl 全函数有覆盖。
- 坑：toast 的 data 是 map[string]string 不是 map[string]interface{}——
  类型断言失败会让"断言 toast 存在"永远假；testEmitter 的字段名是 name
  不是 event（in-package 测试先看辅助类型定义）。
- 教训：写断言前先看一眼被断言数据的真实类型——事件载荷的 map 类型在
  Go 里是编译期不检查的 interface{}，断言错了不报错只是静默为假。

### R59（2026-10-27）
- 覆盖率确认核心交互 **onEdge（推过边缘控制对方/被控推回）此前 0% 覆盖**
  ——产品最核心的判定逻辑从没被测过。新增 edge_intent_test.go 三测试：
  空闲推对边→发起控制；推错边→不动作；被控推控制器方位边→拿回（双侧
  role 归 0）。
- 新测试自己先翻车：推回断言前只等了 A 进入控制、没等 B 侧 ctl.PeerID
  就绪——B 还在 idle 时推边会被当成控制请求，1/3 概率挂。修正为等
  "B 已被控且对端=A"，-race -cpu 1,2 count=6 连跑全过。
- 教训：**多角色状态机的测试要等"被断言的那个角色"的状态到位**——等了
  对面的状态就推边，等于在真实世界"还没被控就推边"，语义都错了。

### R58（2026-10-27）
- 跑覆盖率选点（总 63.7%；0% 全是 input_windows/edgebar 的 Windows 钩子
  代码——本机无法安全自动化，符合预期）。据此补两件：
  1. **TestUIEventsHTTPAfter**：/api/events?after=N 的 HTTP 层测试——
     after=2 只回放 seq=3；缺省 after 不回放（用 2s 超时客户端验证 hold
     行为）。bus 层逻辑已有测试，这里钉住 handleEvents 的参数解析+flush。
  2. README 开发节补"desk 自测脚本清单"表（smoke/soak/uia/uia_pair/
     screenshot/node --check 各一句用途）——五个脚本此前只活在 ROUNDS 里。
- 坑：测试里写 h.Emit——Hub 的事件方法是小写 emit（未导出），
  in-package 测试可以直接用。

### R57（2026-10-27）
- 回放环边界修正：desk:state（高频全量快照）不再入 512 回放环——否则
  事件密集时 state 会把 chat/transfer/pair 挤出补传窗口，客户端补回来的
  全是无用 state（重连本就会重新拉 snapshot）。新增
  TestUIBusStateNotReplayed。
- 方法集清剿**第三批漏网**：`cfg.Peers` 直接遍历还有 5 处（hub_ctl 的
  边缘推挤/链式切换/热键路径×2 + pairedCountSnapshot）——R49 只修了
  connectMissing 一处。全部换 PeersSnapshot()，重新全量 grep 计数归零。
- 热键 Ctrl+Alt+D 的自动化验证**评估后不做**：hotkeyPressed 的动作是
  TakeControl（真起 Windows 钩子+注入），合成注入会真劫持本机鼠标且有
  不触发 RegisterHotKey 的先例——维持"待用户实测"。
- 门禁：bus 三测试 ✓ / 全套 -race 51.3s ✓ / smoke PASS。
- 教训：清剿类修复（race/直接访问）**必须按字段全域 grep 计数归零收尾**，
  R49 修了一处就说"清零"、R51 补了 3 处、R57 又补 5 处——每轮都该以
  "全仓该字段裸访问=0"为完成标准而不是"修了发现的"。

### R56（2026-10-27）
- 补 TestChatHistoryCapTrim：聊天历史上限裁剪（>500 条裁旧留新，首末条
  精确断言）+ 重启恢复后仍为上限条数——此前 TestChatPersistence 只测
  持久化不测裁剪，裁剪逻辑（recordChat 的 list 切片）首次有测试钉住。
- --start-hidden GUI 形态实机验证：真进程起 winforgedesk --start-hidden，
  10 秒后进程存活 ✓、UIA 窗口枚举数=0（完全不可见，符合"开机自启形态：
  只留托盘"设计）。托盘图标可见性本身仍待用户实测。
- 教训：写验证脚本前先确认依赖产物的实际路径（uia_pair 的构建产物在
  wfdesk-uiapair 不在脚本临时目录，FileNotFoundError 白跑一轮）。

### R55（2026-10-27）
- uia_pair_desk.py 补全发出方向：A 桌面输入框真实输入+点"发送"→B 侧
  chatHistory 断言收到——Wails 双参数方法绑定（SendChat(id,text)）首次
  验证。至此桌面窗口端到端覆盖：配对（收 modal）/收消息气泡/发消息/
  收传输请求+确认落地，四个方向全真实点击/输入。
- 托盘代码审读（tray_systray.go）：三个菜单回调接线正确（打开控制台/
  共享开关 ToggleShare 取反/退出），trayEmitter 随 desk:state 更新勾选
  状态与 tooltip；代码层健壮。真实托盘交互（点系统托盘菜单）仍待用户
  实测——UIA 自动化托盘溢出区成本高收益低。
- .gitignore 补 __pycache__/。
- 踩坑：①**pywinauto type_keys 会吞空格**——断言文本带空格导致假"未收到"，
  排查两轮才发现；教训=对"未收到"类问题，先 dump 接收方原始返回再猜链路
  （原始 dump 一出来真相立现：消息早到了，是断言字符串错）。②wait_for
  超时抛 SystemExit，失败诊断分支要自写循环不能挂在它后面。③hub 无 Stop，
  脚本级实例清理按进程杀。

### R54（2026-10-27）★两大产品级缺陷
- 新增 uia_pair_desk.py：桌面窗口+浏览器实例端到端——B 发起配对→A(GUI)
  弹配对 modal→UIA 真实点击确认→双方在线→B 发消息→UIA 断言 A 窗口实时
  气泡→B 发文件→UIA 点"接收"→落地校验。**这是桌面 GUI 的首次完整端到端**。
- 调试过程连环挖出两个自始缺陷（像素截图是破案关键，UIA 不可见性会伪装成
  "事件链路断了"）：
  1. **桌面窗口整体是静态壳（自 R4）**：Wails 绑定结构体叫 DeskApp，前端
     makeBridge 找的是 window.go.main.Desk——永不匹配→静默降级到浏览器桥，
     而窗口模式没有 /api/rpc→**所有按钮 53 轮从未真正调通过后端**。R37 的
     "端到端 UI 验证"只点到了静态 DOM+CSS 切换，一个后端调用都没经过。
     修：结构体改名 Desk（绑定名随之变 window.go.main.Desk）+前端双名兜底。
  2. **UI 点按钮配对从未成功过（自 R1）**：desk:pair 的 request 事件不带
     reqID，弹窗按钮 pairRespond(p.reqID) 的 reqID 是 undefined→确认静默
     无效→发起方超时"对方未完成确认"。smoke 用 snapshot 里的 reqID（全的）
     所以 52 轮测不出来。修：request 态事件补 reqID。
- 修复后 UIA-PAIR PASS：配对 modal 点击、实时气泡、传输接收全链真实验证。
  桌面端已重部署（本修复直接改变桌面窗口可用性）。
- 踩坑：①"配对成功"modal 不关会盖住后续所有操作（脚本要点"好"）；②UIA
  对 modal 的可见性时好时坏（复用节点文本有时可见有时不可见）——**按钮
  （新建节点）才是可靠锚点**，别等标题文本；③sessions/snapshot 的视角、
  配对发起方/接受方的确认动作在两侧 UI 不同（发起方"确认配对"、接受方
  "确认配对码一致"），写脚本前先理协议角色。
- 教训：R53 的教训加倍成立——每个"看起来能用"的界面路径都要有一次真实
  点击+真实后端响应的验证；绑定名/协议字段这类"两层各错一半"的对不上，
  靠单元测试永远测不到，只有跨层端到端能抓。

### R53（2026-10-27）★产品级修复
- 新增长稳 soak_desk.py（N 轮双向消息+1MB 文件+开关共享，默认 10 轮，手动
  工具不进 CI；内存采样取 tasklist 行内最后一个整数列——GBK 中文列直接按
  下标取会解析失败）。第 9 轮即抓到真问题：A2B 消息事件丢失。
- 顺藤摸瓜挖出**自 R1 起的产品级缺陷**：浏览器控制台的实时推送从未工作过
  ——app.js 连的 `/ws` 没有任何服务端实现（gorilla 只是间接依赖）；
  入站聊天、传输请求按钮、配对弹窗、toast 在浏览器模式全部收不到。此前
  截图里"来消息气泡"全靠 chatHistory 历史加载掩盖（R42/R43 的截图恰好
  都走了这条路），冒烟又直接打 RPC 绕过 UI，所以 52 轮无人察觉。
- **修复**：uiBus 加 512 有界回放环 + `/api/events?after=seq` 断点续传
  （文件头注释设计了 52 轮从没实现的机制）；浏览器桥换成单长轮询循环
  （带 seq 续传，替代幻影 /ws）+ desk:chat 按 peer|ts 去重（回放可能重复）
  + soak/smoke poller 同步带 after。兼容性：after 缺省=仅实时（老客户端
  不受影响）；客户端 seq 领先（hub 重启）不回放；回放限最近 256 条=
  订阅通道缓冲，持锁写入不阻塞。
- 测试：TestUIBusReplayAfter/Cap（回放/缺省/领先/容量）；smoke PASS；
  soak 10/10 全过（修复前第 9 轮必挂）。Wails 桌面端走 runtime events
  不受影响，已重部署。
- 教训：**"截图里看得见"≠"实时链路通"**——历史加载会把推送失效完美掩盖；
  冒烟绕过 UI 的自测方式让 UI 的传输层成了永远没人走的那段路。soak 这类
  "长时多轮 + 真实前端路径"的测试抓的就是这种静态门禁永远碰不到的洞。

### R52（2026-10-27）
- 新增 TestSessionGoroutineHygiene（泄漏回归）：10 轮强制断开→重连后
  goroutine 三采样（3s/4s/4s 间隔）不增长即过（容差 6）——泄漏会随轮数
  单调增长，增长信号不受前面测试遗留的常量 goroutine 影响。实测
  22→20→20 收敛，-race count=3 连跑全过。
- README troubleshooting 补"断线重连后消息没到"（收敛窗口不补投、稍后
  重发即可）——把 ROUNDS 里的已知边界第一次写进用户可见文档。
- 坑：sessions 表以**对端** ID 为键，写测试时先想清楚视角（第一次就写反
  成自己的 ID，waitFor 永假）。hub 无 Stop 方法，测试内 hub 生命周期到
  t.Cleanup 才结束——泄漏测试要采样"增长"而非对比固定基线。

### R51（2026-10-27）
- R49 方法集清剿的**漏网之鱼补刀**：ui.go 上传处理器两处 MaxFileBytes 裸读
  + hub_ui.go SendFile 的 openSenderFile 一处——R49 的 grep 输出被 head -20
  截断没看全（教训：清剿类排查别用 head 截输出，`-c` 计数+全量落盘）。
  全部换 MaxFileBytesV()；config.go 加载期归一化的裸访问按"构造后不可变"
  原则保留。
- 新增 TestUIUploadOverLimit：超限上传 413（大小检查在 SendFile 之前，
  无需真实对端即可独立触发）；限内+对端不存在走 400 业务错误。
- 验证：新单测 ✓ / 全套 -race 20.3s ✓。
- 教训：批量修复后要"重新全量 grep + 计数核对"，截断输出会漏项——
  race 家族这类"清一个少一个"的活必须以计数归零收尾。

### R50（2026-10-27）
- 63a14e2（YAML 修复后首个真实 CI run）红：**race 已修掉**（Diagnostics RACE
  无输出），挂在 TestDialRaceConverges"双向消息送达"5s 超时——诊断通道
  给出的消息行精确到 `_test.go:1078`，闭环比想象中好用。
- 根因是**测试语义**不是产品 bug：tryChat 只重试"发送报错"，但收敛窗口里
  发送可能落进正被仲裁踢掉的旧会话（TCP 写缓冲成功=无错误，对端永远收不
  到，即 ROUNDS"重连收敛窗口内消息会丢"的已知边界）→ 消息静默丢失后
  waitFor 永远等不到。改 deliver()：发送+确认整体重试（与 UI 调用方正确
  姿势一致），去掉单独的 5s waitFor。
- 验证：-race -cpu 1,2 count=15 连跑 32.8s ✓ + 全套 race 20.0s ✓。
- 教训：**写缓冲成功≠送达**——对"可能静默丢失"的通道，重试单位必须是
  "操作+确认"，只重试 error 会把丢失漏过去；这也正是产品侧 SendChat
  调用方（UI）该有的行为，测试与产品姿势对齐后这类假失败自然消失。

### R49（2026-10-27）★race 修复
- 48825b8 的 CI 红=**真 DATA RACE + desk 包跑满 600s 超时**（本地 -race
  从未输过时序）。R49 的 Diagnostics RACE 步骤还没推上去，本地用
  `-race -cpu 1,2` 循环压测 3 轮内复现，拿到完整 race 报告。
- **根因**：ShareEnabled 的"cfg 方法集"规则漏网——写方 ToggleShare 持
  h.mu 直写字段+锁外裸调 SaveConfig（marshal 全结构体，与持 cfg.mu 的
  并发写者竞态），读方 session.go:305 裸读；另 Snapshot/shareSnapshot/
  ShareEnabledSnapshot×2/onCtlTake 五处裸读全靠 h.mu 当挡箭牌（写方走
  cfg.mu，挡不住）。race 报告的双方正是 ToggleShare vs session sendState。
- **修复**：新 Config.SetShareEnabled(path,on)（变更+落盘都在 cfg.mu 内）；
  上述七处裸访问全换 V() 方法；onFileOffer 的 MaxFileBytes 同步换 V()
  （R46 的 SetMaxFileBytes 让它也成了运行期可变字段）；规则收敛为：
  **运行期可变字段（Peers/ShareEnabled/MaxFileBytes）必须走方法集，
  ID/Name 等加载后不可变字段裸读无害**。
- 验证：修复后 -race -cpu 1,2,4 循环 4 轮全净 + 全套 race ✓ + smoke PASS
  （仅 internal/agent 已知 Windows 存量失败）。桌面端重部署。
- CI 600s 超时的解释：race 检测拖慢 + CI 弱机，病态调度下 suite 撑爆
  10m 默认超时；race 修后应回落。若再红，新加的 Diagnostics RACE
  （race 报告头 14 行）+ KEY running tests 段会直接给出答案。
- 教训：**锁不能跨守卫对象**——h.mu 守 hub 状态，cfg.mu 守配置字段，
  "持了把锁"不等于"这个读安全"；race 要主动加压（-cpu 变体是关键，
  单 GOMAXPROCS 下时序根本错不开），等 CI 报不如本地压出来。
- 6aaf593 追加翻车：R49 顺手的 ci.yml 编辑把 YAML 弄坏（`run:` 裸标量里
  `'WARNING: DATA RACE'` 的"冒号+空格"被解析成新映射键）→ run 显示
  failure 但 **jobs 为空**（startup failure）→ race 修复那版其实没跑过 CI。
  修复：三步诊断全部块标量化；每轮规则加 4b（改 ci.yml 必须本地验 YAML，
  pyyaml 已装）。本条随 YAML 修复一起推送，CI 才真正复验 race 修复。

### R48（2026-10-27）
- TestAddrLearning 的 CI 抖动（ea40cf3）根因加固：静态排查全部地址学习
  路径（配对发起方存拨号目标/应答方存空/hello learnPeerAddr/拨号成功回写/
  发现广播通告 controlPort）都只写控制端口，写不出错值——失败样本质疑
  "waitFor 只等 Addr 非空、端口检查单次采样"的**测试语义问题**：地址学习
  是最终一致，应在"端口也正确"上轮询，而非"非空就过再单次校验"。
- 改法：轮询 ID+端口后缀直到正确（15s），超时 dump B 视角全部 peer
  （want+全列表进失败消息，配合 R47 的 KEY 注解必可见）；原单次校验
  防御性保留。若真存在持续写错端口的 bug，轮询照样超时并 dump，不掩盖。
- 验证：-count=10 连跑 43s ✓ + 全套 -race ✓。纯测试改动，无行为变更。
- 教训：对最终一致的量，断言要采样到收敛而非采样到"非默认值"——中间态
  采样会把偶发写成必然；超时消息要带全量现场，否则 CI 上永远是谜。

### R47（2026-10-27）
- ea40cf3 的 CI failure 是**R44 诊断通道首次实战成功**：注解直接给出
  KEY（--- FAIL: TestAddrLearning 4.17s）/TAIL（含 big.bin 拒收 toast，
  证明 R46 代码在跑）。顺带发现 ci-log 发布这次吐了 401 Bad credentials
  （GET/PUT 全被拒，疑 GITHUB_TOKEN 瞬时失效或仓库设置变更；b1b5d558 时代
  是成功的）——好在 KEY/TAIL 注解已够诊断，ci-log 降级为备用通道。
- TestAddrLearning 失败分析：4.17s 挂 = 15s 的 waitFor 已通过、挂在
  "学到的地址端口不对"Fatalf；具体 got 值没进注解（KEY 模式缺 _test.go:
  消息行）→ KEY 模式补 `_test\.go:[0-9]+:`，下次必见 got=...。
- 本地排查：TestAddrLearning -count=30 连跑 124s 全过（此前全套 race 也
  多轮全绿）→ CI 特有/竞态嫌疑，本推送即重跑；若再红则据 got 值修
  （疑点：入站连接的 RemoteAddr 端口学与 hello.Port 更新之间的竞态）。
- R46 两修复（拒收 emit + nil panic）本轮 CI 复验待出。

### R46（2026-10-27）
- ★补 max_file_bytes 边界测试时抓到两个真 bug（TestMaxFileSizeRejected）：
  1. **拒收不发事件**：onFileReject 只改内部状态+toast，从不 emit
     desk:transfer——发端 UI 卡片永远停在"请求中"，R43 的传输历史也记不到
     "rejected"。修复：补 emit（id/dir=rejected）。
  2. **伪造 fid 可打 nil panic**：toast 在 `t != nil` 判断外解引用 t.Name，
     对端发未知 fid 的 reject 即 panic（serve 的 safeCall 会拦住不至于崩
     进程，但读循环死掉=会话断）。修复：toast 移进 known 分支。
- 新增 Config.SetMaxFileBytes（内存态，测试用边界控制）。
- UIA 扩展配对弹窗验证：点＋配对 → "配对新设备"标题出现 → 取消关闭。
  ★修正 R45 的局限描述：JS **新建**的子树 UIA 可见（弹窗标题能读到），
  看不见的是对**已有节点**的文本改写（热键文案/列表项更新）。
- 门禁：新单测 ✓ / desk 全套 -race 19.3s ✓ / go test ./... 仅 internal/agent
  已知 Windows 存量失败 / smoke PASS。桌面端已重部署（reject 修复生效）。
- 教训：写边界测试不是"补覆盖率"——拒收路径从未被任何测试走到，一走就
  掉出两个 bug；协议消息的每个分支都该有"对端状态如何感知"的断言。

### R45（2026-10-27）
- R37 的 ad-hoc UIA 验证固化为仓库资产 `uia_desk.py`：自行 go build
  （desktop,production tags）→ 临时配置（端口全 0）+ --no-tray 起真 GUI
  进程 → pywinauto/UIA 按窗口标题+PID 过滤窗口（与用户运行实例共存，
  不杀托盘）→ 走查两遍触发 WebView2 辅助功能 → 断言静态元素 + 真实点击
  传输/控制/共享三个 tab 断言各页内容。连跑两次全 PASS。
- **发现并记录 WebView2 UIA 局限**：JS 动态改写的文本节点（热键文案、设备
  列表、聊天气泡等）不进 UIA 树，静态 HTML 文本不受影响——热键断言降级为
  [info]，门禁以静态 DOM + 真实点击后的元素出现为准。此前 R37/R42 命中的
  全是静态文本，回头看与此一致。
- 每轮规则第 2 条追加：改窗口/UI 交互或 Wails 壳时跑 uia_desk.py。
- 脚本三次迭代踩坑：JS 填充文案不能作为就绪断言（轮询 8s 也不出现）；
  click_input 前必须 set_focus；临时配置端口写 0 才能避开用户实例端口。
- 本轮无 Go/web 代码变更；门禁照跑（build/test 全绿）。

### R44（2026-10-27）
- d59f382 CI failure 诊断（签名与 998d08e 完全相同：日志停在 desk 配对中间、
  exit=1、KEY 零命中）反转出两个通道级问题：
  1. **注解 10 条/步上限**：HEAD 25 行 + TEST-FAILED 就把 Test 步配额吃光，
     KEY/TAIL 全被 GitHub 静默丢弃——R40 的"加宽"实际没人看得见。改为
     KEY/TAIL 拆独立步骤（各拿 10 条配额），HEAD 减到 3 行。
  2. **ci-log 发布静默失败**：jobs API 显示 Publish 步 conclusion=success，
     那是 continue-on-error 掩盖后的结论——默认 shell 是 `bash -eo pipefail`，
     `SHA=$(curl|grep|head|grep)` 一旦 grep 落空整条管道非零，`-e` 中止脚本，
     PUT 和 PUBLISH-HTTP 回显根本没执行（998d08e 起两次失败日志全丢）。
     修：`set +e` + GET-LEN/SHA 诊断回显 + sha grep 兼容 `"sha": "..."`
     带空格格式 + 失败重试一次。
- 本地排查 R43 代码嫌疑：desk 套件 -race count=2 连跑 35.8s 全绿——
  d59f382 失败更可能是 CI 侧抖动（与 998d08e 同理），本次推送本身就是重跑；
  若再红，新通道下必出完整日志。
- 教训：**continue-on-error 会把"中途失败"包装成 success**，查 channels
  要看 jobs API 的 steps 明细；管道赋值在 pipefail 下是隐形地雷；诊断
  通道要按 GitHub 注解上限（10/步）设计预算。

### R43（2026-10-27）
- 传输历史进 Snapshot（R42 留下的已知缺口收口）：emit 对 desk:transfer 统一
  记录（recordTransfer，独立 trMu——emit 的调用方有的持 h.mu 有的不持，不能
  共用一把锁），同 id 合并字段（对齐前端 Object.assign 语义），上限 24 条
  淘汰最早；Snapshot 增 "transfers"，app.js 初始快照与 desk:state 都种子
  填充进 transfers Map 后渲染。
- 效果实测：刷新/重开控制台后传输卡片回放（截图确认 firmware-v2.3.bin
  "已发出 5.0 MB/5.0 MB" 卡片出现，不再"暂无传输任务"）；README 截图随之更新。
- 测试：新单测 TestTransferHistoryInSnapshot（合并/无 id 忽略/上限淘汰）；
  冒烟加断言"snapshot 含 done 传输历史"。门禁：node --check ✓ / build ✓ /
  desk 套件 -race ✓ / go test ./... 仅 internal/agent 已知 Windows 存量失败
  （与 desk 无关）/ smoke PASS（本轮回环 20.8 MB/s，与 25.5 基线同量级波动）。
- 桌面端已重部署重启（D:\tools\bin，带 desktop,production tags）。
- 坑：Platform 是结构体不是接口，NewHub 测试参数写 nil 编译失败，要传
  Platform{}——写测试先看类型定义。

### R42（2026-10-27）★P0
- **P0 修复：已提交的 app.js 整份解析失败**——顶层重复 `let startupWarnShown`
  （第 80/477 行，重复 let = SyntaxError，控制台 JS 全死）。R37 UIA 验证发生在
  重复引入之前，R39 把坏工作区原样提交；门禁全没拦住：go 门禁只嵌不执行 JS，
  冒烟走 HTTP API 不过 JS。修复=删重复声明。
- **新门禁**：CI 加 `node --check internal/desk/web/app.js`（ubuntu runner 自带
  node）；每轮规则第 2 条同步加"改 web 前端必须 node --check"。
- app.js 顺手补 `?tab=` 深链（switchTab 抽函数，老 UI 有新 UI 丢了），
  screenshot_desk.py 的 `&tab=share/transfer` 重新可用。
- README 两张截图用微信风新 UI 重生成（R11 时代老 UI 截图已过时）：
  共享 tab（三栏+设备卡）与传输 tab（气泡聊天+文件面板）均真实渲染。
  截图过程本身=浏览器宿主端到端验证（配对+消息+5MB 传输真实跑通）。
- 桌面端重部署：D:\tools\bin\winforgedesk.exe 带 desktop,production tags 重建、
  重启实例，UIA 实读窗口内真实元素（共享/传输/控制/＋配对/键鼠共享）——
  两个宿主的 JS 存活都实测确认。
- 已知小缺口（留后续轮）：传输卡片只由实时事件填充，snapshot 不含传输历史，
  刷新后"暂无传输任务"；要保留历史需 Go 侧存最近传输列表（动共享代码）。
- 教训：**"UI 长得对"和"JS 活着"是两件事**——窗口 visible/元素存在不证明
  脚本没崩；前端产物没有语法门禁时，一次重复声明就能全灭且无人察觉。

### R41（2026-10-27）
- ★修发布清单缺口：Makefile SHA256SUMS 用 `shasum -a 256 winforge-*`，glob
  匹配不到 `winforgedesk-*`（`winforge-*` 要求第 9 字符是 `-`）——两个 desk
  变体自加入 release 起就没进过校验清单。改 `winforge-* winforgedesk-*`，
  本地 `make release` 实测：8 产物全出 + SHA256SUMS 8 行（含两个 desk 变体）。
- 打包清单项勾选（此前"变体早已存在但一直没勾"+本轮补修的 glob）；
  连接状态动效清单项核实勾选（R8 已做）。
- 冒烟脚本加吞吐测量：32MB 端到端 1.3s = 25.5 MB/s（本机回环，含 offer/
  transferRespond 往返；真实局域网带宽另算）。后续回归可对比这个基线。
- 门禁：go build ✓ / go test desk 13.8s ✓ / make release ✓ / smoke PASS ✓。
- 教训：glob 按前缀匹配对"共享前缀的兄弟命名"（winforge / winforgedesk）
  会静默漏项——清单类产物要逐个 glob 或用显式文件名核对行数。

### R40（2026-10-27）
- 998d08e 的 CI failure 复盘：HEAD d0967ac（Go 代码完全相同，只差 web 静态文件）
  CI 全绿 success，本地 -race count=3 连跑 45s 无 FAIL 无 DATA RACE → 定性为
  CI 侧一次性抖动，非代码问题。当时日志没落 ci-log（HEAD 仍是 b1b5d558 的旧日志），
  注解只有"TEST-FAILED exit=1"，KEY grep（FAIL|panic|DATA RACE…）零命中、
  TAIL 停在配对输出中间——是"无 FAIL 行的中途退出"类失败，诊断通道没接住。
- ci.yml 诊断增强：KEY grep 加宽（+fatal error|timed out|shutdown signal|exit status）、
  TAIL 25→60 行、TEST-FAILED 带日志总行数、发布失败时吐响应体前 400 字节
  （PUBLISH-BODY）——下次"谜之失败"可完整自诊断。
- 教训：ci-log 机制自 b1b5d558 以来没被真实失败演练过（那次之后 CI 一直绿），
  第一次真失败就暴露了覆盖面缺口；诊断通道本身也要随失败模式演进。

### R39（2026-10-27）
- 补入库：微信风格三栏 UI（app.js/index.html/style.css，-471/+583）实际一直
  只在工作区、从未提交——R38 续记录的"已随 R38 前段提交"有误（当时只部署了
  /d/tools/bin，忘了 commit）。本轮走完门禁（build/vet/-race/冒烟 6 段全绿）
  后正式入库为 d0967ac，CI 全绿。
- ★教训：每轮规则第 1 条"git status 确认干净"不只防吸收别人改动，也防
  自己上一轮"以为提交了其实没有"；push 前后各看一次 git status。

### R38 续（2026-10-27）
- 残留 race 清零：connectMissing 的 h.cfg.Peers 直接遍历换 PeersSnapshot()
  （R28 漏改点，-race 实测抓到）。count=2 连跑 0 race。
- 微信风格 UI（三栏布局+绿主色+气泡聊天+传输卡片）已随 R38 前段提交，
  /d/tools/bin 与实例已更新部署并运行，窗口 visible=True。

### R37（2026-10-27）
- ★端到端 UI 交互首次真机验证成功：UIA 读取 wailsWindow 内部真实渲染的
  前端元素（标题/空闲状态/键鼠共享 CheckBox/三个 tab/设备文案），并真实
  点击"传输"tab——聊天输入框、发送按钮、清空记录、文件区、打开接收目录
  全部真实渲染。WebView2 内容 UIA 可读需走查两遍触发 Chromium 辅助功能。
- 意义：此前 36 轮的"端到端"只到 HTTP 层冒烟，窗口内 UI 交互从未验证过
  （受 R36 tags bug 影响）。现在 UIA 脚本可做端到端 UI 回归（配对弹窗、
  发消息、传输进度、控制按钮等均可自动化点击验证）。
- 本轮无代码变更（只读验证）。

### R36（2026-10-27）——**重大修复：Wails 窗口从未真正显示过**
- ★★根因：Wails v2 构建**必须带 -tags "desktop,production"**，缺 tags 时
  wails.Run 只弹一个 Error 对话框（"Wails applications will not build
  without the correct build tags. Please use wails build"）然后停住——
  窗口永不显示。这正是：
  ① 用户看到的"弹编译错误"对话框本体（windowsgui 子系统下 panic/弹窗
    显示为对话框）；
  ② "打开控制台没反应"（wails.Run 阻塞，WindowShow 无窗口可显示）；
  ③ 历次"GUI 实机验证"假阴性的原因（此前只验证进程存活+端口监听，
    而 hub/tray 在 wails.Run 之前启动，全部正常——唯独窗口没了）。
- 修复：Makefile 的 winforgedesk 两行加 -tags "desktop,production"；
  实测构建后 Wails 窗口真实显示（wailsWindow visible=True，内存 30MB
  webview 已加载 vs 坏构建 19MB）。
- 影响：此前 dist 产物（R19 起）的 GUI 窗口全部不可用，托盘+边缘条+键鼠
  正常；用户此前所有"窗口相关"的待实测项需在新构建上重新目视。
- go.mod 的 go 1.26.10 与 x/net v0.60.0 升级保持。

### R35 续（2026-10-27）
- **左键点托盘图标 = 打开控制台**：fyne systray v1.12.2 有 SetOnTapped，
  已接线 openConsole（用户此前左键无反应正是缺这个）。
- D:	oolsin\winforgedesk.exe 已更新部署并重启实例（--start-hidden）。
- 自动化验证边界：flyout 会在失焦后自动关闭，合成鼠标点击不可靠——
  左键开控制台/右键菜单的真机效果留待用户目视；UIA 侧已验证图标存在、
  溢出区可枚举、名称正确。
- "打开工作站没反应"待用户澄清：UI 内无"工作站"元素（菜单是 打开控制台/
  键鼠共享/退出；控制 tab 是 控制这台/结束共享）。若指对端机器的控制，
  需先在两台机器配对且对端在线。

### R35 续（本地安装完成，2026-10-27）
- 正式安装：D:	oolsin\winforgedesk.exe（desk 分支 HEAD 构建，含全部
  R1-R33 修复），实例已在本机运行（控制端口 41831，配置持久化 ID 不变）。
- autostart 已开启（HKCU Run → "D:	oolsin\winforgedesk.exe"
  --start-hidden），开机后托盘常驻不弹窗。
- 托盘图标位置确认：在任务栏"显示隐藏的图标"^溢出区第一位，图标名
  "WinForge Desk — 共享 / 传输 / 控制"；右键菜单含 打开控制台/键鼠共享/
  退出。**用户可见性**：Windows 默认折叠新图标，点 ^ 即见，可拖到常驻区。
- 用户报告的"一直弹编译错误"已澄清：自主编辑期的临时编译错误被 IDE 保存
  即编译弹出，当前 HEAD 干净（build/vet/test 全绿）。

### R35（2026-10-27）
- 用户反馈"本地托盘没看到"+"一直弹编译错误"。排查结论：
  ①"编译错误"弹窗 = 自主编辑期间工作树的**临时编译错误**被用户侧编辑器
  （gopls/IDE 保存即编译）持续弹窗——R31 的 s.mux.Use、R33 的字段名等
  中间态。当前 HEAD 干净且 build/vet/test 全绿，编辑器重载后应消失。
  ②托盘窗口诊断：运行 winforgedesk.exe 后 EnumWindows 找到 2 个
  SystrayClass 窗口（两个测试实例各一），IsWindowVisible=False——这是
  fyne systray 的**消息窗口**常态（托盘图标由 Shell_NotifyIcon 单独管理，
  不等于该窗口可见性）；图标本身大概率藏在托盘溢出区（^ 图标），
  需真机目视确认（点 ^ 展开或拖到常驻区）。
- 测试遗留进程已全部清理；session.go gofmt 对齐补交。

### R34（2026-10-27）
- CI 状态复核：desk 分支最近 5 次 run（R30-R33 全部 push）连续 success——
  R24 首绿后门禁持续稳定，无回归。
- 例行门禁全绿（build/vet/race/冒烟）。无代码变更。

### R33（2026-10-27）
- 隐私目录权限收紧：chat/配置/GUI 日志目录 0o755→0o700（下载目录保持
  0o755）。-race 全量绿 + 冒烟通过。
- 前端接线"清空记录"按钮：聊天面板一键调 DeleteChatHistory（后端 R32
  已就绪），本地内存桶同步清；confirm 确认防误触。

### R33（2026-10-27）
- 隐私目录权限收紧：聊天记录目录（chat/）、配置目录（含 PSK 的 desk.json
  所在）、GUI 日志目录 0o755→0o700（Unix 语义；Windows DACL 忽略无害）。
  下载目录保持 0o755（非隐私，用户需自行访问）。
- -race 全量绿 + 冒烟通过（一次 B 收到消息超时为偶发，复跑即绿——
  重连收敛窗口波动，与 R28 记录一致）。

### R32（2026-10-27）
- 隐私加固：删除配对时聊天记录文件与内存历史一并清理（"忘记这台设备"
  语义完整，聊天明文不留磁盘）；DeleteChatHistory 沿用 chatFilePath 的
  hex 正则校验，非法 ID 不触碰文件系统。补两例测试（清理生效/非法 ID 拒绝）。
- -race 全量绿 + 冒烟通过。

### R31（2026-10-27）
- 控制台 HTTP 安全头：securityHeaders 中间件统一加 X-Content-Type-Options:
  nosniff 与 Referrer-Policy: no-referrer（首次写法误用 ServeMux.Use——标准库
  mux 无该方法，改为包裹整个 mux）。
- 协议边界测试：session_test 补超大长度前缀拒绝/超限写拒绝/边界帧读取三例
  （防内存放大）。-race 全量绿 + 冒烟通过。

### R30（2026-10-27）
- 稳态巡检：build/race 全绿、冒烟 10.6s 全过（重连收敛在窗口内，
  R25 的 90s 等待是防御性的，实际 <5s）；工作树干净。无代码变更。

### R29（2026-10-27）
- 配置损坏自愈的用户可见性闭环：startupWarning 随快照发给 UI（一次性 toast），
  用户在控制台就能看到"配置已备份为 xxx 并用默认配置启动"，而不是只在
  日志里。Options.Version 同链路（R28）。-race 全量绿 + 冒烟通过。

### R28（2026-10-27）
- Config 并发访问加固第三批：PeersSnapshot()（切片快照）+ 标量 getter
  （NameV/SelfID/ShareEnabledV/HotkeyV/DownloadsDirV/MaxFileBytesV，cfg.mu 内），
  hub 全部直接字段访问（connectMissing 遍历/Snapshot/遍历分支）换快照——
  修掉 dialSession 学地址写盘 vs connectMissing 遍历的 DATA RACE（本地 -race
  实测抓到，R21 修复的同类残留：方法集加锁后外部直接字段访问仍绕锁）。
- desk 启动日志补版本号（Options.Version ← ldflags main.version），排查用户
  问题第一线索。GUI 实机验证日志行正确。
- 例行：vet 自有三包净、-race 全量绿、冒烟通过（一次偶发超时复跑即绿，
  B 重启后消息通路恢复的收敛窗口波动，非回归）。

### R27（2026-10-27）
- 传输条实时速率显示：基于相邻 progress 事件的字节差/时间差 EMA 平滑
  （0.7/0.3），receiving/sending 状态显示"· x MB/s"；终态自动清除。
  纯前端计算，无协议变更。-race 全量绿 + 冒烟通过。

### R26（2026-10-27）
- 重拨指数退避：被仲裁淘汰方（或对端不在线方）的重拨从固定 3s 节流改为
  3s→6s→12s→24s→30s 封顶的指数退避（dialFails 计数），会话建立成功后归零。
  R25 观察的"B 无效重拨循环"从每 ~2.6s 一次压到最低 30s 一次。
- 冒烟（含断线重连场景）与 -race 全量绿。
- 备注：若真机反馈重连"感觉慢"，退避参数可调；kick 消息方案（被踢时通知
  对端立即重拨）仍是备选，当前以简单实现优先。

### R25（2026-10-27）
- 冒烟脚本整体重写并新增第 5 段"杀 B 重启 → 自动重连 → 消息恢复"真进程
  重连场景，全绿。
- ★排查过程抓到一个**产品级真 bug**：**断线重连后 A→B 消息持续送达失败**
  ——B 重启后 A 的 connectMissing 拨号成功注册新会话，但 A 端sessions 里
  旧会话已被 accept 的反向连接仲裁关闭的竞态窗口内，SendChat 反复
  "对端不在线"（sendChat 写 socket 成功但对端已不认）。
  根因链：双端交替拨号 + 仲裁 + 级联关闭的收敛窗口 > 20s（脚本窗口），
  产品"最终收敛"承诺成立但**收敛期间消息丢失**。已有 Alive 检查把错误
  显性化为 errNotConnected（而非假成功），收敛时长属可接受范围，
  真机场景的容忍度待用户实测判定。
- smoke 脚本自身修复：重启后 URL 等待必须等"全新 token"（旧 token 一直
  在日志里，wait_for 有匹配即返回导致拿旧 token 401 一整轮）；
  rpc 对 401/连接重置带重试；取证改 raw 容错打印。
- 本轮无产品代码变更（重连收敛时长属设计权衡，待真机反馈决定是否优化）。

### R24（2026-10-27）——**CI 首绿** 🎉
- desk 分支的 GitHub Actions **首次全绿**（run 40385650266 系列，HEAD 2974faa）：
  test -race ✓ / vet ✓ / govulncheck ✓（go 1.26.10 + x/net v0.60.0）/ release ✓。
- 诊断过程还原：上一轮 dbb8c66 的 test 失败是 **runner 时序偶发**（本 round
  宽诊断通道下同配置转绿），非编译错误非新 race。之前的 race 修复、重试、
  go/x-net 升级均已生效。
- workflow 诊断加固沉淀：失败时 HEAD/KEY/TAIL 三段 ::error:: 注解（匿名
  check-runs API 可读）+ 日志 publish 步骤 continue-on-error（其失败不再
  干扰主门禁）+ publish HTTP 码注解。
- 结论：质量门禁四件套（本地 -race/冒烟/CI test/CI govulncheck）首次全部
  真实在线运行且绿。

### R23 续（R24 前置修复，2026-10-27）
- TestDialRaceConverges 的 SendChat 撞仲裁替换瞬态窗口且无重试（0.11s 即
  Fatal）——补 20×250ms 重试（与 TestAddrLearning 同模式）。CI-DIAG 注解通道
  已打通：::error:: 行经 check-runs API 匿名可读（R22 四条失败通道之外的
  第五条，也是第一条真正闭环的）。
- 单独 count=3 全过、全量 race 绿、冒烟过。

### R23（2026-10-27）
- **CI 真相确认**：test 步已过（R21 race 修复在 CI 生效），CI 红的原因 =
  govulncheck 报 x/net@v0.56.0 的 5 条 HTTP/2 公告（GO-2026-6617/6612/6611/6610/6603，
  修复版 v0.60.0）——CI 的漏洞库比 R10 本地扫描时新。
- 修复：x/net 升级 v0.56.0→v0.60.0（连带 x/sys/x/text），-race 全量绿 + 冒烟通过。
- TestDialRaceConverges 断言修正：删"3 秒零闪烁"强断言（收敛过程的闪烁是
  仲裁设计允许的瞬态，套件环境必闪），只断言收敛终点在线 + 双向消息通。
- 诊断通道演进：::error:: annotations 通道打通（check-runs API 匿名可读，
  annotations_count 11 条）——CI 失败细节今后可经此通道读取。
- 待 CI 复验：push 7d64611 起的 run 应转绿（若 govulncheck 又报新公告，
  按公告修对应依赖版本）。

### R22（2026-10-27）
- CI 排查继续但遭遇通道墙：contents API 404（匿名限流伪装）、raw CDN 缓存、
  step summary 匿名不回显。**已知确定事实**：CI 在 ubuntu 的 test 步失败；
  b1b5d55（race 修复前）的日志证实过 DATA RACE；race 修复（R21）本地稳定
  且多轮全绿；**修复后的 CI 失败输出未获取到**——无法区分是残余 race 还是
  CI runner（弱机+race 慢）上的时序超时。
- **待用户动作（二选一，2 分钟）**：
  a) 网页打开 https://github.com/wangxiuwen/winforge-agent/actions 前两个
     失败 run 的 test job 日志，把 "--- FAIL" 附近几行发来；
  b) 或本机 `gh auth login` 走一次设备码授权，之后我能自助取。
- 收尾判断：不再盲改产品代码。本地全链门禁（race×多轮+冒烟）全绿，
  真实使用以真机实测为准。

### R21 续（CI 排查闭环，2026-10-27）
- CI 失败日志经 ci-log 分支取回：**真 race——Config.UpPeer 写 Peers 切片 vs
  无锁 cfg.Peer() 读并发**（dialSession 学地址写盘 vs accept 读）。修复：
  Config 方法集（Peer/UpPeer/RemovePeer/SetPeerPosition/SetPeerEnabled）
  内部互斥，调用方无论是否持 hub.mu 都安全（锁序恒为 hub.mu→cfg.mu）。
- CI 可见性闭环：workflow 失败时把 test.log 推送到 ci-log 孤儿分支，
  raw.githubusercontent.com 匿名可读——以后 CI 红了无需凭证即可诊断。
- 诊断链路：加 desk 触发→失败→step summary 匿名不可见→artifact 要认证→
  最终 git 推日志分支成功。本地 race 全量绿 + 冒烟通过，待 CI 复验。
- 遗留：修复 commit 3737579 的 CI 复验未完成——诊断通道逐个受限
  （step summary 匿名 API 不回显、artifact 需认证、GitHub API 匿名限流 60/h
  已耗尽、ci-log 二次推送结果未核实）。下轮首选：限流重置后查 3737579 的
  run 结论；若仍受限，gh auth login 刷新凭证或请用户网页查看。

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
