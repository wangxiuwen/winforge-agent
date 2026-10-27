# -*- coding: utf-8 -*-
"""WinForge Desk 长稳 soak：N 轮（双向消息 + 1MB 文件 + 开关共享）循环。
本地手动工具，不进 CI——冒烟（smoke_desk.py）走单轮快速门禁，本脚本抓
长尾问题：句柄/内存缓涨、退避状态残留、配对目录膨胀等。
用法：py -3 soak_desk.py [轮数=10]；exe 用 WFDESK_EXE 指定，默认现构建。"""
import json, os, re, secrets, subprocess, sys, threading, time, urllib.request, urllib.error

EXE = os.environ.get("WFDESK_EXE", r"C:\Users\williamwong\AppData\Local\Temp\winforge-desk.exe")
BASE = r"C:\Users\williamwong\AppData\Local\Temp\wfdesk-soak"


def rpc(base, token, cmd, args=None):
    body = json.dumps({"cmd": cmd, "args": args or []}).encode()
    req = urllib.request.Request(base + "/api/rpc?t=" + token, data=body,
                                 headers={"Content-Type": "application/json"})
    with urllib.request.urlopen(req, timeout=10) as r:
        return json.loads(r.read().decode())


class EventPoller:
    def __init__(self, base, token):
        self.base, self.token, self.events = base, token, []
        self.last_seq = 0
        self.lock = threading.Lock()
        threading.Thread(target=self.loop, daemon=True).start()

    def loop(self):
        while True:
            try:
                url = self.base + "/api/events?t=" + self.token
                if self.last_seq > 0:
                    url += "&after=%d" % self.last_seq
                req = urllib.request.Request(url)
                with urllib.request.urlopen(req, timeout=35) as r:
                    batch = json.loads(r.read().decode() or "[]")
                with self.lock:
                    self.events.extend(batch or [])
                    for ev in batch or []:
                        self.last_seq = max(self.last_seq, ev.get("seq", 0))
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


def start_instance(name, ui_port, ctl_port):
    cfgdir = os.path.join(BASE, name)
    os.makedirs(cfgdir, exist_ok=True)
    dl = os.path.join(BASE, "dl-" + name)
    os.makedirs(dl, exist_ok=True)
    cfgpath = os.path.join(cfgdir, "desk.json")
    with open(cfgpath, "w", encoding="utf-8") as f:
        json.dump({"id": secrets.token_hex(16), "name": name,
                   "ui_port": ui_port, "control_port": ctl_port, "discovery_port": 0,
                   "hotkey": "Ctrl+Alt+D", "downloads_dir": dl,
                   "max_file_bytes": 104857600, "share_enabled": True, "peers": []}, f)
    logf = open(os.path.join(cfgdir, "out.log"), "w", encoding="utf-8", errors="replace")
    subprocess.Popen([EXE, "desk", "--config", cfgpath, "--no-tray", "--ui", "browser", "--no-open"],
                     stdout=logf, stderr=subprocess.STDOUT)

    def find_url():
        text = open(os.path.join(cfgdir, "out.log"), encoding="utf-8", errors="replace").read()
        m = re.findall(r"http://127\.0\.0\.1:(\d+)/\?t=([0-9a-f]+)", text)
        return m[-1] if m else None
    m = wait_for(find_url, name + " 启动")
    inst = {"base": "http://127.0.0.1:%s" % m[0], "token": m[1], "name": name, "dl": dl}
    inst["poller"] = EventPoller(inst["base"], inst["token"])
    return inst


def mem_mb(image):
    out = subprocess.run(["tasklist", "/FI", "IMAGENAME eq " + image, "/FO", "CSV"],
                         capture_output=True).stdout.decode("utf-8", errors="replace")
    total = 0
    for line in out.splitlines():
        cols = [c.strip().strip('"').replace(",", "").replace(" K", "").strip()
                for c in line.split('","')]
        nums = [int(c) for c in cols if c.isdigit()]
        if nums:
            total += nums[-1]  # 行内最后一个整数列是内存(K)
    return total / 1024


def main():
    cycles = int(sys.argv[1]) if len(sys.argv) > 1 else 10
    subprocess.run(["taskkill", "/F", "/IM", "winforge-desk.exe"], capture_output=True)
    time.sleep(1)
    import shutil
    shutil.rmtree(BASE, ignore_errors=True)
    os.makedirs(BASE, exist_ok=True)
    mem0 = mem_mb("winforge-desk.exe")
    a = start_instance("soak-A", 41990, 41991)
    b = start_instance("soak-B", 41992, 41993)
    t0 = time.time()
    try:
        bport = rpc(b["base"], b["token"], "snapshot")["self"]["port"]
        rpc(a["base"], a["token"], "pairStart", ["127.0.0.1:%d" % bport])
        req = wait_for(lambda: rpc(b["base"], b["token"], "snapshot").get("pairRequest"), "配对请求")
        rpc(b["base"], b["token"], "pairRespond", [req["reqID"], True])
        rpc(a["base"], a["token"], "pairOutConfirm", [True])
        wait_for(lambda: rpc(a["base"], a["token"], "snapshot")["peers"][0]["online"], "会话在线")
        peer_a = rpc(a["base"], a["token"], "snapshot")["peers"][0]["id"]
        peer_b = rpc(b["base"], b["token"], "snapshot")["peers"][0]["id"]
        print("[ok] 配对完成，开始 %d 轮 soak" % cycles)

        done_fids = set()
        for i in range(1, cycles + 1):
            c0 = time.time()
            rpc(a["base"], a["token"], "sendChat", [peer_a, "soak %d A2B" % i])
            rpc(b["base"], b["token"], "sendChat", [peer_b, "soak %d B2A" % i])
            wait_for(lambda: any(d.get("text") == "soak %d B2A" % i for d in a["poller"].of("desk:chat")),
                     "%d: B2A 送达" % i)
            wait_for(lambda: any(d.get("text") == "soak %d A2B" % i for d in b["poller"].of("desk:chat")),
                     "%d: A2B 送达" % i)
            data = secrets.token_bytes(1024 * 1024 + i)
            src = os.path.join(BASE, "soak-%d.bin" % i)
            with open(src, "wb") as f:
                f.write(data)
            rpc(a["base"], a["token"], "sendFile", [peer_a, src])
            fid = wait_for(lambda: next(iter([d["id"] for d in b["poller"].of("desk:transfer")
                                              if d.get("state") == "offer" and d["id"] not in done_fids]), None),
                           "%d: offer" % i)
            done_fids.add(fid)
            rpc(b["base"], b["token"], "transferRespond", [fid, True])
            wait_for(lambda: any(d.get("id") == fid and d.get("state") == "done"
                                 for d in b["poller"].of("desk:transfer")), "%d: done" % i, timeout=30)
            rpc(b["base"], b["token"], "toggleShare", [False])
            time.sleep(0.3)
            rpc(b["base"], b["token"], "toggleShare", [True])
            time.sleep(0.3)
            print("[ok] 第 %d/%d 轮 (%.1fs)" % (i, cycles, time.time() - c0))
        mem1 = mem_mb("winforge-desk.exe")
        print("[ok] soak PASS: %d 轮共 %.0fs, 进程内存 %.1fMB → %.1fMB"
              % (cycles, time.time() - t0, mem0, mem1))
    finally:
        subprocess.run(["taskkill", "/F", "/IM", "winforge-desk.exe"], capture_output=True)


if __name__ == "__main__":
    main()
