# -*- coding: utf-8 -*-
"""给 README 生成控制台截图：双实例配对 + 消息 + 文件传输后，无头 Chrome 截 A 的控制台。"""
import json, os, re, secrets, shutil, subprocess, threading, time, urllib.request

BASE = r"C:\Users\williamwong\AppData\Local\Temp\wfdesk-shot"
CHROME = r"C:\Program Files\Google\Chrome\Application\chrome.exe"
EXE = os.environ.get("WFDESK_EXE", r"C:\Users\williamwong\AppData\Local\Temp\winforge-desk.exe")

def rpc(base, token, cmd, args=None):
    body = json.dumps({"cmd": cmd, "args": args or []}).encode()
    req = urllib.request.Request(base + "/api/rpc?t=" + token, data=body,
                                 headers={"Content-Type": "application/json"})
    with urllib.request.urlopen(req, timeout=10) as r:
        return json.loads(r.read().decode())

def wait_for(fn, what, timeout=20):
    """轮询直到 fn() 返回真值并返回该值。"""
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

class EventPoller:
    """持续长轮询事件流。"""
    def __init__(self, base, token):
        self.base, self.token, self.events = base, token, []
        threading.Thread(target=self.loop, daemon=True).start()
    def loop(self):
        while True:
            try:
                req = urllib.request.Request(self.base + "/api/events?t=" + self.token)
                with urllib.request.urlopen(req, timeout=35) as r:
                    batch = json.loads(r.read().decode() or "[]")
                self.events.extend(batch or [])
            except Exception:
                time.sleep(0.3)
    def of(self, name):
        return [e["data"] for e in self.events if e["event"] == name]

def start_instance(name, ui_port, ctl_port, downloads):
    os.makedirs(downloads, exist_ok=True)
    cfgdir = os.path.join(BASE, name)
    os.makedirs(cfgdir, exist_ok=True)
    cfgpath = os.path.join(cfgdir, "desk.json")
    with open(cfgpath, "w", encoding="utf-8") as f:
        json.dump({"id": secrets.token_hex(16), "name": name,
                   "ui_port": ui_port, "control_port": ctl_port, "discovery_port": 0,
                   "hotkey": "Ctrl+Alt+D", "downloads_dir": downloads,
                   "max_file_bytes": 104857600, "share_enabled": True, "peers": []}, f)
    logf = open(os.path.join(cfgdir, "out.log"), "w", encoding="utf-8", errors="replace")
    subprocess.Popen([EXE, "desk", "--config", cfgpath, "--no-tray", "--ui", "browser", "--no-open"],
                     stdout=logf, stderr=subprocess.STDOUT)
    def find_url():
        text = open(os.path.join(cfgdir, "out.log"), encoding="utf-8", errors="replace").read()
        return re.search(r"http://127\.0\.0\.1:(\d+)/\?t=([0-9a-f]+)", text)
    m = wait_for(find_url, name + " 启动")
    inst = {"base": "http://127.0.0.1:%s" % m.group(1), "token": m.group(2)}
    inst["poller"] = EventPoller(inst["base"], inst["token"])
    return inst

def shot(url, out):
    subprocess.run([CHROME, "--headless=new", "--disable-gpu",
                    "--virtual-time-budget=9000", "--window-size=1180,780",
                    "--screenshot=" + os.path.abspath(out), url],
                   capture_output=True, timeout=60)
    if not os.path.exists(out):
        raise SystemExit("截图未生成: " + out)
    print("[ok] 截图:", out, os.path.getsize(out), "bytes")

def main():
    shutil.rmtree(BASE, ignore_errors=True)
    os.makedirs(BASE, exist_ok=True)
    os.makedirs("docs", exist_ok=True)
    a = start_instance("书房-主机", 41960, 41961, os.path.join(BASE, "dl-a"))
    b = start_instance("客厅-迷你机", 41970, 41971, os.path.join(BASE, "dl-b"))
    try:
        bport = rpc(b["base"], b["token"], "snapshot")["self"]["port"]
        rpc(a["base"], a["token"], "pairStart", ["127.0.0.1:%d" % bport])
        req = wait_for(lambda: rpc(b["base"], b["token"], "snapshot").get("pairRequest"), "配对请求")
        rpc(b["base"], b["token"], "pairRespond", [req["reqID"], True])
        rpc(a["base"], a["token"], "pairOutConfirm", [True])
        wait_for(lambda: rpc(a["base"], a["token"], "snapshot")["peers"] and rpc(a["base"], a["token"], "snapshot")["peers"][0]["online"],
                 "会话在线")
        peer = rpc(a["base"], a["token"], "snapshot")["peers"][0]["id"]
        a_peer = rpc(b["base"], b["token"], "snapshot")["peers"][0]["id"]
        rpc(a["base"], a["token"], "sendChat", [peer, "把固件传给你了，直接在下载目录里"])
        time.sleep(0.4)
        rpc(b["base"], b["token"], "sendChat", [a_peer, "收到，已经在测了 👌"])
        time.sleep(0.4)
        rpc(a["base"], a["token"], "sendChat", [peer, "把鼠标推过右边缘就能直接控制这台"])
        src = os.path.join(BASE, "firmware-v2.3.bin")
        with open(src, "wb") as f:
            f.write(secrets.token_bytes(5 * 1024 * 1024))
        rpc(a["base"], a["token"], "sendFile", [peer, src])
        fid = wait_for(lambda: next(iter([d["id"] for d in b["poller"].of("desk:transfer")
                                          if d.get("state") == "offer"]), None), "offer")
        rpc(b["base"], b["token"], "transferRespond", [fid, True])
        wait_for(lambda: any(d.get("state") == "done" for d in b["poller"].of("desk:transfer")), "done")
        print("[ok] 状态就绪，开始截图")
        shot(a["base"] + "/?t=" + a["token"] + "&tab=share", "docs/console.png")
        shot(a["base"] + "/?t=" + a["token"] + "&tab=transfer", "docs/console-transfer.png")
    finally:
        subprocess.run(["taskkill", "/F", "/IM", "winforge-desk.exe"], capture_output=True)

if __name__ == "__main__":
    main()
