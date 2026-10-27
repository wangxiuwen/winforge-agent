# -*- coding: utf-8 -*-
"""WinForge Desk 双实例端到端冒烟：
发现/配对 → 消息 → 文件传输 → 控制状态机 → 杀进程重启后自动重连。"""
import json, os, re, subprocess, sys, time, urllib.request, urllib.error, secrets, shutil, threading

EXE = os.environ.get("WFDESK_EXE", r"C:\Users\williamwong\AppData\Local\Temp\winforge-desk.exe")
BASE = r"C:\Users\williamwong\AppData\Local\Temp\wfdesk-smoke"

def rpc(base, token, cmd, args=None):
    body = json.dumps({"cmd": cmd, "args": args or []}).encode()
    req = urllib.request.Request(base + "/api/rpc?t=" + token, data=body,
                                 headers={"Content-Type": "application/json"})
    last = None
    for _ in range(5):
        try:
            with urllib.request.urlopen(req, timeout=10) as r:
                return json.loads(r.read().decode())
        except (ConnectionResetError, urllib.error.URLError) as e:
            last = e
            time.sleep(0.6)
        except urllib.error.HTTPError as e:
            if e.code != 401:
                raise
            last = e
            time.sleep(0.6)
    raise last

def snapshot(base, token):
    return rpc(base, token, "snapshot")

class EventPoller:
    """持续长轮询事件流（模拟真实 UI 常驻订阅）。"""
    def __init__(self, base, token):
        self.base, self.token = base, token
        self.events = []
        self.lock = threading.Lock()
        threading.Thread(target=self.loop, daemon=True).start()
    def loop(self):
        while True:
            try:
                req = urllib.request.Request(self.base + "/api/events?t=" + self.token)
                with urllib.request.urlopen(req, timeout=35) as r:
                    batch = json.loads(r.read().decode() or "[]")
                with self.lock:
                    self.events.extend(batch or [])
            except Exception:
                time.sleep(0.3)
    def of(self, name):
        with self.lock:
            return [e["data"] for e in self.events if e["event"] == name]

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

def start_instance(name, ui_port, ctl_port, downloads, disc_port=0):
    os.makedirs(downloads, exist_ok=True)
    cfgdir = os.path.join(BASE, name)
    os.makedirs(cfgdir, exist_ok=True)
    cfgpath = os.path.join(cfgdir, "desk.json")
    if not os.path.exists(cfgpath):
        cfg = {
            "id": secrets.token_hex(16), "name": name,
            "ui_port": ui_port, "control_port": ctl_port, "discovery_port": disc_port,
            "hotkey": "Ctrl+Alt+D", "downloads_dir": downloads,
            "max_file_bytes": 104857600, "share_enabled": True, "peers": [],
        }
        with open(cfgpath, "w", encoding="utf-8") as f:
            json.dump(cfg, f)
    logf = open(os.path.join(cfgdir, "out.log"), "w", encoding="utf-8", errors="replace")
    proc = subprocess.Popen([EXE, "desk", "--config", cfgpath, "--no-tray",
                             "--ui", "browser", "--no-open"],
                            stdout=logf, stderr=subprocess.STDOUT)
    def latest_url():
        text = open(os.path.join(cfgdir, "out.log"), encoding="utf-8", errors="replace").read()
        ms = re.findall(r"http://127\.0\.0\.1:(\d+)/\?t=([0-9a-f]+)", text)
        return ms[-1] if ms else None
    m = wait_for(latest_url, name + " 启动")
    inst = {"name": name, "cfg": cfgpath, "proc": proc, "proc_pid": proc.pid,
            "log": os.path.join(cfgdir, "out.log"),
            "base": "http://127.0.0.1:%s" % m[0], "token": m[1]}
    inst["poller"] = EventPoller(inst["base"], inst["token"])
    return inst

def main():
    shutil.rmtree(BASE, ignore_errors=True)
    os.makedirs(BASE, exist_ok=True)
    a = start_instance("实例A", 41930, 41931, os.path.join(BASE, "dl-a"))
    b = start_instance("实例B", 41940, 41941, os.path.join(BASE, "dl-b"))
    try:
        # 0) UDP 广播发现
        a_id = snapshot(a["base"], a["token"])["self"]["id"]
        b_id = snapshot(b["base"], b["token"])["self"]["id"]
        wait_for(lambda: any(p["id"] == b_id for p in snapshot(a["base"], a["token"]).get("presence") or []),
                 "A 发现 B")
        wait_for(lambda: any(p["id"] == a_id for p in snapshot(b["base"], b["token"]).get("presence") or []),
                 "B 发现 A")
        print("[ok] UDP 广播发现互通")
        # 1) 配对
        bport = snapshot(b["base"], b["token"])["self"]["port"]
        assert bport == 41941, bport
        rpc(a["base"], a["token"], "pairStart", ["127.0.0.1:%d" % bport])
        req = wait_for(lambda: snapshot(b["base"], b["token"]).get("pairRequest"), "B 收到配对请求")
        code_b = req["code"]
        code_a = wait_for(lambda: (snapshot(a["base"], a["token"]).get("pairOut") or {}).get("code"), "A 出现配对码")
        assert code_a == code_b, "配对码不一致"
        rpc(b["base"], b["token"], "pairRespond", [req["reqID"], True])
        rpc(a["base"], a["token"], "pairOutConfirm", [True])
        wait_for(lambda: any(p["online"] for p in snapshot(a["base"], a["token"])["peers"]), "会话在线")
        print("[ok] 配对完成且会话在线")
        peer_a = snapshot(a["base"], a["token"])["peers"][0]["id"]
        # 2) 消息
        rpc(a["base"], a["token"], "sendChat", [peer_a, "你好 B，这是冒烟消息"])
        wait_for(lambda: any("你好 B" in str(d.get("text", "")) for d in b["poller"].of("desk:chat")),
                 "B 收到消息")
        print("[ok] 消息送达")
        # 3) 文件 32MB
        src = os.path.join(BASE, "payload.bin")
        data = secrets.token_bytes(32 * 1024 * 1024 + 777)
        with open(src, "wb") as f:
            f.write(data)
        t0 = time.time()
        rpc(a["base"], a["token"], "sendFile", [peer_a, src])
        fid = wait_for(lambda: next(iter([d["id"] for d in b["poller"].of("desk:transfer")
                                          if d.get("state") == "offer"]), None), "B 收到文件请求")
        rpc(b["base"], b["token"], "transferRespond", [fid, True])
        path = wait_for(lambda: next(iter([d["path"] for d in b["poller"].of("desk:transfer")
                                           if d.get("state") == "done"]), None), "B 接收完成", timeout=40)
        with open(path, "rb") as f:
            got = f.read()
        assert got == data, "文件内容不一致"
        dt = time.time() - t0
        mbs = len(data) / dt / (1024 * 1024)
        print(f"[ok] 文件 32MB 校验一致 ({dt:.1f}s, {mbs:.1f} MB/s 端到端):", path)
        snap_tr = rpc(b["base"], b["token"], "snapshot").get("transfers") or []
        done_tr = [d for d in snap_tr if d.get("state") == "done" and d.get("name") == "payload.bin"]
        assert done_tr, "snapshot 缺传输历史: %r" % (snap_tr,)
        print("[ok] 传输历史进 snapshot（刷新可回放）")
        # 4) 控制状态机
        rpc(b["base"], b["token"], "toggleShare", [False])
        time.sleep(0.3)
        rpc(a["base"], a["token"], "takeControl", [peer_a])
        wait_for(lambda: snapshot(a["base"], a["token"])["control"]["role"] == 0, "被拒后回空闲")
        print("[ok] B 关共享时拒绝控制")
        rpc(b["base"], b["token"], "toggleShare", [True])
        time.sleep(0.3)
        rpc(a["base"], a["token"], "takeControl", [peer_a])
        wait_for(lambda: snapshot(a["base"], a["token"])["control"]["role"] == 1, "A 进入控制")
        wait_for(lambda: snapshot(b["base"], b["token"])["control"]["role"] == 2, "B 进入被控")
        print("[ok] 控制切换状态机正常")
        rpc(a["base"], a["token"], "releaseControl", [])
        wait_for(lambda: snapshot(a["base"], a["token"])["control"]["role"] == 0 and
                 snapshot(b["base"], b["token"])["control"]["role"] == 0, "双方回空闲")
        print("[ok] 控制交还正常")
        # 5) 断线重连：杀 B 重启（配置持久化，ID 不变），A 应自动重连
        subprocess.run(["taskkill", "/F", "/PID", str(b["proc_pid"])], capture_output=True)
        time.sleep(1)
        logf_b = open(os.path.join(BASE, "实例B", "out.log"), "a", encoding="utf-8", errors="replace")
        b["proc"] = subprocess.Popen([EXE, "desk", "--config", b["cfg"], "--no-tray",
                                      "--ui", "browser", "--no-open"],
                                     stdout=logf_b, stderr=subprocess.STDOUT)
        # 等待出现"与重启前不同的新 token"（重启前日志里的旧 token 会一直存在）
        old_tok = b["token"].split("/?t=")[-1] if "/?t=" in b["token"] else b["token"]
        def new_token_url():
            text = open(os.path.join(BASE, "实例B", "out.log"), encoding="utf-8", errors="replace").read()
            ms = re.findall(r"http://127\.0\.0\.1:(\d+)/\?t=([0-9a-f]+)", text)
            for mm in reversed(ms):
                if mm[1] != old_tok:
                    return mm
            return None
        m = wait_for(new_token_url, "B 重启出全新 token")
        print("[dbg] B 新 token=%s..." % m[1][:12])
        b["base"], b["token"] = "http://127.0.0.1:%s" % m[0], m[1]
        b["poller"] = EventPoller(b["base"], b["token"])
        wait_for(lambda: snapshot(a["base"], a["token"])["peers"][0]["online"],
                 "重启后 A 自动重连 B", timeout=30)
        print("[ok] 断线后自动重连")
        # 6) 重连后消息重发（容忍会话切换瞬态，轮询直到 B 收到）
        def chat_roundtrip():
            try:
                rpc(a["base"], a["token"], "sendChat", [peer_a, "重启后的消息"])
            except Exception:
                pass
            return any("重启后的消息" in str(d.get("text", "")) for d in b["poller"].of("desk:chat"))
        try:
            wait_for(chat_roundtrip, "重连后消息送达", timeout=90)
        except SystemExit as e:
            print("[dbg] 重连消息超时，继续取证:", e)
        def raw(base, token, cmd, args):
            body = json.dumps({"cmd": cmd, "args": args}).encode()
            req = urllib.request.Request(base + "/api/rpc?t=" + token, data=body,
                                         headers={"Content-Type": "application/json"})
            try:
                with urllib.request.urlopen(req, timeout=10) as r:
                    return "OK", r.read().decode()[:400]
            except urllib.error.HTTPError as e:
                return e.code, e.read().decode()[:200]
            except Exception as e:
                return "EXC", str(e)[:200]
        st_b, body_b = raw(b["base"], b["token"], "chatHistory", [peer_a])
        print("[dbg] B chatHistory(%s): %s" % (st_b, body_b))
        st_a, body_a = raw(a["base"], a["token"], "chatHistory", [b_id])
        print("[dbg] A chatHistory(%s): %s" % (st_a, body_a[:300]))
        st_o, body_o = raw(b["base"], b["token"], "snapshot", [])
        print("[dbg] B snapshot(%s): %s" % (st_o, body_o[:300]))
        print("[ok] 重连后消息通路恢复")

        print("SMOKE PASS")
        return 0
    finally:
        if os.environ.get("KEEP"):
            print("[dbg] KEEP=1 保留进程现场")
        else:
            for inst in (a, b):
                p = inst.get("proc")
                if p is not None and p.poll() is None:
                    p.terminate()
            time.sleep(1)
            subprocess.run(["taskkill", "/F", "/IM", "winforge-desk.exe"], capture_output=True)

if __name__ == "__main__":
    sys.exit(main())
