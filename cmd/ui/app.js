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

async function loadInput() {
  const input = $("#input-path").value.trim();
  if (!input) { status("enter a video path first", "err"); return; }
  state.input = input;
  persist();

  $("#load-btn").disabled = true;
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
      status("transcribing audio… this only happens once (may take a minute)", "busy");
      $("#spinner").hidden = false;
      const ok = await transcribe();
      $("#spinner").hidden = true;
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
    $("#load-btn").disabled = false;
  }
}

function formatTime(t) {
  const m = Math.floor(t / 60), s = t % 60;
  return `${m}:${s.toFixed(2).padStart(5, "0")}`;
}

async function transcribe(force = false) {
  const r = await fetch("/api/transcribe", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ config: state.config, input: state.input, force }),
  });
  const j = await r.json();
  if (j.error) { status("transcribe failed: " + j.error, "err"); return false; }
  state.hasCache = true;
  status(`transcribed ${j.words} words (${j.duration.toFixed(1)}s)`, "ok");
  return true;
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
  status("rendering full video — this may take a minute…", "busy");
  const t0 = performance.now();
  try {
    const r = await fetch("/api/generate", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ config: state.config, input: state.input }),
    });
    const j = await r.json();
    if (j.error) { status("generate failed: " + j.error, "err"); return; }
    const dt = ((performance.now() - t0) / 1000).toFixed(1);
    status(`✓ rendered in ${dt}s → ${j.output}`, "ok");
  } finally {
    $("#generate-btn").disabled = false;
  }
}

$("#load-btn").addEventListener("click", loadInput);
$("#save-btn").addEventListener("click", saveConfig);
$("#generate-btn").addEventListener("click", generate);
$("#retranscribe-btn").addEventListener("click", async () => {
  if (!state.input) { status("load a video first", "err"); return; }
  status("re-transcribing…", "busy");
  await transcribe(true);
  updatePreview();
});

$("#input-path").addEventListener("keydown", (e) => { if (e.key === "Enter") loadInput(); });

$("#time").addEventListener("input", (e) => {
  state.previewTime = parseFloat(e.target.value);
  $("#time-label").textContent = formatTime(state.previewTime);
  persist();
  schedulePreview(120);
});

restore();
loadConfig();
