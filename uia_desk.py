# -*- coding: utf-8 -*-
"""WinForge Desk 桌面端 UI 端到端回归（R45 固化 R37 的 ad-hoc 验证）。

起一个真 winforgedesk.exe（临时配置 + --no-tray，端口全 0 避开用户实例），
用 pywinauto/UIA 读 WebView2 渲染出的真实元素：
  1. 三个导航 tab（共享/传输/控制）与关键控件（＋配对/键鼠共享/热键文案）
  2. 真实点击"传输"→ 聊天输入框/发送/打开接收目录 出现
  3. 真实点击"控制"→ 空闲状态与操作提示出现
R37 经验：WebView2 的 UIA 可读性要走查两遍才触发 Chromium 辅助功能。

用法：py -3 uia_desk.py [exe路径]；不带参数则从仓库自行 go build。
只动自己起的实例（按 PID 过滤窗口），不碰已运行的用户实例。
"""
import json, os, secrets, subprocess, sys, tempfile, time

REPO = os.path.dirname(os.path.abspath(__file__))
TMP = os.path.join(tempfile.gettempdir(), "wfdesk-uia")


def build_exe():
    exe = os.path.join(TMP, "winforgedesk-uia.exe")
    os.makedirs(TMP, exist_ok=True)
    cmd = ["go", "build", "-tags", "desktop,production", "-trimpath",
           "-ldflags", "-s -w -H=windowsgui", "-o", exe, "./cmd/winforgedesk"]
    subprocess.run(cmd, cwd=REPO, check=True)
    return exe


def start_instance(exe):
    os.makedirs(TMP, exist_ok=True)
    cfg = {
        "id": secrets.token_hex(16), "name": "UIA测试机",
        "ui_port": 0, "control_port": 0, "discovery_port": 0,
        "hotkey": "Ctrl+Alt+D", "downloads_dir": os.path.join(TMP, "dl"),
        "max_file_bytes": 104857600, "share_enabled": True, "peers": [],
    }
    cfgpath = os.path.join(TMP, "desk.json")
    with open(cfgpath, "w", encoding="utf-8") as f:
        json.dump(cfg, f)
    proc = subprocess.Popen([exe, "--config", cfgpath, "--no-tray"],
                            cwd=TMP, stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL,
                            creationflags=subprocess.CREATE_NEW_PROCESS_GROUP)
    return proc


def find_window(pid, timeout=25):
    from pywinauto import Desktop
    deadline = time.time() + timeout
    while time.time() < deadline:
        for w in Desktop(backend="uia").windows():
            if w.window_text() == "WinForge Desk" and w.process_id() == pid:
                return w
        time.sleep(0.5)
    return None


def element_names(w, keyword=None):
    try:
        names = [c.element_info.name for c in w.descendants() if c.element_info.name]
    except Exception:
        return []
    if keyword:
        names = [n for n in names if keyword in str(n)]
    return names


def wait_name(w, want, timeout=8):
    """轮询等待元素出现（JS 渲染的内容不与窗口同时就绪）。"""
    deadline = time.time() + timeout
    while time.time() < deadline:
        if any(want in str(n) for n in element_names(w)):
            return True
        time.sleep(0.5)
    return False


def main():
    exe = sys.argv[1] if len(sys.argv) > 1 else build_exe()
    print("[exe]", exe)
    proc = start_instance(exe)
    ok = True
    try:
        w = find_window(proc.pid)
        assert w is not None, "窗口未出现（PID %d）" % proc.pid
        print("[ok] 窗口出现 PID=%d" % proc.pid)
        w.set_focus()
        # R37：走查两遍触发 WebView2 辅助功能
        for _ in range(2):
            element_names(w)
            time.sleep(1)

        for want in ["共享", "传输", "控制", "＋配对", "键鼠共享", "搜索"]:
            hit = wait_name(w, want, timeout=5)
            print(("[ok] " if hit else "[FAIL] ") + "静态元素: " + want)
            ok = ok and hit
        # 已知局限（R45）：WebView2 的 UIA 树不暴露 JS 动态改写的文本节点
        # （热键文案/设备列表/会话标题都是 JS 填充），静态文本不受影响。
        # 热键文案只作 info 观察，不作为门禁。
        if wait_name(w, "Ctrl+Alt+D", timeout=2):
            print("[info] 热键文案本次 UIA 可见")
        else:
            print("[info] 热键文案 UIA 不可见（已知局限，非故障）")

        def click_tab(label, expects):
            nonlocal ok
            btns = [c for c in w.descendants()
                    if c.element_info.name == label and c.element_info.control_type == "Button"]
            if not btns:
                print("[FAIL] 找不到按钮: " + label)
                ok = False
                return
            btns[0].click_input()
            time.sleep(1.5)
            names = element_names(w)
            for want in expects:
                hit = any(want in str(n) for n in names)
                print(("[ok] " if hit else "[FAIL] ") + "%s 页元素: %s" % (label, want))
                ok = ok and hit

        click_tab("传输", ["发送", "打开接收目录", "给对方发条消息"])
        click_tab("控制", ["空闲", "推过对应屏幕边缘"])
        click_tab("共享", ["＋配对"])

        # 配对弹窗：＋配对 → modal 出现 → 取消关闭（JS 动态创建的子树，
        # 若 WebView2 a11y 不暴露则降级为 info，见下方已知局限注释）
        pair_btns = [c for c in w.descendants()
                     if c.element_info.name == "＋配对" and c.element_info.control_type == "Button"]
        if pair_btns:
            pair_btns[0].click_input()
            time.sleep(1.2)
            shown = wait_name(w, "配对新设备", timeout=4)
            print(("[ok] " if shown else "[FAIL] ") + "配对弹窗: 标题出现")
            ok = ok and shown
            cancel = [c for c in w.descendants() if c.element_info.name == "取消"]
            if cancel:
                cancel[0].click_input()
                time.sleep(0.8)
        else:
            print("[FAIL] 找不到 ＋配对 按钮")
            ok = False
    finally:
        subprocess.run(["taskkill", "/F", "/PID", str(proc.pid)],
                       capture_output=True)
    if ok:
        print("UIA PASS")
    else:
        print("UIA FAIL")
        sys.exit(1)


if __name__ == "__main__":
    main()
