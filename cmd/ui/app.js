const $ = (s) => document.querySelector(s);
const $$ = (s) => Array.from(document.querySelectorAll(s));

const state = {
  config: null,
  configPath: "my_config.json",
  input: "",
  duration: 0,
  hasCache: false,
  previewTime: 1,
  inflight: null,
  version: null,
  canRestart: false,
  update: null,
  output: null,
  models: [],
};

const STORAGE_KEY = "capper.last";

function status(msg, kind) {
  const el = $("#status");
  el.textContent = msg;
  el.className = kind || "";
}

function get(obj, path) {
  return path.split(".").reduce((o, k) => (o ? o[k] : undefined), obj);
}
function set(obj, path, val) {
  const keys = path.split(".");
  let cur = obj;
  for (let i = 0; i < keys.length - 1; i++) cur = cur[keys[i]];
  cur[keys[keys.length - 1]] = val;
}

function persist() {
  localStorage.setItem(STORAGE_KEY, JSON.stringify({
    input: state.input,
    output: $("#output-path").value,
    previewTime: state.previewTime,
  }));
}
function restore() {
  try {
    const j = JSON.parse(localStorage.getItem(STORAGE_KEY) || "{}");
    if (j.input) { state.input = j.input; $("#input-path").value = j.input; }
    if (j.output) $("#output-path").value = j.output;
    if (typeof j.previewTime === "number") state.previewTime = j.previewTime;
  } catch (e) {}
}

function updateOutputs() {
  $$("[data-cfg]").forEach((el) => {
    const wrap = el.closest(".range");
    if (!wrap) return;
    const out = wrap.querySelector("output");
    if (out) {
      const v = parseFloat(el.value);
      out.textContent = Number.isInteger(v) ? v : v.toFixed(2);
    }
  });
  $$(".color-pair").forEach((p) => {
    const inp = p.querySelector("input[type=color]");
    const hex = p.querySelector(".hex");
    if (inp && hex) hex.textContent = inp.value.toUpperCase();
  });
}

function bindForm() {
  $$("[data-cfg]").forEach((el) => {
    const path = el.dataset.cfg;
    const v = get(state.config, path);
    if (v === undefined || v === null) return;
    if (el.type === "checkbox") el.checked = !!v;
    else el.value = v;

    el.addEventListener("input", () => {
      let val;
      if (el.type === "checkbox") val = el.checked;
      else if (el.type === "number" || el.type === "range") val = parseFloat(el.value);
      else if (el.tagName === "SELECT" && /^\d+$/.test(el.value)) val = parseInt(el.value, 10);
      else val = el.value;
      set(state.config, path, val);
      updateOutputs();
      toggleAnimSubfields();
      schedulePreview();
    });
  });
  updateOutputs();
  toggleAnimSubfields();
}

function toggleAnimSubfields() {
  const t = state.config?.animation?.type;
  $$(".anim-slide").forEach((el) => { el.style.display = t === "slide-in" ? "" : "none"; });
}

let previewTimer = null;
function schedulePreview(delay = 350) {
  clearTimeout(previewTimer);
  previewTimer = setTimeout(updatePreview, delay);
}

async function loadConfig() {
  const r = await fetch(`/api/config?path=${encodeURIComponent(state.configPath)}`);
  const j = await r.json();
  if (j.error) { status("config error: " + j.error, "err"); return; }
  state.config = j.config;
  state.configPath = j.path;
  if (!$("#output-path").value) $("#output-path").value = state.config.output_path || "";
  bindForm();
}

// Derive a sensible default output path next to the input video.
function deriveOutput(input) {
  const i = Math.max(input.lastIndexOf("/"), input.lastIndexOf("\\"));
  const dir = input.slice(0, i + 1);
  const name = input.slice(i + 1);
  const dot = name.lastIndexOf(".");
  const base = dot > 0 ? name.slice(0, dot) : name;
  return dir + base + "-captioned.mp4";
}

async function loadInput() {
  const input = $("#input-path").value.trim();
  if (!input) { status("choose a video first", "err"); return; }
  state.input = input;
  if (!$("#output-path").value.trim()) {
    $("#output-path").value = deriveOutput(input);
  }
  persist();

  $("#input-browse").disabled = true;
  try {
    status("loading video info…", "busy");
    const r = await fetch(`/api/info?input=${encodeURIComponent(input)}`);
    const j = await r.json().catch(() => ({ error: "invalid response from server" }));
    if (j.error) { status(j.error, "err"); return; }

    state.duration = j.duration;
    state.hasCache = j.has_cache;
    $("#time").max = j.duration.toFixed(2);
    $("#time").disabled = false;

    if (!state.hasCache) {
      status("transcribing audio… this only happens once", "busy");
      const ok = await transcribe();
      if (!ok) return;
    }

    if (state.previewTime > state.duration || state.previewTime < 0.5) {
      state.previewTime = Math.min(2, state.duration / 4);
    }
    $("#time").value = state.previewTime;
    $("#time-label").textContent = formatTime(state.previewTime);
    updatePreview();
  } catch (e) {
    status("load failed: " + e.message, "err");
    $("#spinner").hidden = true;
  } finally {
    $("#input-browse").disabled = false;
  }
}

let picking = false;
async function pickPath(mode, title, def) {
  if (picking) return null; // a dialog is already open
  picking = true;
  $("#input-browse").disabled = true;
  $("#output-browse").disabled = true;
  try {
    const r = await fetch("/api/pick", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ mode, title, default: def || "" }),
    });
    const j = await r.json().catch(() => ({ error: "dialog failed" }));
    if (j.error) { status(j.error, "err"); return null; }
    return j.canceled ? null : j.path;
  } finally {
    picking = false;
    $("#input-browse").disabled = false;
    $("#output-browse").disabled = false;
  }
}

async function browseInput() {
  status("file picker open — pick a video (check your taskbar if you don't see it)", "busy");
  const path = await pickPath("open", "Select input video", state.input || $("#input-path").value);
  if (!path) { status("ready", ""); return; }
  $("#input-path").value = path;
  $("#output-path").value = deriveOutput(path);
  persist();
  loadInput();
}

async function browseOutput() {
  const cur = $("#output-path").value || (state.input ? deriveOutput(state.input) : "");
  const path = await pickPath("save", "Save captioned video as", cur);
  if (!path) return;
  $("#output-path").value = path;
  persist();
}

async function revealOutput() {
  if (!state.output) return;
  const r = await fetch(`/api/reveal?path=${encodeURIComponent(state.output)}`);
  const j = await r.json().catch(() => ({}));
  if (j.error) status("could not open folder: " + j.error, "err");
}

function formatTime(t) {
  const m = Math.floor(t / 60), s = t % 60;
  return `${m}:${s.toFixed(2).padStart(5, "0")}`;
}

async function streamNDJSON(url, body, onEvent) {
  const r = await fetch(url, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(body),
  });
  if (!r.ok) {
    let err = `HTTP ${r.status}`;
    try { const j = await r.json(); if (j.error) err = j.error; } catch (e) {}
    throw new Error(err);
  }
  const reader = r.body.getReader();
  const decoder = new TextDecoder();
  let buf = "";
  while (true) {
    const { done, value } = await reader.read();
    if (done) break;
    buf += decoder.decode(value, { stream: true });
    let idx;
    while ((idx = buf.indexOf("\n")) >= 0) {
      const line = buf.slice(0, idx).trim();
      buf = buf.slice(idx + 1);
      if (line) {
        try { onEvent(JSON.parse(line)); } catch (e) {}
      }
    }
  }
  if (buf.trim()) {
    try { onEvent(JSON.parse(buf.trim())); } catch (e) {}
  }
}

function showProgress(stage) {
  $("#progress").hidden = false;
  setStage(stage, null);
}
function hideProgress() {
  $("#progress").hidden = true;
  $("#progress").classList.remove("indeterminate");
}
// value === null/undefined → indeterminate animation; a number → determinate %.
function setStage(stage, value) {
  $("#progress-stage").textContent = stage;
  if (value == null) {
    $("#progress").classList.add("indeterminate");
    $("#progress-pct").textContent = "";
  } else {
    $("#progress").classList.remove("indeterminate");
    const pct = Math.round(value * 100);
    $("#progress-fill").style.width = pct + "%";
    $("#progress-pct").textContent = pct + "%";
  }
}

async function transcribe(force = false) {
  let ok = false;
  showProgress("transcribing");
  try {
    await streamNDJSON("/api/transcribe", { config: state.config, input: state.input, force }, (e) => {
      if (e.type === "stage") setStage(e.stage, null);
      else if (e.type === "progress") setStage(e.stage, e.value);
      else if (e.type === "done") {
        state.hasCache = true;
        status(`transcribed ${e.words} words (${e.duration.toFixed(1)}s)`, "ok");
        ok = true;
      } else if (e.type === "error") {
        status("transcribe failed: " + e.error, "err");
      }
    });
  } catch (err) {
    status("transcribe failed: " + err.message, "err");
  } finally {
    hideProgress();
  }
  return ok;
}

async function updatePreview() {
  if (!state.config || !state.input || !state.hasCache) return;

  if (state.inflight) state.inflight.abort();
  state.inflight = new AbortController();

  $("#spinner").hidden = false;
  try {
    const r = await fetch("/api/preview", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ config: state.config, input: state.input, time: state.previewTime }),
      signal: state.inflight.signal,
    });
    if (!r.ok) {
      const j = await r.json().catch(() => ({}));
      status("preview error: " + (j.error || r.status), "err");
      return;
    }
    const blob = await r.blob();
    const url = URL.createObjectURL(blob);
    const img = $("#preview");
    const old = img.src;
    img.src = url;
    if (old.startsWith("blob:")) URL.revokeObjectURL(old);
    status(`preview at ${formatTime(state.previewTime)}`, "ok");
  } catch (e) {
    if (e.name !== "AbortError") status("preview failed: " + e.message, "err");
  } finally {
    $("#spinner").hidden = true;
  }
}

async function saveConfig() {
  const r = await fetch(`/api/config?path=${encodeURIComponent(state.configPath)}`, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(state.config),
  });
  const j = await r.json();
  status(j.error ? ("save error: " + j.error) : ("saved to " + j.path), j.error ? "err" : "ok");
}

async function generate() {
  if (!state.input) { status("set an input video first", "err"); return; }
  state.config.output_path = $("#output-path").value || state.config.output_path;
  persist();

  $("#generate-btn").disabled = true;
  status("starting…", "busy");
  showProgress("starting");
  const t0 = performance.now();
  try {
    await streamNDJSON("/api/generate", { config: state.config, input: state.input }, (e) => {
      if (e.type === "stage") {
        setStage(e.stage, null);
        status(e.stage + "…", "busy");
      } else if (e.type === "progress") {
        setStage(e.stage, e.value);
      } else if (e.type === "done") {
        const dt = ((performance.now() - t0) / 1000).toFixed(1);
        status(`✓ rendered in ${dt}s → ${e.output}`, "ok");
        state.hasCache = true;
        state.output = e.output;
        $("#reveal-btn").hidden = false;
      } else if (e.type === "error") {
        status("generate failed: " + e.error, "err");
      }
    });
  } catch (err) {
    status("generate failed: " + err.message, "err");
  } finally {
    hideProgress();
    $("#generate-btn").disabled = false;
  }
}

$("#load-btn").addEventListener("click", loadInput);
$("#input-browse").addEventListener("click", browseInput);
$("#output-browse").addEventListener("click", browseOutput);
$("#reveal-btn").addEventListener("click", revealOutput);
$("#save-btn").addEventListener("click", saveConfig);
$("#generate-btn").addEventListener("click", generate);
$("#retranscribe-btn").addEventListener("click", async () => {
  if (!state.input) { status("load a video first", "err"); return; }
  status("re-transcribing…", "busy");
  await transcribe(true);
  updatePreview();
});

$("#input-path").addEventListener("keydown", (e) => { if (e.key === "Enter") loadInput(); });
$("#input-path").addEventListener("change", () => { if ($("#input-path").value.trim()) loadInput(); });

$("#time").addEventListener("input", (e) => {
  state.previewTime = parseFloat(e.target.value);
  $("#time-label").textContent = formatTime(state.previewTime);
  persist();
  schedulePreview(120);
});

async function loadFonts() {
  try {
    const r = await fetch("/api/fonts");
    const j = await r.json();
    const sel = $("#font-family");
    const fonts = j.fonts || [];
    const current = state.config?.font?.family;
    if (current && !fonts.includes(current)) fonts.unshift(current);
    sel.innerHTML = fonts
      .map((f) => `<option value="${f.replace(/"/g, "&quot;")}">${f}</option>`)
      .join("");
    if (current) sel.value = current;
  } catch (e) {}
}

// --- version / self-update -------------------------------------------------

async function checkVersion() {
  try {
    const r = await fetch("/api/version", { cache: "no-store" });
    const j = await r.json();
    state.version = j.version;
    state.canRestart = !!j.can_restart;
    $("#version-tag").textContent = j.version ? (j.version === "dev" ? "dev" : j.version) : "";
  } catch (e) {}
}

async function checkUpdate() {
  try {
    const r = await fetch("/api/update", { cache: "no-store" });
    if (!r.ok) return;
    const j = await r.json();
    if (j.available) {
      state.update = j;
      const btn = $("#update-btn");
      btn.hidden = false;
      btn.textContent = `⬆ Update to ${j.latest}`;
      btn.title = j.notes ? j.notes.slice(0, 400) : `Update from ${j.current} to ${j.latest}`;
    }
  } catch (e) {}
}

async function runUpdate() {
  if (!state.update) return;
  const target = state.update.latest;
  const btn = $("#update-btn");
  btn.disabled = true;
  status(`updating to ${target}…`, "busy");
  showProgress("downloading");
  let restarting = false;
  try {
    await streamNDJSON("/api/update", {}, (e) => {
      if (e.type === "stage") setStage(e.stage, null);
      else if (e.type === "progress") setStage(e.stage, e.value);
      else if (e.type === "uptodate") {
        status("already up to date", "ok");
        btn.hidden = true;
      } else if (e.type === "done") {
        restarting = e.restarting;
      } else if (e.type === "error") {
        status("update failed: " + e.error, "err");
      }
    });
  } catch (err) {
    // A hard connection drop right after install can surface here even on success.
    restarting = state.canRestart;
  } finally {
    hideProgress();
  }

  if (restarting) {
    status(`restarting into ${target}…`, "busy");
    await waitForRestart(target);
  } else if (state.update) {
    status(`updated to ${target} — restart Capper to apply`, "ok");
    btn.hidden = true;
    btn.disabled = false;
  }
}

// Poll the server until it comes back on the new version, then reload the page.
async function waitForRestart(target) {
  const start = Date.now();
  while (Date.now() - start < 60000) {
    await new Promise((r) => setTimeout(r, 1000));
    try {
      const r = await fetch("/api/version", { cache: "no-store" });
      const j = await r.json();
      if (j.version === target || j.version !== state.version) {
        location.reload();
        return;
      }
    } catch (e) {
      // server still down between exit and relaunch — keep polling
    }
  }
  status("restart is taking a while — reload the page once Capper is back", "err");
}

$("#update-btn").addEventListener("click", runUpdate);

// --- speech model manager --------------------------------------------------

function modelSizeLabel(mb) {
  return mb >= 1000 ? (mb / 1000).toFixed(1) + " GB" : mb + " MB";
}

async function loadModels() {
  try {
    const r = await fetch("/api/models", { cache: "no-store" });
    const j = await r.json();
    state.models = j.models || [];
  } catch (e) {
    state.models = [];
  }
  renderModelSelect();
}

function renderModelSelect() {
  const sel = $("#model-select");
  const cur = state.config?.whisper?.model_path || "";
  sel.innerHTML = state.models
    .map((m) => `<option value="${m.file}">${m.downloaded ? "✓ " : "⬇ "}${m.name} — ${modelSizeLabel(m.size_mb)}</option>`)
    .join("");
  if (state.models.some((m) => m.file === cur)) {
    sel.value = cur;
  } else if (cur) {
    const opt = document.createElement("option");
    opt.value = cur;
    opt.textContent = cur + " (custom)";
    sel.appendChild(opt);
    sel.value = cur;
  }
  updateModelAction();
}

function currentModel() {
  return state.models.find((m) => m.file === $("#model-select").value);
}

function updateModelAction() {
  const m = currentModel();
  const btn = $("#model-download-btn");
  const hint = $("#model-hint");
  if (!m) {
    btn.hidden = true;
    hint.textContent = "custom model path";
  } else if (m.downloaded) {
    btn.hidden = true;
    hint.textContent = "✓ downloaded — active";
  } else {
    btn.hidden = false;
    btn.className = "ready";
    btn.textContent = `⬇ Download ${m.name} (${modelSizeLabel(m.size_mb)})`;
    hint.textContent = "not downloaded — pick a smaller model or download this one";
  }
}

// Persist the model choice quietly (no status spam) so it survives restarts.
async function persistModelChoice() {
  try {
    await fetch(`/api/config?path=${encodeURIComponent(state.configPath)}`, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(state.config),
    });
  } catch (e) {}
}

$("#model-select").addEventListener("change", () => {
  if (state.config?.whisper) state.config.whisper.model_path = $("#model-select").value;
  updateModelAction();
  persistModelChoice();
});

async function downloadModel() {
  const m = currentModel();
  if (!m) return;
  const btn = $("#model-download-btn");
  btn.disabled = true;
  $("#model-progress").hidden = false;
  $("#model-fill").style.width = "0%";
  $("#model-pct").textContent = "0%";
  status(`downloading ${m.name} (${modelSizeLabel(m.size_mb)})…`, "busy");
  try {
    await streamNDJSON("/api/models/download", { file: m.file }, (e) => {
      if (e.type === "progress") {
        const pct = Math.round(e.value * 100);
        $("#model-fill").style.width = pct + "%";
        $("#model-pct").textContent = pct + "%";
      } else if (e.type === "done") {
        status(`✓ ${m.name} ready`, "ok");
      } else if (e.type === "error") {
        status("download failed: " + e.error, "err");
      }
    });
    if (state.config?.whisper) state.config.whisper.model_path = m.file;
    await persistModelChoice();
    await loadModels();
    $("#model-select").value = m.file;
    updateModelAction();
  } catch (err) {
    status("download failed: " + err.message, "err");
  } finally {
    btn.disabled = false;
    $("#model-progress").hidden = true;
  }
}
$("#model-download-btn").addEventListener("click", downloadModel);

restore();
loadConfig().then(loadModels);
loadFonts();
checkVersion().then(checkUpdate);
