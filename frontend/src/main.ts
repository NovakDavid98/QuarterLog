import "./style.css";

// Wails exposes bound Go methods on window.go.main.App and events on window.runtime.
declare global {
  interface Window {
    go: { main: { App: any } };
    runtime: any;
  }
}

const App = () => window.go.main.App;
const rt = () => window.runtime;

interface Pending {
  id: string;
  date: string;
  from: string;
  to: string;
  hours: number;
  status: string;
  thumb: string;
  locked: boolean;
}

interface Config {
  miniMaxApiKey: string;
  miniMaxBaseUrl: string;
  miniMaxModel: string;
  filePath: string;
  categories: string;
  types: string;
  intervalMinutes: number;
  monitor: number;
  popupPosition: string;
  language: string;
  prompt: string;
  paused: boolean;
  autostart: boolean;
  confidential: boolean;
}

interface Suggestion {
  description: string;
  type: string;
}

const root = document.getElementById("app")!;
let current: Pending | null = null; // interval being edited
let cameFromQueue = false;
// Remembered across entries within a session so you don't re-pick every time.
let lastCategory = "";
let lastType = "";
// Confidentiality regime: when on, no screenshot is ever sent to the AI.
let confidential = false;

// opts parses a newline/comma-separated config string into an options array.
function opts(s: string): string[] {
  return (s || "")
    .split(/[\n,]/)
    .map((x) => x.trim())
    .filter(Boolean);
}

// selectHtml builds a <select> with the given options and selected value.
function selectHtml(id: string, options: string[], selected: string, placeholder?: string): string {
  const items = options
    .map((o) => `<option value="${esc(o)}" ${o === selected ? "selected" : ""}>${esc(o)}</option>`)
    .join("");
  const ph = placeholder ? `<option value="" ${!selected ? "selected" : ""}>${esc(placeholder)}</option>` : "";
  return `<select id="${id}">${ph}${items}</select>`;
}

// ---------- helpers ----------
function el(html: string): HTMLElement {
  const t = document.createElement("template");
  t.innerHTML = html.trim();
  return t.content.firstElementChild as HTMLElement;
}

function esc(s: string): string {
  return s.replace(/[&<>"]/g, (c) =>
    ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;" }[c]!)
  );
}

let toastTimer: number | undefined;
function toast(msg: string, isErr = false) {
  document.querySelector(".toast")?.remove();
  const t = el(`<div class="toast ${isErr ? "err" : ""}">${esc(msg)}</div>`);
  root.appendChild(t);
  requestAnimationFrame(() => t.classList.add("show"));
  clearTimeout(toastTimer);
  toastTimer = window.setTimeout(
    () => {
      t.classList.remove("show");
      setTimeout(() => t.remove(), 250);
    },
    isErr ? 5000 : 2600
  );
}

function hideWindow() {
  App().HidePopup();
}

// confirmDialog shows a styled in-app modal and resolves true/false. Matches the
// app's look instead of the browser's default confirm box.
function confirmDialog(opts: {
  icon?: string;
  title: string;
  body: string;
  confirmText: string;
  cancelText: string;
  danger?: boolean;
}): Promise<boolean> {
  return new Promise((resolve) => {
    const overlay = el(`
      <div class="modal-overlay">
        <div class="modal" role="dialog" aria-modal="true">
          ${opts.icon ? `<div class="modal-icon">${opts.icon}</div>` : ""}
          <div class="modal-title">${esc(opts.title)}</div>
          <div class="modal-body">${esc(opts.body)}</div>
          <div class="modal-actions">
            <button class="btn ghost no-drag" id="m-cancel">${esc(opts.cancelText)}</button>
            <button class="btn ${opts.danger ? "danger" : "primary"} no-drag" id="m-ok">${esc(opts.confirmText)}</button>
          </div>
        </div>
      </div>
    `);
    const close = (v: boolean) => {
      overlay.classList.remove("show");
      document.removeEventListener("keydown", onKey);
      setTimeout(() => overlay.remove(), 180);
      resolve(v);
    };
    const onKey = (e: KeyboardEvent) => {
      if (e.key === "Escape") close(false);
      if (e.key === "Enter") close(true);
    };
    overlay.querySelector("#m-ok")?.addEventListener("click", () => close(true));
    overlay.querySelector("#m-cancel")?.addEventListener("click", () => close(false));
    overlay.addEventListener("click", (e) => { if (e.target === overlay) close(false); });
    document.addEventListener("keydown", onKey);
    root.appendChild(overlay);
    requestAnimationFrame(() => overlay.classList.add("show"));
    (overlay.querySelector("#m-cancel") as HTMLElement | null)?.focus();
  });
}

// runCorrection sends the description box text to MiniMax to fix spelling and
// diacritics, replacing the text in place. Triggered by Shift+R.
let correcting = false;
async function runCorrection(ta: HTMLTextAreaElement) {
  const text = ta.value.trim();
  if (!text || correcting) return;
  // In the confidentiality regime, even the text-only spelling fix leaves the
  // machine — so confirm before sending the typed text to the AI provider.
  if (confidential) {
    const ok = await confirmDialog({
      icon: "🔒",
      title: "Confidentiality regime is on",
      body:
        "Fixing spelling sends the text you just typed to the AI provider (MiniMax). No screenshot is sent — but the words are. Do you want to send this text?",
      confirmText: "Send text & fix",
      cancelText: "No, keep it private",
    });
    if (!ok) return;
  }
  correcting = true;
  const prev = ta.value;
  ta.disabled = true;
  ta.value = "Fixing spelling…";
  try {
    const fixed: string = await App().Correct(prev);
    ta.value = fixed || prev;
    toast("Spelling & diacritics fixed ✓");
  } catch (e: any) {
    ta.value = prev;
    toast(String(e?.message ?? e), true);
  } finally {
    ta.disabled = false;
    correcting = false;
    ta.focus();
  }
}

// ---------- interval editor (used for popup + queue open) ----------
async function renderEditor(iv: Pending) {
  current = iv;
  void current;
  const locked = iv.locked;
  const cfg: Config = await App().GetConfig();
  const categories = opts(cfg.categories);
  const types = opts(cfg.types);
  const catSel = lastCategory || categories[0] || "";
  const typeSel = lastType || "";

  const shell = el(`
    <div class="shell">
      <div class="topbar">
        <div>
          <div class="title">${cameFromQueue ? "Log interval" : "What did you work on?"}</div>
          <div class="sub">${esc(iv.date)}</div>
        </div>
        <div class="spacer"></div>
        ${cameFromQueue ? `<button class="iconbtn no-drag" id="back" title="Back">‹</button>` : ""}
        <button class="iconbtn no-drag" id="later" title="Later">✕</button>
      </div>
      <div class="content fit">
        <div class="time-row">
          <div class="time-range">${esc(iv.from)} – ${esc(iv.to)}</div>
          <div class="pill">${iv.hours.toFixed(2)} h</div>
          ${locked ? `<div class="pill warn">screen locked</div>` : ""}
          <div class="pill lock" id="confpill" title="No screenshot is sent to the AI. Toggle with Shift+C." style="${confidential ? "" : "display:none;"}">🔒 Confidential</div>
        </div>
        <div class="thumb-wrap" id="thumb">
          ${
            locked || !iv.thumb
              ? `<div class="thumb-empty">No screenshot for this interval.<br/>Type what you did below.</div>`
              : `<img src="${iv.thumb}" alt="screenshot preview"/>`
          }
        </div>
        <div class="row" style="margin:8px 0 14px;">
          <button class="btn ghost wide no-drag" id="retake" title="Hides this window and re-shoots the screen — cover anything confidential first">🔄 Retake</button>
          <button class="btn ghost wide no-drag" id="opensettings" title="Open settings">⚙️ Settings</button>
        </div>
        <textarea class="desc no-drag" id="desc" placeholder="${
          locked ? "Describe your work…" : "Write it yourself, or let AI suggest from the screenshot…"
        }"></textarea>
        <div class="row mt">
          <button class="btn ghost wide no-drag" id="ai" data-locked="${locked ? "1" : "0"}" ${locked || confidential ? "disabled" : ""} title="${confidential ? "Disabled in the confidentiality regime — screenshots are never sent to the AI" : "Send the screenshot to the AI for a suggested description"}">✨ Suggest with AI</button>
          <button class="btn ghost wide no-drag" id="fix" title="Fix spelling & diacritics with AI — or press Shift + R">✓ Fix spelling</button>
        </div>
        <div class="grid2 no-drag" style="margin-top:10px;">
          <div class="field" style="margin-bottom:0;">
            <label>Category from order</label>
            ${selectHtml("cat", categories, catSel, categories.length ? undefined : "—")}
          </div>
          <div class="field" style="margin-bottom:0;">
            <label>Type</label>
            ${selectHtml("type", types, typeSel, "—")}
          </div>
        </div>
        <div class="row mt">
          <button class="btn ghost no-drag" id="later2">Later</button>
          <button class="btn ghost no-drag" id="skip" title="Nothing work-related this interval — discard it">Skip</button>
          <button class="btn primary wide no-drag" id="log">Log it</button>
        </div>
      </div>
    </div>
  `);

  root.replaceChildren(shell);

  const desc = shell.querySelector<HTMLTextAreaElement>("#desc")!;
  const aiBtn = shell.querySelector<HTMLButtonElement>("#ai");
  const fixBtn = shell.querySelector<HTMLButtonElement>("#fix");
  const catEl = shell.querySelector<HTMLSelectElement>("#cat")!;
  const typeEl = shell.querySelector<HTMLSelectElement>("#type")!;
  const logBtn = shell.querySelector<HTMLButtonElement>("#log")!;

  fixBtn?.addEventListener("click", () => runCorrection(desc));
  shell.querySelector("#opensettings")?.addEventListener("click", () => App().ShowSettings());

  const retakeBtn = shell.querySelector<HTMLButtonElement>("#retake");
  retakeBtn?.addEventListener("click", async () => {
    retakeBtn.disabled = true;
    if (aiBtn) aiBtn.disabled = true;
    const original = retakeBtn.innerHTML;
    try {
      // Countdown so the user can move a window over confidential content.
      for (let n = 3; n >= 1; n--) {
        retakeBtn.textContent = `Retaking in ${n}…`;
        await new Promise((r) => setTimeout(r, 1000));
      }
      retakeBtn.innerHTML = `<span class="spinner"></span>Capturing…`;
      const thumb: string = await App().Recapture(iv.id);
      iv.thumb = thumb;
      iv.locked = false;
      const wrap = shell.querySelector<HTMLElement>("#thumb");
      if (wrap) wrap.innerHTML = `<img src="${thumb}" alt="screenshot preview"/>`;
      toast("New screenshot taken ✓");
    } catch (e: any) {
      toast(String(e?.message ?? e), true);
    } finally {
      retakeBtn.disabled = false;
      retakeBtn.innerHTML = original;
      if (aiBtn) aiBtn.disabled = iv.locked || confidential; // enabled once an image exists
    }
  });

  aiBtn?.addEventListener("click", async () => {
    aiBtn.disabled = true;
    const original = aiBtn.innerHTML;
    aiBtn.innerHTML = `<span class="spinner"></span>Thinking…`;
    try {
      const s: Suggestion = await App().Describe(iv.id);
      desc.value = s.description;
      if (s.type) typeEl.value = s.type; // AI-chosen Type pre-selects the dropdown
      desc.focus();
    } catch (e: any) {
      toast(String(e?.message ?? e), true);
    } finally {
      aiBtn.innerHTML = original;
      aiBtn.disabled = false;
    }
  });

  logBtn.addEventListener("click", async () => {
    const text = desc.value.trim();
    if (!text) {
      desc.focus();
      toast("Add a short description first.", true);
      return;
    }
    logBtn.disabled = true;
    const original = logBtn.innerHTML;
    logBtn.innerHTML = `<span class="spinner"></span>Logging…`;
    try {
      await App().Submit(iv.id, text, catEl.value, typeEl.value);
      lastCategory = catEl.value;
      lastType = typeEl.value;
      hideWindow(); // dismiss the popup as soon as it's logged
    } catch (e: any) {
      toast(String(e?.message ?? e), true);
      logBtn.disabled = false;
      logBtn.innerHTML = original;
    }
  });

  shell.querySelector("#later")?.addEventListener("click", laterAction);
  shell.querySelector("#later2")?.addEventListener("click", laterAction);
  shell.querySelector("#skip")?.addEventListener("click", async () => {
    try {
      await App().Dismiss(iv.id);
      toast("Skipped");
      await afterResolve();
    } catch (e: any) {
      toast(String(e?.message ?? e), true);
    }
  });
  shell.querySelector("#back")?.addEventListener("click", () => renderQueue());

  setTimeout(() => desc.focus(), 60);
}

function laterAction() {
  if (cameFromQueue) renderQueue();
  else hideWindow();
}

// After logging or dismissing: go back to queue if more remain, else close/return.
async function afterResolve() {
  const pending: Pending[] = await App().GetPending();
  if (cameFromQueue) {
    renderQueue(pending);
  } else if (pending.length > 0) {
    renderEditor(pending[0]);
  } else {
    hideWindow();
  }
}

// ---------- queue review ----------
async function renderQueue(preloaded?: Pending[]) {
  cameFromQueue = true;
  const pending: Pending[] = preloaded ?? (await App().GetPending());
  const list = pending
    .map(
      (iv) => `
      <div class="qitem no-drag" data-id="${iv.id}">
        ${
          iv.locked || !iv.thumb
            ? `<div class="qthumb"></div>`
            : `<img class="qthumb" src="${iv.thumb}"/>`
        }
        <div class="qmeta">
          <div class="qtime">${esc(iv.from)} – ${esc(iv.to)}</div>
          <div class="qtag">${esc(iv.date)} · ${iv.hours.toFixed(2)} h${
            iv.locked ? " · screen was locked" : ""
          }</div>
        </div>
        <button class="qdel" data-del="${iv.id}" title="Delete this interval" aria-label="Delete">🗑</button>
        <div class="chev">›</div>
      </div>`
    )
    .join("");

  const shell = el(`
    <div class="shell">
      <div class="topbar">
        <div>
          <div class="title">Pending intervals</div>
          <div class="sub">${pending.length} to review</div>
        </div>
        <div class="spacer"></div>
        ${pending.length > 0 ? `<button class="btn ghost sm no-drag" id="clearall">Clear all</button>` : ""}
        <button class="iconbtn no-drag" id="close" title="Close">✕</button>
      </div>
      <div class="content">
        ${
          pending.length === 0
            ? `<div class="empty"><div class="big">✓</div>All caught up.<br/>Nothing waiting to be logged.</div>`
            : list
        }
      </div>
    </div>
  `);
  root.replaceChildren(shell);

  shell.querySelectorAll<HTMLElement>(".qitem").forEach((node) => {
    node.addEventListener("click", () => {
      const iv = pending.find((p) => p.id === node.dataset.id);
      if (iv) renderEditor(iv);
    });
  });

  // Per-item delete (doesn't open the editor).
  shell.querySelectorAll<HTMLButtonElement>(".qdel").forEach((btn) => {
    btn.addEventListener("click", async (e) => {
      e.stopPropagation();
      const id = btn.dataset.del!;
      btn.disabled = true;
      try {
        await App().Dismiss(id);
        renderQueue(pending.filter((p) => p.id !== id));
      } catch (err: any) {
        btn.disabled = false;
        toast(String(err?.message ?? err), true);
      }
    });
  });

  shell.querySelector("#clearall")?.addEventListener("click", async () => {
    if (!window.confirm(`Delete all ${pending.length} pending interval(s)? This can't be undone.`)) return;
    try {
      await App().ClearQueue();
      renderQueue([]);
    } catch (err: any) {
      toast(String(err?.message ?? err), true);
    }
  });

  shell.querySelector("#close")?.addEventListener("click", hideWindow);
}

// ---------- manual entry ----------
async function renderManual() {
  const now: { date: string } = await App().NowParts();
  const cfg: Config = await App().GetConfig();
  const categories = opts(cfg.categories);
  const types = opts(cfg.types);
  const durations = [0.25, 0.5, 0.75, 1, 1.5, 2, 3, 4];
  const shell = el(`
    <div class="shell">
      <div class="topbar">
        <div>
          <div class="title">New entry</div>
          <div class="sub">Log time manually</div>
        </div>
        <div class="spacer"></div>
        <button class="iconbtn no-drag" id="close" title="Close">✕</button>
      </div>
      <div class="content no-drag">
        <div class="grid2">
          <div class="field">
            <label>Date</label>
            <input type="text" id="date" value="${esc(now.date)}" placeholder="YYYY-MM-DD"/>
          </div>
          <div class="field">
            <label>How long did it take?</label>
            <select id="hours">
              ${durations
                .map((h) => `<option value="${h}" ${h === 0.25 ? "selected" : ""}>${h.toFixed(2)} h</option>`)
                .join("")}
            </select>
          </div>
        </div>
        <div class="grid2">
          <div class="field">
            <label>Category from order</label>
            ${selectHtml("cat", categories, lastCategory || categories[0] || "", categories.length ? undefined : "—")}
          </div>
          <div class="field">
            <label>Type</label>
            ${selectHtml("type", types, lastType, "—")}
          </div>
        </div>
        <div class="field">
          <label>What did you work on?</label>
          <textarea id="desc" placeholder="Describe the task…" style="min-height:96px;"></textarea>
          <div class="row mt"><button class="btn ghost wide no-drag" id="fix" type="button" title="Fix spelling & diacritics with AI — or press Shift + R">✓ Fix spelling</button></div>
        </div>
      </div>
      <div class="footer-bar no-drag">
        <button class="btn ghost" id="cancel">Cancel</button>
        <button class="btn primary wide" id="save">Log it</button>
      </div>
    </div>
  `);
  root.replaceChildren(shell);

  const q = (id: string) => shell.querySelector<HTMLInputElement | HTMLSelectElement | HTMLTextAreaElement>("#" + id)!;
  const saveBtn = shell.querySelector<HTMLButtonElement>("#save")!;

  shell.querySelector("#fix")?.addEventListener("click", () => runCorrection(q("desc") as HTMLTextAreaElement));

  saveBtn.addEventListener("click", async () => {
    const desc = (q("desc") as HTMLTextAreaElement).value.trim();
    if (!desc) {
      (q("desc") as HTMLTextAreaElement).focus();
      toast("Add a short description first.", true);
      return;
    }
    const date = (q("date") as HTMLInputElement).value.trim();
    const hours = parseFloat((q("hours") as HTMLSelectElement).value);
    const cat = (q("cat") as HTMLSelectElement).value;
    const typ = (q("type") as HTMLSelectElement).value;
    saveBtn.disabled = true;
    const original = saveBtn.innerHTML;
    saveBtn.innerHTML = `<span class="spinner"></span>Logging…`;
    try {
      await App().SubmitManual(date, hours, cat, desc, typ);
      lastCategory = cat;
      lastType = typ;
      toast("Logged ✓");
      hideWindow();
    } catch (e: any) {
      toast(String(e?.message ?? e), true);
      saveBtn.disabled = false;
      saveBtn.innerHTML = original;
    }
  });
  shell.querySelector("#cancel")?.addEventListener("click", hideWindow);
  shell.querySelector("#close")?.addEventListener("click", hideWindow);
  setTimeout(() => (q("desc") as HTMLTextAreaElement).focus(), 60);
}

// ---------- settings ----------
async function renderSettings() {
  const cfg: Config = await App().GetConfig();
  const shell = el(`
    <div class="shell">
      <div class="topbar">
        <div class="title">Settings</div>
        <div class="spacer"></div>
        <button class="iconbtn no-drag" id="close" title="Close">✕</button>
      </div>
      <div class="content no-drag">
        <div class="section-title">MiniMax vision API</div>
        <div class="field">
          <label>API key</label>
          <input type="password" id="miniMaxApiKey" value="${esc(cfg.miniMaxApiKey)}" placeholder="sk-…"/>
        </div>
        <div class="grid2">
          <div class="field">
            <label>Base URL</label>
            <input type="text" id="miniMaxBaseUrl" value="${esc(cfg.miniMaxBaseUrl)}"/>
          </div>
          <div class="field">
            <label>Model</label>
            <input type="text" id="miniMaxModel" value="${esc(cfg.miniMaxModel)}"/>
          </div>
        </div>

        <div class="section-title">Worklog file</div>
        <div class="field">
          <label>Excel file path</label>
          <input type="text" id="filePath" value="${esc(cfg.filePath)}" placeholder="…\\Documents\\Quarterlog\\worklog.xlsx"/>
        </div>
        <div class="row" style="margin-bottom:4px;">
          <button class="btn ghost wide" id="openFile" type="button">📄 Open file</button>
          <button class="btn ghost wide" id="revealFile" type="button">📂 Show in folder</button>
        </div>

        <div class="section-title">Worklog fields</div>
        <div class="grid2">
          <div class="field">
            <label>Categories (one per line)</label>
            <textarea id="categories" placeholder="VAPOMAN">${esc(cfg.categories)}</textarea>
          </div>
          <div class="field">
            <label>Types (one per line)</label>
            <textarea id="types" placeholder="New">${esc(cfg.types)}</textarea>
          </div>
        </div>

        <div class="section-title">Capture</div>
        <div class="grid2">
          <div class="field">
            <label>Interval (minutes)</label>
            <input type="number" id="intervalMinutes" min="1" max="120" value="${cfg.intervalMinutes}"/>
          </div>
          <div class="field">
            <label>Monitor</label>
            <select id="monitor">
              <option value="-1" ${cfg.monitor === -1 ? "selected" : ""}>Primary</option>
              <option value="-2" ${cfg.monitor === -2 ? "selected" : ""}>All (stitched)</option>
              <option value="0" ${cfg.monitor === 0 ? "selected" : ""}>Display 1</option>
              <option value="1" ${cfg.monitor === 1 ? "selected" : ""}>Display 2</option>
              <option value="2" ${cfg.monitor === 2 ? "selected" : ""}>Display 3</option>
            </select>
          </div>
        </div>
        <div class="field">
          <label>Popup position on screen</label>
          <div class="posgrid" id="posgrid">
            ${["top-left","top-center","top-right","center-left","center","center-right","bottom-left","bottom-center","bottom-right"]
              .map((p) => `<button type="button" class="poscell ${p === (cfg.popupPosition || "bottom-right") ? "sel" : ""}" data-pos="${p}"></button>`)
              .join("")}
          </div>
          <div class="hint">Click a zone to choose where the 15-minute popup appears.</div>
        </div>
        <div class="field">
          <label>Description language</label>
          <input type="text" id="language" value="${esc(cfg.language)}" placeholder="Czech"/>
        </div>
        <div class="field">
          <label>AI guidance prompt</label>
          <textarea id="prompt">${esc(cfg.prompt)}</textarea>
        </div>
        <div class="field">
          <label class="check">
            <input type="checkbox" id="autostart" ${cfg.autostart ? "checked" : ""}/>
            Launch Quarterlog at Windows startup
          </label>
        </div>

        <div class="section-title">Privacy</div>
        <div class="field">
          <label class="check">
            <input type="checkbox" id="confidential" ${cfg.confidential ? "checked" : ""}/>
            Confidentiality regime (toggle anytime with <b>Shift + C</b>)
          </label>
          <div class="hint">When ON, <b>no screenshot is ever sent to the AI</b>. “Suggest with AI” is disabled and you type descriptions yourself. Only the text-only spelling fix works — and it asks for confirmation first, because it still sends your typed text to the AI provider.</div>
        </div>

        <div class="section-title">Danger zone</div>
        <div class="field">
          <button class="btn danger no-drag" id="clearxls" type="button">🗑 Delete all rows from the worklog…</button>
          <div class="hint">Permanently empties the Excel file (keeps the styled header). You'll be asked to type <b>DELETE</b> to confirm.</div>
        </div>
      </div>
      <div class="footer-bar no-drag">
        <button class="btn ghost" id="cancel">Cancel</button>
        <button class="btn primary wide" id="save">Save</button>
      </div>
    </div>
  `);
  root.replaceChildren(shell);

  const get = (id: string) =>
    shell.querySelector<HTMLInputElement | HTMLSelectElement | HTMLTextAreaElement>("#" + id)!;

  let pickedPos = cfg.popupPosition || "bottom-right";
  shell.querySelectorAll<HTMLElement>(".poscell").forEach((c) => {
    c.addEventListener("click", () => {
      pickedPos = c.dataset.pos || pickedPos;
      shell.querySelectorAll(".poscell").forEach((x) => x.classList.remove("sel"));
      c.classList.add("sel");
    });
  });

  shell.querySelector("#save")?.addEventListener("click", async () => {
    const next: Config = {
      miniMaxApiKey: (get("miniMaxApiKey") as HTMLInputElement).value.trim(),
      miniMaxBaseUrl: (get("miniMaxBaseUrl") as HTMLInputElement).value.trim(),
      miniMaxModel: (get("miniMaxModel") as HTMLInputElement).value.trim(),
      filePath: (get("filePath") as HTMLInputElement).value.trim(),
      categories: (get("categories") as HTMLTextAreaElement).value,
      types: (get("types") as HTMLTextAreaElement).value,
      intervalMinutes: parseInt((get("intervalMinutes") as HTMLInputElement).value, 10) || 15,
      monitor: parseInt((get("monitor") as HTMLSelectElement).value, 10),
      popupPosition: pickedPos,
      language: (get("language") as HTMLInputElement).value.trim(),
      prompt: (get("prompt") as HTMLTextAreaElement).value,
      paused: cfg.paused,
      autostart: (get("autostart") as HTMLInputElement).checked,
      confidential: (get("confidential") as HTMLInputElement).checked,
    };
    try {
      await App().SaveConfig(next);
      confidential = next.confidential; // keep the shortcut/UI state in sync
      toast("Settings saved ✓");
      hideWindow();
    } catch (e: any) {
      toast(String(e?.message ?? e), true);
    }
  });
  shell.querySelector("#openFile")?.addEventListener("click", async () => {
    try {
      await App().SetFilePath((get("filePath") as HTMLInputElement).value.trim());
      await App().OpenWorklogFile();
    } catch (e: any) {
      toast(String(e?.message ?? e), true);
    }
  });
  shell.querySelector("#revealFile")?.addEventListener("click", async () => {
    try {
      await App().SetFilePath((get("filePath") as HTMLInputElement).value.trim());
      await App().RevealWorklogFolder();
    } catch (e: any) {
      toast(String(e?.message ?? e), true);
    }
  });
  shell.querySelector("#clearxls")?.addEventListener("click", async () => {
    const ans = window.prompt(
      "This permanently deletes EVERY row in your worklog Excel file.\n\nType DELETE to confirm:"
    );
    if (ans === null) return; // cancelled
    if (ans !== "DELETE") {
      toast("Not deleted — you must type DELETE exactly.", true);
      return;
    }
    try {
      await App().SetFilePath((get("filePath") as HTMLInputElement).value.trim());
      await App().ClearWorklog();
      toast("Worklog cleared ✓");
    } catch (e: any) {
      toast(String(e?.message ?? e), true);
    }
  });
  shell.querySelector("#cancel")?.addEventListener("click", hideWindow);
  shell.querySelector("#close")?.addEventListener("click", hideWindow);
}

// ---------- wiring ----------
// syncConfidentialDom reflects the current regime in an open editor without a
// full re-render (so typed text isn't lost).
function syncConfidentialDom() {
  const pill = document.getElementById("confpill");
  if (pill) pill.style.display = confidential ? "" : "none";
  const ai = document.getElementById("ai") as HTMLButtonElement | null;
  if (ai) ai.disabled = confidential || ai.dataset.locked === "1";
}

function boot() {
  // Load the confidentiality regime state.
  App().GetConfig().then((c: Config) => { confidential = !!c.confidential; syncConfidentialDom(); });

  document.addEventListener("keydown", (e) => {
    const ae = document.activeElement as HTMLElement | null;
    // Shift+R inside the description box: fix spelling & diacritics via AI.
    if (e.shiftKey && e.code === "KeyR" && ae && ae.id === "desc") {
      e.preventDefault();
      runCorrection(ae as HTMLTextAreaElement);
      return;
    }
    // Shift+C toggles the confidentiality regime — but not while typing, so
    // capital C (and Č) still work in text fields.
    const typing = ae && (ae.tagName === "INPUT" || ae.tagName === "TEXTAREA");
    if (e.shiftKey && e.code === "KeyC" && !typing) {
      e.preventDefault();
      App().ToggleConfidential().then((on: boolean) => {
        confidential = on;
        syncConfidentialDom();
        toast(on ? "🔒 Confidentiality regime ON" : "Confidentiality regime OFF");
      });
    }
  });

  rt().EventsOn("tick", (iv: Pending) => {
    cameFromQueue = false;
    renderEditor(iv);
  });

  rt().EventsOn("navigate", (view: string) => {
    if (view === "settings") renderSettings();
    else if (view === "manual") renderManual();
    else renderQueue();
  });

  renderQueue();
}

if (window.runtime) boot();
else window.addEventListener("DOMContentLoaded", () => setTimeout(boot, 50));
