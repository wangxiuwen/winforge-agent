// WinForge Desk 控制台。同时适配两种宿主：
//   Wails 桌面窗口（window.go 存在，走绑定 + EventsOn）
//   浏览器模式（走 /api/rpc + WebSocket）
"use strict";

// ---------- 桥接 ----------
function makeBridge() {
  if (window.go && window.go.main && window.go.main.Desk) {
    const b = window.go.main.Desk;
    const rt = window.runtime;
    return {
      mode: "wails",
      snapshot: () => b.Snapshot(),
      call: (cmd, ...args) => {
        const map = {
          pairStart: b.PairStart, pairRespond: b.PairRespond, pairOutConfirm: b.PairOutConfirm,
          peerPosition: b.PeerPosition, peerToggle: b.PeerToggle, peerRemove: b.PeerRemove,
          sendChat: b.SendChat, sendFile: b.SendFile, pickAndSendFile: b.PickAndSendFile,
          transferRespond: b.TransferRespond, takeControl: b.TakeControl, releaseControl: b.ReleaseControl,
          toggleShare: b.ToggleShare, openPath: b.OpenPath,
        };
        const fn = map[cmd];
        if (!fn) return Promise.reject(new Error("未知命令 " + cmd));
        return fn(...args);
      },
      on: (event, cb) => {
        rt.EventsOn(event, (data) => cb(data));
      },
      pickFile: () => Promise.reject(new Error("由 Go 侧对话框处理")),
    };
  }
  const token = new URLSearchParams(location.search).get("t") || sessionStorage.getItem("wfdesk-token") || "";
  sessionStorage.setItem("wfdesk-token", token);
  async function rpc(cmd, args) {
    const r = await fetch("/api/rpc?t=" + encodeURIComponent(token), {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ cmd, args: args || [] }),
    });
    if (!r.ok) throw new Error(await r.text());
    return r.json();
  }
  return {
    mode: "http",
    snapshot: () => rpc("snapshot", []),
    call: (cmd, ...args) => rpc(cmd, args),
    on: (event, cb) => {
      const ws = new WebSocket(
        (location.protocol === "https:" ? "wss://" : "ws://") + location.host + "/ws?t=" + encodeURIComponent(token));
      ws.onmessage = (e) => {
        try {
          const m = JSON.parse(e.data);
          if (m.event === event) cb(m.data);
        } catch (_) {}
      };
      ws.onclose = () => setTimeout(() => bridge.on(event, cb), 1500);
    },
    pickFile: () => new Promise((resolve) => {
      const inp = document.getElementById("file-input");
      inp.value = "";
      inp.onchange = () => resolve(inp.files.length ? inp.files[0] : null);
      inp.click();
    }),
    upload: (file, peer) => {
      const fd = new FormData();
      fd.append("file", file);
      return fetch("/api/upload?peer=" + encodeURIComponent(peer) + "&t=" + encodeURIComponent(token),
        { method: "POST", body: fd }).then(r => r.ok ? r.json() : Promise.reject(new Error(r.status)));
    },
  };
}
const bridge = makeBridge();

// ---------- 状态 ----------
let S = { peers: [], presence: [], control: { role: 0 } };
let chatPeer = "";
const chatLog = []; // {from, text, ts, outgoing}

const $ = (id) => document.getElementById(id);
function esc(s) {
  const d = document.createElement("div");
  d.textContent = String(s == null ? "" : s);
  return d.innerHTML;
}

function toast(level, text) {
  const t = document.createElement("div");
  t.className = "toast " + (level || "info");
  t.textContent = text;
  $("toasts").appendChild(t);
  setTimeout(() => t.remove(), 5200);
}

function fmtSize(n) {
  if (n > 1 << 30) return (n / (1 << 30)).toFixed(1) + " GB";
  if (n > 1 << 20) return (n / (1 << 20)).toFixed(1) + " MB";
  if (n > 1024) return (n / 1024).toFixed(0) + " KB";
  return n + " B";
}
function fmtTime(ts) {
  const d = new Date(ts);
  return d.getHours().toString().padStart(2, "0") + ":" + d.getMinutes().toString().padStart(2, "0");
}

// ---------- 渲染 ----------
function render() {
  $("self-name").textContent = S.self ? S.self.name : "";
  const hotkey = (S.self && S.self.hotkey) || "Ctrl+Alt+D";
  $("hotkey-hint").textContent = "切换热键 " + hotkey;
  $("hotkey-inline").textContent = hotkey;
  $("hotkey-inline2").textContent = hotkey;
  $("share-toggle").checked = !!S.share;

  const ctl = S.control || { role: 0 };
  const st = $("ctl-status");
  st.className = "ctl-status" + (ctl.role === 1 ? " ctrl" : ctl.role === 2 ? " victim" : "");
  st.textContent = ctl.role === 1 ? "正在控制 " + ctl.peer_name : ctl.role === 2 ? ctl.peer_name + " 正在控制本机" : "空闲";
  const big = $("control-big");
  big.className = "control-big" + (ctl.role === 1 ? " ctrl" : ctl.role === 2 ? " victim" : "");
  big.textContent = ctl.role === 1 ? "鼠标键盘正在控制「" + ctl.peer_name + "」"
    : ctl.role === 2 ? "「" + ctl.peer_name + "」正在控制本机" : "空闲 —— 通过边缘推挤、热键或下方按钮开始";

  renderPresence();
  renderPaired();
  renderControl();
  renderChatSelect();
  renderChat();
  renderTransfers();
}

function renderPresence() {
  const box = $("presence-list");
  const known = new Set(S.peers.map(p => p.id));
  const list = (S.presence || []).filter(p => p.id && !known.has(p.id));
  if (!list.length) {
    box.innerHTML = '<div class="card"><span class="meta">正在扫描局域网…未发现设备时确认双方都在运行且端口一致（默认 41832/UDP）</span></div>';
    return;
  }
  box.innerHTML = "";
  for (const p of list) {
    const c = document.createElement("div");
    c.className = "card";
    c.innerHTML = `<span class="dot on"></span><span class="name">${esc(p.name)}</span>
      <span class="meta">${esc(p.addr)}${p.port ? ":" + p.port : ""}</span><span class="grow"></span>
      <button data-act="pair" data-addr="${esc(p.addr)}${p.port ? ":" + p.port : ""}">配对</button>`;
    box.appendChild(c);
  }
}

function renderPaired() {
  const box = $("paired-list");
  box.innerHTML = "";
  if (!S.peers.length) {
    box.innerHTML = '<div class="card"><span class="meta">还没有配对设备</span></div>';
    return;
  }
  for (const p of S.peers) {
    const c = document.createElement("div");
    c.className = "card";
    c.innerHTML = `<span class="dot ${p.online ? "on" : ""}"></span><span class="name">${esc(p.name)}</span>
      <span class="meta">${p.online ? "在线" : "离线"}${p.addr ? " · " + esc(p.addr) : ""}</span><span class="grow"></span>
      <span class="pos-group">
        <button data-act="pos" data-id="${p.id}" data-pos="left" class="${p.position === "left" ? "on" : ""}">← 左</button>
        <button data-act="pos" data-id="${p.id}" data-pos="" class="${!p.position ? "on" : ""}">·</button>
        <button data-act="pos" data-id="${p.id}" data-pos="right" class="${p.position === "right" ? "on" : ""}">右 →</button>
      </span>
      <button data-act="ctl" data-id="${p.id}" ${p.online ? "" : "disabled"}>控制</button>
      <button class="alt" data-act="toggle" data-id="${p.id}">${p.enabled ? "停用" : "启用"}</button>
      <button class="danger" data-act="remove" data-id="${p.id}">删除</button>`;
    box.appendChild(c);
  }
}

function renderControl() {
  const box = $("control-peers");
  box.innerHTML = "";
  for (const p of (S.peers || [])) {
    if (!p.enabled) continue;
    const c = document.createElement("div");
    c.className = "card";
    c.innerHTML = `<span class="dot ${p.online ? "on" : ""}"></span><span class="name">${esc(p.name)}</span>
      <span class="meta">${esc(p.position === "left" ? "在左边" : p.position === "right" ? "在右边" : "未设方位")}</span>
      <span class="grow"></span>
      <button data-act="ctl" data-id="${p.id}" ${p.online && (!S.control || S.control.role === 0) ? "" : "disabled"}>控制这台</button>
      <button class="alt" data-act="unctl" ${S.control && S.control.role !== 0 ? "" : "disabled"}>结束共享</button>`;
    box.appendChild(c);
  }
  if (!(S.peers || []).some(p => p.enabled)) {
    box.innerHTML = '<div class="card"><span class="meta">先在"共享"页配对设备</span></div>';
  }
}

function renderChatSelect() {
  const sel = $("chat-peer");
  const cur = sel.value;
  sel.innerHTML = "";
  for (const p of S.peers) {
    const o = document.createElement("option");
    o.value = p.id;
    o.textContent = p.name + (p.online ? "" : "（离线）");
    sel.appendChild(o);
  }
  if (cur) sel.value = cur;
  chatPeer = sel.value || chatPeer;
}

function renderChat() {
  const box = $("chat-log");
  box.innerHTML = chatLog.map(m =>
    `<div class="msg ${m.outgoing ? "out" : ""}"><div class="who">${esc(m.from)} · ${fmtTime(m.ts)}</div>${esc(m.text)}</div>`
  ).join("");
  box.scrollTop = box.scrollHeight;
}

const transfers = new Map(); // id -> view
function renderTransfers() {
  const box = $("transfer-list");
  box.innerHTML = "";
  for (const [id, t] of transfers) {
    const pct = t.size ? Math.min(100, Math.round((t.recv || 0) * 100 / t.size)) : 0;
    const el = document.createElement("div");
    el.className = "tr-item";
    const stateText = { offer: "等待你确认", accepted: "已接受", receiving: "接收中", sending: "发送中",
      sent: "已发出", done: "已完成", failed: "失败", rejected: "被拒收" }[t.state] || t.state;
    el.innerHTML = `<div class="row">
        <span class="name">${t.dir === "in" ? "↓ " : "↑ "}${esc(t.name || "")}</span>
        <span class="grow"></span><span class="state ${t.state}">${stateText} ${t.size ? fmtSize(t.recv || 0) + "/" + fmtSize(t.size) : ""}</span>
      </div>
      <div class="bar"><div style="width:${pct}%"></div></div>
      <div class="row" style="margin-top:6px">
        <span class="grow"></span>
        ${t.state === "offer" ? `<button data-act="tr-yes" data-id="${id}">接收</button>
          <button class="danger" data-act="tr-no" data-id="${id}">拒收</button>` : ""}
        ${t.state === "done" && t.path ? `<button class="alt" data-act="open" data-path="${esc(t.path)}">打开</button>` : ""}
      </div>`;
    box.appendChild(el);
  }
}

// ---------- 事件 ----------
document.querySelectorAll("nav .tab").forEach(b => b.onclick = () => {
  document.querySelectorAll("nav .tab").forEach(x => x.classList.remove("active"));
  document.querySelectorAll(".tab-page").forEach(x => x.classList.remove("active"));
  b.classList.add("active");
  $("tab-" + b.dataset.tab).classList.add("active");
});

document.body.addEventListener("click", (e) => {
  const btn = e.target.closest("button[data-act]");
  if (!btn) return;
  const act = btn.dataset.act;
  const id = btn.dataset.id;
  if (act === "pair") bridge.call("pairStart", btn.dataset.addr).catch(err => toast("error", String(err)));
  if (act === "pos") bridge.call("peerPosition", id, btn.dataset.pos).catch(() => {});
  if (act === "toggle") {
    const p = S.peers.find(x => x.id === id);
    if (p) bridge.call("peerToggle", id, !p.enabled).catch(() => {});
  }
  if (act === "remove") { if (confirm("删除该配对？双方都需要重新配对。")) bridge.call("peerRemove", id).catch(() => {}); }
  if (act === "ctl") bridge.call("takeControl", id).then(r => { if (r && r.error) toast("warn", r.error); }).catch(err => toast("warn", String(err)));
  if (act === "unctl") bridge.call("releaseControl").catch(() => {});
  if (act === "tr-yes") bridge.call("transferRespond", id, true).catch(() => {});
  if (act === "tr-no") bridge.call("transferRespond", id, false).catch(() => {});
  if (act === "open") bridge.call("openPath", btn.dataset.path).catch(err => toast("error", String(err)));
});

$("share-toggle").onchange = (e) => bridge.call("toggleShare", e.target.checked).catch(() => {});
$("chat-send").onclick = sendChat;
$("chat-text").addEventListener("keydown", (e) => { if (e.key === "Enter") sendChat(); });
function sendChat() {
  const v = $("chat-text").value.trim();
  if (!v || !chatPeer) return;
  bridge.call("sendChat", chatPeer, v)
    .then((r) => { if (r && r.error) toast("warn", r.error); $("chat-text").value = ""; })
    .catch(err => toast("warn", String(err)));
}

$("file-send").onclick = () => {
  const path = $("file-path").value.trim();
  if (!path || !chatPeer) return;
  bridge.call("sendFile", chatPeer, path).then(r => {
    if (r && r.error) toast("warn", r.error); else $("file-path").value = "";
  }).catch(err => toast("error", String(err)));
};
$("file-pick").onclick = async () => {
  if (bridge.mode === "wails") {
    bridge.call("pickAndSendFile", chatPeer).catch(err => toast("error", String(err)));
    return;
  }
  const f = await bridge.pickFile();
  if (f && chatPeer) bridge.upload(f, chatPeer).then(r => {
    if (r && r.error) toast("error", r.error);
  }).catch(err => toast("error", String(err)));
};
$("open-downloads").onclick = () => bridge.call("openPath", "").catch(err => toast("error", String(err)));

// ---------- 服务端/宿主事件 ----------
bridge.on("desk:state", (s) => { S = s; render(); });
bridge.on("desk:toast", (t) => toast(t.level, t.text));
bridge.on("desk:chat", (m) => { chatLog.push(m); if (chatLog.length > 500) chatLog.shift(); renderChat(); });
bridge.on("desk:transfer", (t) => {
  const cur = transfers.get(t.id) || {};
  transfers.set(t.id, Object.assign(cur, t));
  renderTransfers();
});
bridge.on("desk:pair", (p) => renderPairModal(p));
bridge.on("desk:control", (c) => { if (S) { S.control = c; render(); } });

// ---------- 配对弹窗 ----------
function showModal(title, bodyHTML, actions) {
  $("pair-title").textContent = title;
  $("pair-body").innerHTML = bodyHTML;
  const box = $("pair-actions");
  box.innerHTML = "";
  for (const a of actions) {
    const b = document.createElement("button");
    b.textContent = a.text;
    b.className = a.cls || "";
    b.onclick = a.fn;
    box.appendChild(b);
  }
  $("pair-modal").classList.remove("hidden");
}
function hideModal() { $("pair-modal").classList.add("hidden"); }

function renderPairModal(p) {
  if (p.state === "request") {
    showModal("收到配对请求",
      `<p><b>${esc(p.message.split(" 请求配对")[0] || "对方")}</b> 想与本机配对。</p>
       <p class="hint">请核对对方屏幕上的配对码与本机一致：</p>
       <div class="pair-code">${esc(p.code)}</div>`,
      [
        { text: "拒绝", cls: "danger", fn: () => { bridge.call("pairRespond", p.reqID, false); hideModal(); } },
        { text: "确认配对码一致", fn: () => { bridge.call("pairRespond", p.reqID, true); pairWaiting(p.code); } },
      ]);
  } else if (p.state === "code") {
    showModal("正在配对",
      `<p>对方屏幕上应显示相同配对码，核对后两边都点确认：</p>
       <div class="pair-code">${esc(p.code)}</div>
       <p class="hint">${esc(p.message || "")}</p>`,
      [
        { text: "取消", cls: "danger", fn: () => { bridge.call("pairOutConfirm", false); hideModal(); } },
        { text: "确认配对", fn: () => { bridge.call("pairOutConfirm", true); pairWaiting(p.code); } },
      ]);
  } else if (p.state === "done") {
    showModal("配对成功", `<div class="pair-code">✓</div><p>${esc(p.message || "")}</p>`,
      [{ text: "好", fn: hideModal }]);
  } else if (p.state === "failed") {
    showModal("配对失败", `<p>${esc(p.message || "")}</p>`, [{ text: "关闭", cls: "danger", fn: hideModal }]);
  }
}
function pairWaiting(code) {
  showModal("等待对方确认", `<p>已发送确认，等待对方点击…</p><div class="pair-code">${esc(code)}</div>`, []);
}

// ---------- 启动 ----------
bridge.snapshot().then((s) => { S = s; render(); }).catch((e) => toast("error", "初始化失败: " + e));
