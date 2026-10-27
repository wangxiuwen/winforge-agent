# -*- coding: utf-8 -*-
"""桌面窗口实时事件链路端到端（R54）：Wails emit→runtime→JS 从未被验证过。

GUI 实例 A（固定控制端口 42001）+ 浏览器实例 B（42002，禁发现）：
  1. B 发起配对 → A 窗口弹配对 modal → UIA 真实点击"确认配对码一致"
  2. B 确认 → 双方在线
  3. B 发 2 条消息 → UIA 断言 A 窗口出现含消息文本的气泡（新建 DOM 子树
     UIA 可见，R45/R53 结论；消息文本经 renderChat 重建的气泡节点）
  4. B 发文件 → A 窗口出现"接收"按钮 → UIA 点击 → 文件落地校验
前置：先构建（go build -tags "desktop,production"），pywinauto 已装。
"""
import json, os, re, secrets, subprocess, sys, tempfile, time

REPO = os.path.dirname(os.path.abspath(__file__))
TMP = os.path.join(tempfile.gettempdir(), "wfdesk-uiapair")


def build_exe():
    os.makedirs(TMP, exist_ok=True)
    exe = os.path.join(TMP, "winforgedesk-uia.exe")
    subprocess.run(["go", "build", "-tags", "desktop,production", "-trimpath",
                    "-ldflags", "-s -w -H=windowsgui", "-o", exe, "./cmd/winforgedesk"],
                   cwd=REPO, check=True)
    cli = os.path.join(TMP, "winforge-uia.exe")
    subprocess.run(["go", "build", "-trimpath", "-ldflags", "-s -w",
                    "-o", cli, "./cmd/winforge"], cwd=REPO, check=True)
    return exe, cli


def write_cfg(name, ctl, ui, dl):
    cfgpath = os.path.join(TMP, name + ".json")
    with open(cfgpath, "w", encoding="utf-8") as f:
        json.dump({"id": secrets.token_hex(16), "name": name,
                   "ui_port": ui, "control_port": ctl, "discovery_port": 0,
                   "hotkey": "Ctrl+Alt+D", "downloads_dir": dl,
                   "max_file_bytes": 104857600, "share_enabled": True, "peers": []}, f)
    return cfgpath


def start_gui(cfgpath, workdir):
    return subprocess.Popen([sys.argv[1] if len(sys.argv) > 1 else
                             os.path.join(TMP, "winforgedesk-uia.exe"),
                             "--config", cfgpath, "--no-tray"],
                            cwd=workdir, stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL,
                            creationflags=subprocess.CREATE_NEW_PROCESS_GROUP)


def start_cli(cfgpath, workdir):
    p = subprocess.Popen([os.path.join(TMP, "winforge-uia.exe"), "desk",
                          "--config", cfgpath, "--no-tray", "--ui", "browser", "--no-open"],
                         cwd=workdir,
                         stdout=open(os.path.join(workdir, "out.log"), "w", encoding="utf-8",
                                     errors="replace"),
                         stderr=subprocess.STDOUT,
                         creationflags=subprocess.CREATE_NEW_PROCESS_GROUP)
    log = os.path.join(workdir, "out.log")

    def find_url():
        text = open(log, encoding="utf-8", errors="replace").read()
        m = re.findall(r"http://127\.0\.0\.1:(\d+)/\?t=([0-9a-f]+)", text)
        return m[-1] if m else None
    for _ in range(50):
        m = find_url()
        if m:
            return {"base": "http://127.0.0.1:%s" % m[0], "token": m[1]}, p
        time.sleep(0.3)
    raise SystemExit("B 启动失败")


def wait_for(fn, what, timeout=20):
    deadline = time.time() + timeout
    last = None
    while time.time() < deadline:
        try:
            last = fn()
            if last:
                return last
        except Exception as e:
            last = e
        time.sleep(0.3)
    raise SystemExit("超时: %s (last=%r)" % (what, last))


def rpc(base, token, cmd, args=None):
    import urllib.request
    body = json.dumps({"cmd": cmd, "args": args or []}).encode()
    req = urllib.request.Request(base + "/api/rpc?t=" + token, data=body,
                                 headers={"Content-Type": "application/json"})
    with urllib.request.urlopen(req, timeout=10) as r:
        return json.loads(r.read().decode())


def find_window(pid, timeout=25):
    from pywinauto import Desktop
    deadline = time.time() + timeout
    while time.time() < deadline:
        for w in Desktop(backend="uia").windows():
            if w.window_text() == "WinForge Desk" and w.process_id() == pid:
                return w
        time.sleep(0.5)
    return None


def names_of(w):
    try:
        return [str(c.element_info.name) for c in w.descendants() if c.element_info.name]
    except Exception:
        return []


def wait_name(w, want, timeout=10):
    deadline = time.time() + timeout
    while time.time() < deadline:
        if any(want in n for n in names_of(w)):
            return True
        time.sleep(0.4)
    return False


def click_button(w, label, timeout=8):
    deadline = time.time() + timeout
    while time.time() < deadline:
        try:
            btns = [c for c in w.descendants()
                    if c.element_info.name == label and c.element_info.control_type == "Button"]
            if btns:
                btns[0].click_input()
                return True
        except Exception:
            pass
        time.sleep(0.4)
    return False


def main():
    cli_proc = None
    subprocess.run(["taskkill", "/F", "/IM", "winforge-uia.exe"], capture_output=True)
    subprocess.run(["taskkill", "/F", "/IM", "winforgedesk-uia.exe"], capture_output=True)
    time.sleep(1)
    os.makedirs(TMP, exist_ok=True)
    import shutil
    for d in ("dl-a", "dl-b", "wd-a", "wd-b"):
        shutil.rmtree(os.path.join(TMP, d), ignore_errors=True)
    for d in ("dl-a", "dl-b", "wd-a", "wd-b"):
        os.makedirs(os.path.join(TMP, d), exist_ok=True)

    exe, cli = build_exe()
    gui = start_gui(write_cfg("UIA桌面机", 42001, 0, os.path.join(TMP, "dl-a")),
                    os.path.join(TMP, "wd-a"))
    w = None
    ok = True
    b = None
    try:
        w = find_window(gui.pid)
        assert w is not None, "A 窗口未出现"
        w.set_focus()
        for _ in range(2):
            names_of(w)
            time.sleep(1)
        print("[ok] A 窗口就绪 PID=%d" % gui.pid)

        b, cli_proc = start_cli(write_cfg("UIA浏览器机", 42002, 42012, os.path.join(TMP, "dl-b")),
                                os.path.join(TMP, "wd-b"))
        print("[ok] B 控制台 %s" % b["base"])

        # 1) 配对：B 发起 → A 弹 modal → 真实点击。
        # 注意：modal 标题/正文是复用节点改文本（UIA 常不可见，R45 局限），
        # 按钮才是新建节点——等按钮而非标题。
        ret = rpc(b["base"], b["token"], "pairStart", ["127.0.0.1:42001"])
        print("[dbg] pairStart ret=%r" % (ret,))
        assert click_button(w, "确认配对码一致", 15), \
            "A 未出现配对确认按钮; 相关元素=%s" % [n for n in names_of(w) if "配对" in n][:8]
        print("[ok] A 弹出配对弹窗并已点确认（实时事件链路首次验证）")
        time.sleep(0.8)
        # B 是发起方：pairOut 自 pairStart 即存在（snapshot 仅 addr/code 无 state）。
        # A(GUI 接受方) 点"确认配对码一致"后只有"等待对方确认"，无第二次弹窗；
        # 发起方确认在 B 侧直接 rpc 完成。
        rpc(b["base"], b["token"], "pairOutConfirm", [True])
        assert wait_for(lambda: rpc(b["base"], b["token"], "snapshot")["peers"] and
                        rpc(b["base"], b["token"], "snapshot")["peers"][0]["online"],
                        "双方会话在线", 20)
        print("[ok] UIA 全真实点击完成配对，会话在线")
        click_button(w, "好", 8)  # 关掉"配对成功"弹窗，否则盖住后续操作

        # 2) 实时消息 → A 窗口气泡（切到传输 tab 才渲染聊天；先点 tab）
        assert click_button(w, "传输"), "切传输 tab 失败"
        peer_b = rpc(b["base"], b["token"], "snapshot")["peers"][0]["id"]
        for i in range(2):
            rpc(b["base"], b["token"], "sendChat", [peer_b, "实时气泡%d" % i])
        if not wait_name(w, "实时气泡0", 12):
            import PIL.ImageGrab as IG
            r = w.rectangle()
            IG.grab(bbox=(r.left, r.top, r.right, r.bottom)).save(os.path.join(TMP, "bubbles.png"))
            hits = [n for n in names_of(w) if "实时" in n or "气泡" in n or "soak" in n]
            raise SystemExit("A 窗口未出现实时消息气泡; 含'实时'元素=%s 截图=%s"
                             % (hits[:6], os.path.join(TMP, "bubbles.png")))
        assert wait_name(w, "实时气泡1", 5), "第二条气泡缺失"
        print("[ok] 桌面窗口实时消息气泡渲染（Wails 事件链路验证）")

        # 3) A→B 方向：A 输入框真实输入+点发送（Wails 双参绑定 SendChat 未测过）
        edits = [c for c in w.descendants()
                 if c.element_info.control_type == "Edit" and "消息" in str(c.element_info.name or "")]
        if not edits:
            edits = [c for c in w.descendants() if c.element_info.control_type == "Edit"]
        assert edits, "找不到聊天输入框"
        edits[-1].click_input()
        edits[-1].type_keys("hellofromdesktop", pause=0.03)
        assert click_button(w, "发送", 6), "发送按钮点击失败"
        a_id = rpc(b["base"], b["token"], "snapshot")["peers"][0]["id"]
        deadline = time.time() + 15
        got_b = False
        while time.time() < deadline:
            try:
                if any(m.get("text") == "hellofromdesktop"
                       for m in rpc(b["base"], b["token"], "chatHistory", [a_id]) or []):
                    got_b = True
                    break
            except Exception:
                pass
            time.sleep(0.4)
        if not got_b:
            import PIL.ImageGrab as IG
            r = w.rectangle()
            IG.grab(bbox=(r.left, r.top, r.right, r.bottom)).save(os.path.join(TMP, "sendfail.png"))
            snap_b = rpc(b["base"], b["token"], "snapshot")
            hist_b = rpc(b["base"], b["token"], "chatHistory", [a_id])
            raise SystemExit("B 未收到; B.peers=%r\nB.hist(%s)=%r 截图=%s"
                             % (snap_b.get("peers"), a_id, hist_b,
                                os.path.join(TMP, "sendfail.png")))
        print("[ok] 桌面输入框→发送→B 收到（Wails 双参调用验证）")

        # 4) 实时传输请求 → 接收按钮 → 点击 → 落地校验
        src = os.path.join(TMP, "uia-payload.bin")
        payload = secrets.token_bytes(256 * 1024)
        with open(src, "wb") as f:
            f.write(payload)
        rpc(b["base"], b["token"], "sendFile", [peer_b, src])
        assert click_button(w, "接收", 15), "A 未出现接收按钮"
        time.sleep(2.5)
        got = os.path.join(TMP, "dl-a", "uia-payload.bin")
        assert os.path.exists(got) and open(got, "rb").read() == payload, "接收文件不一致"
        print("[ok] 实时传输请求→UIA 点接收→文件落地校验一致")

        print("UIA-PAIR PASS")
    except SystemExit as e:
        print("UIA-PAIR FAIL:", e)
        ok = False
    except AssertionError as e:
        print("UIA-PAIR FAIL:", e)
        ok = False
    finally:
        subprocess.run(["taskkill", "/F", "/PID", str(gui.pid)], capture_output=True)
        if cli_proc is not None:
            subprocess.run(["taskkill", "/F", "/PID", str(cli_proc.pid)], capture_output=True)
    if not ok:
        sys.exit(1)


if __name__ == "__main__":
    cli_proc = None
    main()
