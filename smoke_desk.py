# -*- coding: utf-8 -*-
"""WinForge Desk 双实例端到端冒烟：发现/配对 → 消息 → 文件传输 → 控制状态机。"""
import json, os, re, subprocess, sys, time, urllib.request, urllib.error, secrets, shutil, threading

EXE = os.environ.get("WFDESK_EXE", r"C:\Users\williamwong\AppData\Local\Temp\winforge-desk.exe")
BASE = r"C:\Users\williamwong\AppData\Local\Temp\wfdesk-smoke"

def rpc(base, token, cmd, args=None):
    body = json.dumps({"cmd": cmd, "args": args or []}).encode()
    req = urllib.request.Request(base + "/api/rpc?t=" + token, data=body,
                                 headers={"Content-Type": "application/json"})
    with urllib.request.urlopen(req, timeout=10) as r:
        return json.loads(r.read().decode())

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

def start_instance(name, ui_port, ctl_port, downloads):
    os.makedirs(downloads, exist_ok=True)
    cfgdir = os.path.join(BASE, name)
    os.makedirs(cfgdir, exist_ok=True)
    cfgpath = os.path.join(cfgdir, "desk.json")
    cfg = {
        "id": secrets.token_hex(16), "name": name,
        "ui_port": ui_port, "control_port": ctl_port, "discovery_port": 0,
        "hotkey": "Ctrl+Alt+D", "downloads_dir": downloads,
        "max_file_bytes": 104857600, "share_enabled": True, "peers": [],
    }
    with open(cfgpath, "w", encoding="utf-8") as f:
        json.dump(cfg, f)
    logf = open(os.path.join(cfgdir, "out.log"), "w", encoding="utf-8", errors="replace")
    p = subprocess.Popen([EXE, "desk", "--config", cfgpath, "--no-tray", "--ui", "browser", "--no-open"],
                         stdout=logf, stderr=subprocess.STDOUT)
    # 从日志里抓控制台地址（含 token）
    token = wait_for(lambda: (re.search(r"控制台: http://127\.0\.0\.1:(\d+)/\?t=([0-9a-f]+)",
                                        open(os.path.join(cfgdir, "out.log"), encoding="utf-8", errors="replace").read()) or None),
                     name + " 启动")
    inst = {"name": name, "proc": p, "cfg": cfgpath, "log": os.path.join(cfgdir, "out.log"),
            "base": "http://127.0.0.1:%s" % token.group(1), "token": token.group(2)}
    inst["poller"] = EventPoller(inst["base"], inst["token"])
    return inst

def main():
    shutil.rmtree(BASE, ignore_errors=True)
    os.makedirs(BASE, exist_ok=True)
    a = start_instance("实例A", 41930, 41931, os.path.join(BASE, "dl-a"))
    b = start_instance("实例B", 41940, 41941, os.path.join(BASE, "dl-b"))
    print("[ok] 两实例已启动", a["base"], b["base"])
    try:
        # 1) 配对：A 发起 → B 出请求 → 双确认
        bport = snapshot(b["base"], b["token"])["self"]["port"]
        assert bport == 41941, bport
        rpc(a["base"], a["token"], "pairStart", ["127.0.0.1:%d" % bport])
        req = wait_for(lambda: snapshot(b["base"], b["token"]).get("pairRequest"), "B 收到配对请求")
        code_b = req["code"]
        code_a = wait_for(lambda: (snapshot(a["base"], a["token"]).get("pairOut") or {}).get("code"), "A 出现配对码")
        assert code_a == code_b, "配对码不一致 %s != %s" % (code_a, code_b)
        print("[ok] 配对码一致:", code_a)
        rpc(b["base"], b["token"], "pairRespond", [req["reqID"], True])
        rpc(a["base"], a["token"], "pairOutConfirm", [True])
        wait_for(lambda: len(snapshot(a["base"], a["token"])["peers"]) == 1, "A 保存配对")
        wait_for(lambda: len(snapshot(b["base"], b["token"])["peers"]) == 1, "B 保存配对")
        wait_for(lambda: snapshot(a["base"], a["token"])["peers"][0]["online"], "会话在线")
        print("[ok] 配对完成且会话在线")
        peer_a = snapshot(a["base"], a["token"])["peers"][0]["id"]

        # 2) 消息
        rpc(a["base"], a["token"], "sendChat", [peer_a, "你好 B，这是冒烟消息"])
        wait_for(lambda: any(str(d.get("text", "")).startswith("你好 B") for d in b["poller"].of("desk:chat")),
                 "B 收到消息")
        print("[ok] 消息送达")

        # 3) 文件（3.5MB 随机数据）
        src = os.path.join(BASE, "payload.bin")
        data = secrets.token_bytes(3 * 1024 * 1024 + 777)
        with open(src, "wb") as f:
            f.write(data)
        rpc(a["base"], a["token"], "sendFile", [peer_a, src])
        fid = wait_for(lambda: next(iter([d["id"] for d in b["poller"].of("desk:transfer")
                                          if d.get("state") == "offer"]), None), "B 收到文件请求")
        rpc(b["base"], b["token"], "transferRespond", [fid, True])
        path = wait_for(lambda: next(iter([d["path"] for d in b["poller"].of("desk:transfer")
                                           if d.get("state") == "done"]), None), "B 接收完成", timeout=30)
        with open(path, "rb") as f:
            got = f.read()
        assert got == data, "内容不一致 %d vs %d" % (len(got), len(data))
        print("[ok] 文件 3MB 校验一致:", path)

        # 4) 控制状态机（无真实键鼠，走 take/deny 语义）
        # B 关共享 → A take 应被拒
        rpc(b["base"], b["token"], "toggleShare", [False])
        time.sleep(0.3)
        r = rpc(a["base"], a["token"], "takeControl", [peer_a])
        wait_for(lambda: snapshot(a["base"], a["token"])["control"]["role"] == 0, "被拒后回空闲")
        print("[ok] B 关共享时拒绝控制")
        rpc(b["base"], b["token"], "toggleShare", [True])
        time.sleep(0.3)
        rpc(a["base"], a["token"], "takeControl", [peer_a])
        wait_for(lambda: snapshot(a["base"], a["token"])["control"]["role"] == 1, "A 进入控制")
        wait_for(lambda: snapshot(b["base"], b["token"])["control"]["role"] == 2, "B 进入被控")
        print("[ok] 控制切换状态机正常")
        rpc(a["base"], a["token"], "releaseControl", [])
        wait_for(lambda: snapshot(a["base"], a["token"])["control"]["role"] == 0, "A 回空闲")
        wait_for(lambda: snapshot(b["base"], b["token"])["control"]["role"] == 0, "B 回空闲")
        print("[ok] 控制交还正常")

        print("SMOKE PASS")
        return 0
    finally:
        for inst in (a, b):
            inst["proc"].terminate()
        time.sleep(1)
        for inst in (a, b):
            if inst["proc"].poll() is None:
                inst["proc"].kill()

if __name__ == "__main__":
    sys.exit(main())
