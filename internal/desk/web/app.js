/* WinForge Desk —— 微信风格前端：图标栏 + 列表栏 + 内容区 */
"use strict";

/* ================= 桥接 ================= */
function makeBridge() {
  // R54 教训：Wails 绑定名=结构体名，曾经叫 DeskApp 而前端找 Desk，
  // 桌面窗口整体静默降级成"浏览器桥打不通的静态壳"。双名兜底防再犯。
  const bound = (window.go && window.go.main) && (window.go.main.Desk || window.go.main.DeskApp);
  if (bound) {
    const b = bound;
    const rt = window.runtime;
    return {
      mode: "wails",
      snapshot: () => b.Snapshot(),
      call: (cmd, ...args) => {
        const map = {
          pairStart: b.PairStart, pairRespond: b.PairRespond, pairOutConfirm: b.PairOutConfirm,
          peerPosition: b.PeerPosition, peerToggle: b.PeerToggle, peerRemove: b.PeerRemove,
          sendChat: b.SendChat, sendFile: b.SendFile, pickAndSendFile: b.PickAndSendFile,
          transferRespond: b.TransferRespond, cancelTransfer: b.CancelTransfer,
          chatHistory: b.ChatHistory, clearChat: b.ClearChat,
          takeControl: b.TakeControl, releaseControl: b.ReleaseControl,
          toggleShare: b.ToggleShare, openPath: b.OpenPath,
        };
        const fn = map[cmd];
        if (!fn) return Promise.reject(new Error("未知命令 " + cmd));
        return fn(...args);
      },
      on: (event, cb) => { rt.EventsOn(event, (data) => cb(data)); },
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
  const listeners = new Map(); // event -> Set<cb>
  let lastSeq = 0;
  let pollStarted = false;
  function dispatch(ev) {
    if (ev && typeof ev.seq === "number") lastSeq = Math.max(lastSeq, ev.seq);
    const set = listeners.get(ev.event);
    if (set) for (const cb of Array.from(set)) { try { cb(ev.data); } catch (_) {} }
  }
  // 事件传输：单个长轮询循环分发全部事件（after=seq 断点续传，空档不丢）。
  // 历史上的 /ws 从无服务端实现，浏览器模式推送一直因此失效。
  function startPolling() {
    if (pollStarted) return;
    pollStarted = true;
    (async () => {
      for (;;) {
        try {
          const url = "/api/events?t=" + encodeURIComponent(token) + (lastSeq > 0 ? "&after=" + lastSeq : "");
          const r = await fetch(url);
          const batch = await r.json();
          (batch || []).forEach(dispatch);
        } catch (_) {}
        await new Promise((res) => setTimeout(res, 50));
      }
    })();
  }
  return {
    mode: "http",
    snapshot: () => rpc("snapshot", []),
    call: (cmd, ...args) => rpc(cmd, args),
    on: (event, cb) => {
      let set = listeners.get(event);
      if (!set) { set = new Set(); listeners.set(event, set); }
      set.add(cb);
      startPolling();
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

/* ================= 状态 ================= */
let S = { peers: [], presence: [], control: { role: 0 }, self: {} };
let activeTab = "share";     // share / transfer / control
let selectedPeer = "";       // 共享/控制列表选中的对端
let chatPeer = "";           // 传输视图当前对话对端
const chatLog = Object.create(null); // peerId -> [{from,text,ts,outgoing}]
const transfers = new Map();         // fid -> view
const transferRate = new Map();      // fid -> {lastRecv, lastAt, ema}
let startupWarnShown = false;

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
  if (n > 1 << 20) return (n / (1 << 20)).toFixed(1) + " MB";
  if (n > 1024) return (n / 1024).toFixed(0) + " KB";
  return n + " B";
}
function fmtTime(ts) {
  const d = new Date(ts);
  return String(d.getHours()).padStart(2, "0") + ":" + String(d.getMinutes()).padStart(2, "0");
}
function fmtRate(bps) {
  if (bps > 1 << 20) return (bps / (1 << 20)).toFixed(1) + " MB/s";
  if (bps > 1024) return (bps / 1024).toFixed(0) + " KB/s";
  return bps.toFixed(0) + " B/s";
}
function peerById(id) { return (S.peers || []).find(p => p.id === id); }
function peerName(id) { const p = peerById(id); return p ? p.name : (id || "").slice(0, 8); }

/* ================= 渲染 ================= */
function render() {
  renderHeader();
  renderListPanel();
  renderContent();
  maybeShowStartupWarning();
}
function renderHeader() {
  const st = S.control || { role: 0 };
  const titles = { share: "共享", transfer: "传输", control: "控制" };
  $("content-title").textContent = titles[activeTab] || "共享";
  $("share-toggle").checked = !!S.share;
  $("hotkey-hint").textContent = "热键 " + ((S.self && S.self.hotkey) || "Ctrl+Alt+D");
  const hi = $("hotkey-inline");
  if (hi) hi.textContent = (S.self && S.self.hotkey) || "Ctrl+Alt+D";
}
function renderListPanel() {
  const title = { share: "设备", transfer: "聊天", control: "设备" }[activeTab] || "设备";
  $("list-title").textContent = title;
  const box = $("peer-list");
  box.innerHTML = "";
  const kw = ($("search").value || "").trim().toLowerCase();
  const known = new Set((S.peers || []).map(p => p.id));
  const presence = (S.presence || []).filter(p => !known.has(p.id) &&
    (!kw || p.name.toLowerCase().includes(kw)));
  // 已配对设备
  for (const p of (S.peers || [])) {
    if (kw && !p.name.toLowerCase().includes(kw)) continue;
    const sel = (activeTab !== "transfer" && p.id === selectedPeer) ||
                (activeTab === "transfer" && p.id === chatPeer);
    const item = document.createElement("div");
    item.className = "peer-item" + (sel ? " selected" : "");
    item.innerHTML =
      '<div class="peer-avatar' + (p.online ? "" : " off") + '">' + esc((p.name || "?")[0]) + '</div>' +
      '<div class="peer-info"><div class="peer-name">' + esc(p.name) + '</div>' +
      '<div class="peer-sub">' + (activeTab === "transfer" ? chatPreview(p.id) :
        (p.online ? "在线" : "离线") + (p.addr ? " · " + esc(p.addr) : "")) + '</div></div>' +
      '<div class="peer-actions">' +
      (p.position === "left" ? '<span>←左</span>' : p.position === "right" ? '<span>右→</span>' : '') +
      '</div>';
    item.onclick = () => {
      selectedPeer = p.id;
      if (activeTab === "transfer") { chatPeer = p.id; ensureChatLog(p.id); }
      render();
    };
    box.appendChild(item);
  }
  // 未配对的发现设备（仅共享 tab 展示可配对）
  if (activeTab === "share") {
    for (const p of presence) {
      if (kw && !p.name.toLowerCase().includes(kw)) continue;
      const item = document.createElement("div");
      item.className = "peer-item";
      item.innerHTML =
        '<div class="peer-avatar off">?</div>' +
        '<div class="peer-info"><div class="peer-name">' + esc(p.name) + '</div>' +
        '<div class="peer-sub">未配对 · ' + esc(p.addr) + '</div></div>' +
        '<div class="peer-actions"><button class="btn-mini" data-act="pair" data-addr="' + esc(p.addr) + '">配对</button></div>';
      box.appendChild(item);
    }
  }
  if (!box.children.length) {
    box.innerHTML = '<div class="empty-tip">' + (S.peers.length ? "无匹配设备" :
      "尚未配对设备<br>等待局域网发现或手动输入地址配对") + '</div>';
  }
}
function chatPreview(peerId) {
  const list = chatLog[peerId] || [];
  const last = list[list.length - 1];
  if (!last) return "暂无消息";
  return (last.outgoing ? "我: " : "") + String(last.text).slice(0, 20);
}
function ensureChatLog(id) {
  if (!(id in chatLog)) {
    chatLog[id] = [];
    bridge.call("chatHistory", id).then((list) => {
      chatLog[id] = (list || []).map(x => ({
        from: x.outgoing ? "我" : peerName(id), text: x.text, ts: x.ts, outgoing: x.outgoing,
      }));
      if (chatPeer === id) renderChat();
    }).catch(() => {});
  }
}
function renderContent() {
  if (activeTab === "transfer") renderTransferView();
  else if (activeTab === "control") renderControlView();
  else renderShareDetail();
}
function renderShareDetail() {
  const box = $("share-detail");
  const p = peerById(selectedPeer);
  if (!p) {
    box.innerHTML = '<div class="guide"><p>在左侧选择一台设备查看详情。</p>' +
      '<p>配对：两台机器显示<b>同一个 6 位配对码</b>，两边都点确认即完成。</p>' +
      '<p>方位（左/右）用于屏幕边缘推挤切换；热键 <b>' + esc((S.self && S.self.hotkey) || "Ctrl+Alt+D") +
      '</b> 可随时切换控制。</p></div>';
    return;
  }
  box.innerHTML =
    '<div class="detail-card"><h4>' + esc(p.name) + '</h4>' +
    '<div class="detail-row"><span class="k">状态</span><span class="dot ' + (p.online ? "on" : "") + '"></span>' +
    (p.online ? "在线" : "离线") + '</div>' +
    '<div class="detail-row"><span class="k">地址</span>' + esc(p.addr || "未知") + '</div>' +
    '<div class="detail-row"><span class="k">方位</span>' +
    '<span class="pos-group">' +
    '<button data-act="pos" data-id="' + p.id + '" data-pos="left" class="' + (p.position === "left" ? "on" : "") + '">← 左</button>' +
    '<button data-act="pos" data-id="' + p.id + '" data-pos="" class="' + (!p.position ? "on" : "") + '">·</button>' +
    '<button data-act="pos" data-id="' + p.id + '" data-pos="right" class="' + (p.position === "right" ? "on" : "") + '">右 →</button>' +
    '</span></div>' +
    '<div class="detail-actions">' +
    '<button data-act="ctl" data-id="' + p.id + '"' + (p.online ? "" : " disabled") + '>控制这台</button>' +
    '<button class="ghost" data-act="toggle" data-id="' + p.id + '">' + (p.enabled ? "停用" : "启用") + '</button>' +
    '<button class="danger" data-act="remove" data-id="' + p.id + '">删除配对</button>' +
    '</div></div>';
}
function renderControlView() {
  const ctl = S.control || { role: 0 };
  const big = document.createElement("div");
  big.className = "ctl-big" + (ctl.role === 1 ? " ctrl" : ctl.role === 2 ? " victim" : "");
  big.textContent = ctl.role === 1 ? "正在控制「" + ctl.peer_name + "」" :
    ctl.role === 2 ? "「" + ctl.peer_name + "」正在控制本机" : "空闲";
  const box = $("control-detail");
  box.innerHTML = "";
  box.appendChild(big);
  const card = document.createElement("div");
  card.className = "detail-card";
  let rows = "";
  for (const p of (S.peers || [])) {
    if (!p.enabled) continue;
    rows += '<div class="detail-row"><span class="dot ' + (p.online ? "on" : "") + '"></span>' +
      '<b>' + esc(p.name) + '</b><span class="k">' + esc(p.position === "left" ? "左" : p.position === "right" ? "右" : "未设方位") + '</span>' +
      '<button class="btn-mini" data-act="ctl" data-id="' + p.id + '"' +
      (p.online && ctl.role === 0 ? "" : " disabled") + '>控制</button></div>';
  }
  card.innerHTML = rows || '<div class="empty-tip">暂无配对设备</div>';
  box.appendChild(card);
}
function renderTransferView() {
  ensureChatLog(chatPeer);
  renderChatHeader();
  renderChat();
  renderTransfers();
}
function renderChatHeader() {
  $("chat-peer-name").textContent = chatPeer ? peerName(chatPeer) : "选择设备";
}
function renderChat() {
  const box = $("chat-log");
  const list = chatLog[chatPeer] || [];
  box.innerHTML = list.map(m =>
    '<div class="msg-row ' + (m.outgoing ? "me" : "their") + '">' +
    '<div class="msg-avatar">' + (m.outgoing ? "我" : esc(peerName(chatPeer)[0] || "?")) + '</div>' +
    '<div><div class="msg-time">' + fmtTime(m.ts) + '</div>' +
    '<div class="msg-bubble">' + esc(m.text) + '</div></div></div>'
  ).join("");
  box.scrollTop = box.scrollHeight;
}
const rateEma = new Map();
function renderTransfers() {
  const box = $("transfer-list");
  box.innerHTML = "";
  const now = performance.now();
  for (const [id, t] of transfers) {
    const pct = t.size ? Math.min(100, Math.round((t.recv || 0) * 100 / t.size)) : 0;
    const active = t.state === "receiving" || t.state === "sending";
    let rate = 0;
    if (active) {
      const prev = rateEma.get(id);
      if (prev && t.recv > prev.recv) {
        const dt = (now - prev.at) / 1000;
        if (dt > 0.05) rate = (t.recv - prev.recv) / dt;
      }
      const ema = prev && prev.ema ? prev.ema * 0.6 + rate * 0.4 : rate;
      rateEma.set(id, { recv: t.recv, at: now, ema });
    } else if (!active && rateEma.has(id) && (t.state === "done" || t.state === "failed" ||
               t.state === "cancelled" || t.state === "rejected")) {
      rateEma.delete(id);
    }
    const el = document.createElement("div");
    el.className = "tr-card";
    const stateText = { offer: "等待确认", receiving: "接收中", sending: "发送中",
      sent: "已发出", done: "已完成", failed: "失败", rejected: "被拒收",
      cancelled: "已取消" }[t.state] || t.state;
    const rateText = active && rate > 0 ? " · " + fmtRate(rate) : "";
    el.innerHTML = '<div class="row">' +
      '<span class="tname">' + (t.dir === "in" ? "↓ " : "↑ ") + esc(t.name || "") + '</span>' +
      '<span class="tstate ' + t.state + '">' + stateText + " " +
      (t.size ? fmtSize(t.recv || 0) + "/" + fmtSize(t.size) : "") + rateText + '</span></div>' +
      '<div class="bar"><div style="width:' + pct + '%"></div></div>' +
      '<div class="actions">' +
      (t.state === "offer" ? '<button class="btn-mini" data-act="tr-yes" data-id="' + id + '">接收</button>' +
        '<button class="danger" data-act="tr-no" data-id="' + id + '">拒收</button>' : "") +
      (active ? '<button class="danger" data-act="tr-cancel" data-id="' + id + '">取消</button>' : "") +
      (t.state === "done" && t.path ? '<button class="btn-mini" data-act="open" data-path="' + esc(t.path) + '">打开</button>' : "") +
      '</div>';
    box.appendChild(el);
  }
  if (!transfers.size) {
    box.innerHTML = '<div class="empty-tip">暂无传输任务</div>';
  }
}

/* ================= 事件绑定 ================= */
document.body.addEventListener("click", (e) => {
  const btn = e.target.closest("button[data-act]");
  if (!btn) return;
  const act = btn.dataset.act, id = btn.dataset.id;
  if (act === "pair") bridge.call("pairStart", btn.dataset.addr).catch(err => toast("error", String(err)));
  if (act === "pos") bridge.call("peerPosition", id, btn.dataset.pos).then(() => render()).catch(() => {});
  if (act === "toggle") {
    const p = peerById(id);
    if (p) bridge.call("peerToggle", id, !p.enabled).then(() => render()).catch(() => {});
  }
  if (act === "remove") {
    if (confirm("删除该配对？双方都需要重新配对。")) {
      bridge.call("peerRemove", id).then(() => {
        if (selectedPeer === id) selectedPeer = "";
        render();
      }).catch(() => {});
    }
  }
  if (act === "ctl") {
    bridge.call("takeControl", id).then(r => {
      if (r && r.error) toast("warn", r.error);
    }).catch(err => toast("warn", String(err)));
  }
  if (act === "unctl") bridge.call("releaseControl").catch(() => {});
  if (act === "tr-yes") bridge.call("transferRespond", id, true).catch(() => {});
  if (act === "tr-no") bridge.call("transferRespond", id, false).catch(() => {});
  if (act === "tr-cancel") bridge.call("cancelTransfer", id).catch(() => {});
  if (act === "open") bridge.call("openPath", btn.dataset.path).catch(err => toast("error", String(err)));
});

$("share-toggle").addEventListener("change", (e) =>
  bridge.call("toggleShare", e.target.checked).then(() => render()).catch(() => {}));
$("search").addEventListener("input", () => renderListPanel());
$("btn-pair").onclick = () => showPairAddressModal();
$("chat-send").onclick = sendChatMsg;
$("chat-text").addEventListener("keydown", (e) => { if (e.key === "Enter") sendChatMsg(); });
$("chat-clear").onclick = () => {
  if (!chatPeer) return;
  if (!confirm("清空与该设备的本地聊天记录？（对端不受影响）")) return;
  bridge.call("clearChat", chatPeer).catch(err => toast("error", String(err)));
  delete chatLog[chatPeer];
  renderChat();
};
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
$("btn-exit").onclick = () => toast("info", "请从系统托盘图标右键退出");

function sendChatMsg() {
  const v = $("chat-text").value.trim();
  if (!v || !chatPeer) return;
  bridge.call("sendChat", chatPeer, v).then((r) => {
    if (r && r.error) toast("warn", r.error); else $("chat-text").value = "";
  }).catch(err => toast("warn", String(err)));
}

/* ================= 配对弹窗 ================= */
function showPairAddressModal() {
  const known = (S.presence || []).map(p =>
    '<label class="pair-quick"><input type="radio" name="pq" value="' + esc(p.addr) + '"> ' +
    esc(p.name) + '（' + esc(p.addr) + '）</label>').join("");
  showModal("配对新设备",
    '<p class="hint">在下方输入对方控制台显示的地址（IP:端口），或从发现的设备中选：</p>' +
    '<input id="pair-addr" type="text" placeholder="192.168.x.x:41831" style="width:100%">' +
    '<div class="quick-list">' + known + '</div>',
    [
      { text: "取消", cls: "ghost", fn: hideModal },
      { text: "发起配对", fn: () => {
          const addr = $("pair-addr").value.trim();
          if (!addr) { toast("warn", "请输入地址"); return; }
          bridge.call("pairStart", addr).then(() => {
            showModal("配对中",
              '<p>等待对方确认，配对码在对方屏幕上显示…</p><div class="pair-code">…</div>', []);
          }).catch(err => toast("error", String(err)));
        } },
    ]);
  // 让地址输入默认带第一个发现的设备地址
  const first = (S.presence || [])[0];
  if (first) $("pair-addr").value = first.addr + ":" + first.port;
}
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
      '<p><b>' + esc((p.message || "").split(" 想配对")[0]) + '</b> 想与本机配对。</p>' +
      '<p class="hint">请核对对方屏幕上的配对码与本机一致：</p>' +
      '<div class="pair-code">' + esc(p.code) + '</div>',
      [
        { text: "拒绝", cls: "danger", fn: () => { bridge.call("pairRespond", p.reqID, false); hideModal(); } },
        { text: "确认配对码一致", fn: () => { bridge.call("pairRespond", p.reqID, true); pairWaiting(p.code); } },
      ]);
  } else if (p.state === "pairing") {
    showModal("配对中",
      '<p>对方屏幕上应显示相同配对码，核对后两边都点确认：</p>' +
      '<div class="pair-code">' + esc(p.code) + '</div>' +
      '<p class="hint">' + esc(p.message || "") + '</p>',
      [
        { text: "取消", cls: "danger", fn: () => { bridge.call("pairOutConfirm", false); hideModal(); } },
        { text: "确认配对", fn: () => { bridge.call("pairOutConfirm", true); pairWaiting(p.code); } },
      ]);
  } else if (p.state === "done") {
    showModal("配对成功", '<div class="pair-code">✓</div><p>' + esc(p.message || "") + '</p>',
      [{ text: "好", fn: hideModal }]);
  } else if (p.state === "failed") {
    showModal("配对失败", '<p>' + esc(p.message || "") + '</p>', [{ text: "关闭", cls: "danger", fn: hideModal }]);
  }
}
function pairWaiting(code) {
  showModal("等待对方确认", '<p>已发送确认，等待对方点击…</p><div class="pair-code">' + esc(code) + '</div>', []);
}

/* ================= 服务端/宿主事件 ================= */
function seedTransfers() {
  if (!Array.isArray(S.transfers)) return;
  for (const t of S.transfers) {
    transfers.set(t.id, Object.assign(transfers.get(t.id) || {}, t));
  }
  renderTransfers();
}
bridge.on("desk:state", (s) => {
  S = s;
  seedTransfers();
  if (!chatPeer && S.peers && S.peers.length) { chatPeer = S.peers[0].id; ensureChatLog(chatPeer); }
  if (!selectedPeer && S.peers && S.peers.length) selectedPeer = S.peers[0].id;
  render();
});
const chatSeen = new Set(); // 回放/重发可能重复推送，按 peer|ts 去重
bridge.on("desk:chat", (m) => {
  const key = m.peer + "|" + m.ts;
  if (chatSeen.has(key)) return;
  if (chatSeen.size > 500) chatSeen.clear();
  chatSeen.add(key);
  (chatLog[m.peer] = chatLog[m.peer] || []).push({
    from: m.outgoing ? "我" : peerName(m.peer), text: m.text, ts: m.ts, outgoing: m.outgoing,
  });
  if (m.peer === chatPeer) renderChat();
});
bridge.on("desk:transfer", (t) => {
  transfers.set(t.id, Object.assign(transfers.get(t.id) || {}, t));
  renderTransfers();
});
bridge.on("desk:pair", renderPairModal);
bridge.on("desk:control", (c) => { S.control = c; if (activeTab === "control") renderContent(); });
bridge.on("desk:toast", (t) => toast(t.level, t.text));

/* ================= 启动 ================= */
function maybeShowStartupWarning() {
  if (startupWarnShown || !S || !S.startupWarning) return;
  startupWarnShown = true;
  toast("warn", S.startupWarning);
}
function switchTab(tab) {
  if (!document.getElementById("view-" + tab)) return;
  document.querySelectorAll(".nav-btn").forEach(x => x.classList.toggle("active", x.dataset.tab === tab));
  document.querySelectorAll(".view").forEach(x => x.classList.remove("active"));
  activeTab = tab;
  $("view-" + tab).classList.add("active");
  $("content-title").textContent = { share: "共享", transfer: "传输", control: "控制" }[tab] || "共享";
  renderListPanel();
  renderContent();
}
document.querySelectorAll(".nav-btn[data-tab]").forEach(b => b.onclick = () => switchTab(b.dataset.tab));
const deepTab = new URLSearchParams(location.search).get("tab");
if (deepTab && deepTab !== "share") switchTab(deepTab);
bridge.snapshot().then((s) => {
  S = s;
  seedTransfers();
  if (S.peers && S.peers.length) { chatPeer = S.peers[0].id; selectedPeer = S.peers[0].id; }
  render();
}).catch((e) => toast("error", "初始化失败: " + e));
