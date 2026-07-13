import "./style.css";

type AgentEvent = {
  type: "text" | "tool_use" | "tool_result" | "done" | "error";
  text?: string;
  name?: string;
  input?: unknown;
  ok?: boolean;
  summary?: string;
  stop_reason?: string;
  message?: string;
};

type AdminSession = {
  authenticated: boolean;
  auth_disabled: boolean;
  password_enabled: boolean;
  oidc_enabled: boolean;
};

const LOGIN_ERRORS: Record<string, string> = {
  oidc_not_configured: "Single sign-on is not configured.",
  oidc_unavailable: "The single sign-on provider is unreachable. Try again.",
  oidc_denied: "Single sign-on was cancelled or denied.",
  oidc_state: "Login session expired. Please try again.",
  oidc_verify: "Could not verify the single sign-on response.",
  oidc_forbidden: "This account is not allowed to sign in.",
};

type ChatMeta = { id: string; title: string; created_at: string; updated_at: string };

type TranscriptEntry = {
  role: "user" | "assistant" | "tool" | "error";
  text?: string;
  name?: string;
  ok?: boolean | null;
};

type Settings = {
  spotify_client_id: string;
  spotify_client_secret: string; // masked by the server
  anthropic_api_key: string; // masked by the server
  anthropic_model: string;
  anthropic_max_tokens: number;
};

type ModelInfo = { id: string; display_name: string; max_tokens: number };

const app = document.querySelector<HTMLDivElement>("#app")!;

let activeChatId: string | null = null;

function escapeHtml(s: string): string {
  const div = document.createElement("div");
  div.textContent = s;
  return div.innerHTML;
}

// --- admin login -----------------------------------------------------------

async function boot(): Promise<void> {
  try {
    const res = await fetch("/api/admin/session");
    const session = (await res.json()) as AdminSession;
    if (session.authenticated) {
      renderApp(session);
    } else {
      renderLogin(session);
    }
  } catch {
    app.innerHTML = `<div class="center"><p class="error-text">backend unreachable</p></div>`;
  }
}

function renderLogin(session: AdminSession): void {
  // Surface an error passed back from the OIDC redirect flow.
  const params = new URLSearchParams(window.location.search);
  const loginError = params.get("login_error");
  const initialMessage = loginError ? (LOGIN_ERRORS[loginError] ?? "Login failed.") : "";
  if (loginError) history.replaceState(null, "", "/");

  const ssoBlock = session.oidc_enabled
    ? `<a class="sso-btn" href="/api/admin/oidc/login">Sign in with SSO</a>`
    : "";
  const dividerBlock =
    session.oidc_enabled && session.password_enabled
      ? `<div class="or-divider"><span>or</span></div>`
      : "";
  const passwordBlock = session.password_enabled
    ? `<input id="login-password" type="password" placeholder="Password" autocomplete="current-password" />
       <button type="submit">Log in</button>`
    : "";

  app.innerHTML = `
    <div class="center">
      <form id="login-form" class="login">
        <h1><span>Aux</span></h1>
        <p>Admin login</p>
        ${ssoBlock}
        ${dividerBlock}
        ${passwordBlock}
        <p class="error-text" id="login-error">${escapeHtml(initialMessage)}</p>
      </form>
    </div>
  `;

  const form = document.querySelector<HTMLFormElement>("#login-form")!;
  const password = document.querySelector<HTMLInputElement>("#login-password");
  password?.focus();

  form.addEventListener("submit", async (e) => {
    e.preventDefault();
    if (!password) return; // SSO-only: nothing to submit
    const res = await fetch("/api/admin/login", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ password: password.value }),
    });
    if (res.ok) {
      boot();
    } else {
      document.querySelector("#login-error")!.textContent =
        res.status === 401 ? "Wrong password." : `Login failed (${res.status}).`;
      password.select();
    }
  });
}

// --- main app --------------------------------------------------------------

function renderApp(session: AdminSession): void {
  app.innerHTML = `
    <header>
      <button id="sidebar-toggle" class="icon-btn menu-btn" title="Chats" aria-label="Toggle chat list">☰</button>
      <h1><span class="brand">Aux</span><span class="subtitle"> — AI for your Spotify</span></h1>
      <div class="header-actions">
        <button id="settings-btn" class="icon-btn" title="Settings">⚙<span class="btn-label"> Settings</span></button>
        ${session.auth_disabled ? "" : `<button id="admin-logout" class="icon-btn" title="Log out">⏻<span class="btn-label"> Log out</span></button>`}
      </div>
      <div class="auth" id="auth">checking…</div>
    </header>
    <div class="workspace">
      <div id="sidebar-backdrop"></div>
      <aside id="sidebar">
        <button id="new-chat">+ New chat</button>
        <nav id="chat-list"></nav>
      </aside>
      <main class="chat-pane">
        <div id="messages"></div>
        <form id="chat-form">
          <input id="chat-input" type="text" autocomplete="off"
            placeholder="e.g. Make me a 90s rock playlist from my liked songs" />
          <button id="chat-send" type="submit">Send</button>
        </form>
      </main>
    </div>
    <dialog id="settings-modal">
      <form id="settings-form" method="dialog">
        <h2>Settings</h2>
        <label>Spotify client ID
          <input id="set-client-id" type="text" autocomplete="off" />
        </label>
        <label>Spotify client secret
          <input id="set-client-secret" type="password" autocomplete="new-password" />
        </label>
        <label>Anthropic API key
          <input id="set-anthropic-key" type="password" autocomplete="new-password" />
        </label>
        <label>Model
          <div class="model-row">
            <select id="set-model"></select>
            <button type="button" id="fetch-models" title="Fetch the latest available models">Fetch</button>
          </div>
        </label>
        <label>Max output tokens
          <input id="set-max-tokens" type="number" min="256" step="256" autocomplete="off" />
        </label>
        <p class="hint">Secrets stay blurred: leave a field empty to keep its current value. Pick a cheaper model or lower the token cap to save cost. Changes apply immediately.</p>
        <p class="error-text" id="settings-error"></p>
        <div class="modal-actions">
          <button type="button" id="settings-cancel">Cancel</button>
          <button type="submit" id="settings-save">Save</button>
        </div>
      </form>
    </dialog>
  `;

  wireAuth();
  wireChat();
  wireSettings();
  wireChatList();
  wireSidebar();

  const authError = new URLSearchParams(window.location.search).get("auth_error");
  if (authError) {
    addMessage("error", `Spotify login failed: ${authError}`);
    history.replaceState(null, "", "/");
  }

  document.querySelector<HTMLButtonElement>("#admin-logout")?.addEventListener("click", async () => {
    await fetch("/api/admin/logout", { method: "POST" });
    boot();
  });
}

// --- sidebar drawer (mobile) -------------------------------------------------

// setDrawer opens/closes the off-canvas sidebar on small screens. Harmless on
// desktop, where the sidebar is always visible and the backdrop is hidden.
function setDrawer(open: boolean): void {
  document.querySelector("#sidebar")?.classList.toggle("open", open);
  document.querySelector("#sidebar-backdrop")?.classList.toggle("visible", open);
}

function wireSidebar(): void {
  const sidebar = document.querySelector<HTMLElement>("#sidebar");
  document.querySelector("#sidebar-toggle")?.addEventListener("click", () => {
    setDrawer(!sidebar?.classList.contains("open"));
  });
  document.querySelector("#sidebar-backdrop")?.addEventListener("click", () => setDrawer(false));
}

async function wireAuth(): Promise<void> {
  const authEl = document.querySelector<HTMLDivElement>("#auth")!;
  try {
    const res = await fetch("/api/auth/status");
    if (res.status === 401) return boot();
    const data = await res.json();
    if (data.authenticated) {
      authEl.innerHTML = `<span>Connected as <strong>${escapeHtml(
        data.user.display_name || data.user.id,
      )}</strong></span>`;
    } else {
      authEl.innerHTML = `<button id="spotify-login">Connect Spotify</button>`;
      document.querySelector("#spotify-login")!.addEventListener("click", () => {
        window.location.href = "/api/auth/login";
      });
    }
  } catch {
    authEl.textContent = "backend unreachable";
  }
}

// --- chat list (sidebar) -----------------------------------------------------

async function wireChatList(): Promise<void> {
  document.querySelector("#new-chat")!.addEventListener("click", async () => {
    const res = await fetch("/api/chats", { method: "POST" });
    if (res.status === 401) return boot();
    const meta = (await res.json()) as ChatMeta;
    await selectChat(meta.id);
    await refreshChatList();
  });

  const chats = await refreshChatList();
  const remembered = localStorage.getItem("aux-active-chat");
  const initial =
    chats.find((c) => c.id === remembered)?.id ?? chats[0]?.id ?? null;
  if (initial) {
    await selectChat(initial);
  } else {
    document.querySelector("#new-chat")!.dispatchEvent(new Event("click"));
  }
}

async function refreshChatList(): Promise<ChatMeta[]> {
  const res = await fetch("/api/chats");
  if (res.status === 401) {
    boot();
    return [];
  }
  const data = (await res.json()) as { chats: ChatMeta[] };
  const list = document.querySelector<HTMLElement>("#chat-list");
  if (!list) return data.chats;

  list.innerHTML = "";
  for (const meta of data.chats) {
    const item = document.createElement("div");
    item.className = `chat-item${meta.id === activeChatId ? " active" : ""}`;

    const title = document.createElement("button");
    title.className = "chat-title";
    title.textContent = meta.title;
    title.title = meta.title;
    title.addEventListener("click", () => selectChat(meta.id));

    const rename = document.createElement("button");
    rename.className = "chat-action chat-rename";
    rename.textContent = "✎";
    rename.title = "Rename chat";
    rename.setAttribute("aria-label", "Rename chat");
    rename.addEventListener("click", (e) => {
      e.stopPropagation();
      startRename(title, meta);
    });

    const del = document.createElement("button");
    del.className = "chat-action chat-delete";
    del.textContent = "✕";
    del.title = "Delete chat";
    del.setAttribute("aria-label", "Delete chat");
    del.addEventListener("click", async (e) => {
      e.stopPropagation();
      if (!confirm(`Delete "${meta.title}"?`)) return;
      await fetch(`/api/chats/${meta.id}`, { method: "DELETE" });
      if (activeChatId === meta.id) {
        activeChatId = null;
        document.querySelector("#messages")!.innerHTML = "";
      }
      const remaining = await refreshChatList();
      if (!activeChatId) {
        if (remaining[0]) await selectChat(remaining[0].id);
        else document.querySelector("#new-chat")!.dispatchEvent(new Event("click"));
      }
    });

    item.append(title, rename, del);
    list.appendChild(item);
  }
  return data.chats;
}

async function selectChat(id: string): Promise<void> {
  const res = await fetch(`/api/chats/${id}`);
  if (res.status === 401) return boot();
  if (!res.ok) {
    await refreshChatList();
    return;
  }
  const data = (await res.json()) as {
    meta: ChatMeta;
    transcript: TranscriptEntry[] | null;
  };

  activeChatId = id;
  localStorage.setItem("aux-active-chat", id);

  const messagesEl = document.querySelector<HTMLDivElement>("#messages")!;
  messagesEl.innerHTML = "";
  for (const entry of data.transcript ?? []) {
    switch (entry.role) {
      case "user":
        addMessage("user", entry.text ?? "");
        break;
      case "assistant":
        addMessage("assistant", entry.text ?? "");
        break;
      case "error":
        addMessage("error", entry.text ?? "");
        break;
      case "tool": {
        const chip = addTool(entry.name ?? "tool");
        chip.className = `tool ${entry.ok == null ? "running" : entry.ok ? "ok" : "failed"}`;
        break;
      }
    }
  }
  await refreshChatList(); // re-render highlights with the new active chat
  setDrawer(false); // close the mobile drawer after picking a chat
  document.querySelector<HTMLInputElement>("#chat-input")?.focus();
}

// startRename swaps a chat's title button for an inline text field. Enter or
// blur saves via PATCH; Escape cancels. The list is refreshed either way.
function startRename(titleBtn: HTMLButtonElement, meta: ChatMeta): void {
  const input = document.createElement("input");
  input.className = "chat-rename-input";
  input.value = meta.title;
  titleBtn.replaceWith(input);
  input.focus();
  input.select();

  let settled = false;
  const finish = async (save: boolean) => {
    if (settled) return;
    settled = true;
    const next = input.value.trim();
    if (save && next && next !== meta.title) {
      await fetch(`/api/chats/${meta.id}`, {
        method: "PATCH",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ title: next }),
      });
    }
    await refreshChatList();
  };

  input.addEventListener("keydown", (e) => {
    if (e.key === "Enter") {
      e.preventDefault();
      finish(true);
    } else if (e.key === "Escape") {
      e.preventDefault();
      finish(false);
    }
  });
  input.addEventListener("blur", () => finish(true));
  // Clicks inside the input shouldn't bubble to the chat item.
  input.addEventListener("click", (e) => e.stopPropagation());
}

// --- settings modal ---------------------------------------------------------

function wireSettings(): void {
  const modal = document.querySelector<HTMLDialogElement>("#settings-modal")!;
  const form = document.querySelector<HTMLFormElement>("#settings-form")!;
  const errEl = document.querySelector<HTMLParagraphElement>("#settings-error")!;
  const clientId = document.querySelector<HTMLInputElement>("#set-client-id")!;
  const clientSecret = document.querySelector<HTMLInputElement>("#set-client-secret")!;
  const anthropicKey = document.querySelector<HTMLInputElement>("#set-anthropic-key")!;
  const modelSelect = document.querySelector<HTMLSelectElement>("#set-model")!;
  const fetchModels = document.querySelector<HTMLButtonElement>("#fetch-models")!;
  const maxTokens = document.querySelector<HTMLInputElement>("#set-max-tokens")!;

  // Ensures `id` is an option and selected; adds it if the list doesn't have
  // it yet (e.g. the configured model before any fetch).
  const ensureModelOption = (id: string, label?: string) => {
    if (!id) return;
    if (![...modelSelect.options].some((o) => o.value === id)) {
      modelSelect.add(new Option(label ?? id, id));
    }
    modelSelect.value = id;
  };

  document.querySelector("#settings-btn")!.addEventListener("click", async () => {
    errEl.textContent = "";
    clientSecret.value = "";
    anthropicKey.value = "";
    try {
      const res = await fetch("/api/admin/settings");
      if (res.status === 401) return boot();
      const s = (await res.json()) as Settings;
      clientId.value = s.spotify_client_id;
      clientSecret.placeholder = s.spotify_client_secret || "not set";
      anthropicKey.placeholder = s.anthropic_api_key || "not set";
      ensureModelOption(s.anthropic_model);
      maxTokens.value = String(s.anthropic_max_tokens || "");
      modal.showModal();
    } catch {
      errEl.textContent = "could not load settings";
    }
  });

  fetchModels.addEventListener("click", async () => {
    errEl.textContent = "";
    fetchModels.disabled = true;
    fetchModels.textContent = "Fetching…";
    try {
      const res = await fetch("/api/admin/models");
      if (res.status === 401) return boot();
      if (!res.ok) {
        const data = await res.json().catch(() => ({ error: `HTTP ${res.status}` }));
        errEl.textContent = data.error ?? "could not fetch models";
        return;
      }
      const { models } = (await res.json()) as { models: ModelInfo[] };
      const selected = modelSelect.value;
      modelSelect.innerHTML = "";
      for (const m of models) {
        modelSelect.add(new Option(`${m.display_name} (${m.id})`, m.id));
      }
      // Keep the previously-selected model selected if it's still offered.
      if (selected && [...modelSelect.options].some((o) => o.value === selected)) {
        modelSelect.value = selected;
      }
    } catch {
      errEl.textContent = "could not fetch models";
    } finally {
      fetchModels.disabled = false;
      fetchModels.textContent = "Fetch";
    }
  });

  document.querySelector("#settings-cancel")!.addEventListener("click", () => modal.close());

  form.addEventListener("submit", async (e) => {
    e.preventDefault();
    const body: Record<string, string | number> = {};
    if (clientId.value.trim()) body.spotify_client_id = clientId.value.trim();
    if (clientSecret.value.trim()) body.spotify_client_secret = clientSecret.value.trim();
    if (anthropicKey.value.trim()) body.anthropic_api_key = anthropicKey.value.trim();
    if (modelSelect.value) body.anthropic_model = modelSelect.value;
    const tokens = parseInt(maxTokens.value, 10);
    if (Number.isFinite(tokens) && tokens > 0) body.anthropic_max_tokens = tokens;
    if (Object.keys(body).length === 0) {
      modal.close();
      return;
    }
    const res = await fetch("/api/admin/settings", {
      method: "PUT",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(body),
    });
    if (res.ok) {
      modal.close();
      wireAuth(); // credentials changed — refresh the Spotify status
    } else if (res.status === 401) {
      boot();
    } else {
      const data = await res.json().catch(() => ({ error: `HTTP ${res.status}` }));
      errEl.textContent = data.error ?? "saving failed";
    }
  });
}

// --- chat -------------------------------------------------------------------

// copyText copies to the clipboard, falling back to a hidden textarea for
// non-secure contexts (plain HTTP on a LAN, where navigator.clipboard is
// unavailable). Returns whether it succeeded.
async function copyText(text: string): Promise<boolean> {
  try {
    if (navigator.clipboard && window.isSecureContext) {
      await navigator.clipboard.writeText(text);
      return true;
    }
  } catch {
    /* fall through to the legacy path */
  }
  try {
    const ta = document.createElement("textarea");
    ta.value = text;
    ta.style.position = "fixed";
    ta.style.opacity = "0";
    document.body.appendChild(ta);
    ta.select();
    const ok = document.execCommand("copy");
    ta.remove();
    return ok;
  } catch {
    return false;
  }
}

// addMessage appends a chat bubble. User and assistant bubbles carry a copy
// button; errors don't. Returns the container; its text lives in `.msg-text`.
function addMessage(cls: string, text = ""): HTMLDivElement {
  const messagesEl = document.querySelector<HTMLDivElement>("#messages")!;
  const div = document.createElement("div");
  div.className = `msg ${cls}`;

  const textEl = document.createElement("div");
  textEl.className = "msg-text";
  textEl.textContent = text;
  div.appendChild(textEl);

  if (cls === "user" || cls === "assistant") {
    const copy = document.createElement("button");
    copy.className = "copy-btn";
    copy.textContent = "⧉ Copy";
    copy.title = "Copy message";
    copy.addEventListener("click", async () => {
      const ok = await copyText(textEl.textContent ?? "");
      copy.textContent = ok ? "✓ Copied" : "Copy failed";
      setTimeout(() => (copy.textContent = "⧉ Copy"), 1500);
    });
    div.appendChild(copy);
  }

  messagesEl.appendChild(div);
  messagesEl.scrollTop = messagesEl.scrollHeight;
  return div;
}

function addTool(name: string): HTMLDivElement {
  const messagesEl = document.querySelector<HTMLDivElement>("#messages")!;
  const div = document.createElement("div");
  div.className = "tool running";
  div.textContent = name;
  messagesEl.appendChild(div);
  messagesEl.scrollTop = messagesEl.scrollHeight;
  return div;
}

// Parses a text/event-stream body from fetch (EventSource cannot POST).
async function streamChat(
  message: string,
  onEvent: (ev: AgentEvent) => void,
): Promise<void> {
  const res = await fetch("/api/chat", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ chat_id: activeChatId, message }),
  });
  if (res.status === 401) {
    boot();
    throw new Error("session expired — please log in again");
  }
  if (!res.ok || !res.body) {
    throw new Error(`chat request failed: ${res.status}`);
  }

  const reader = res.body.getReader();
  const decoder = new TextDecoder();
  let buffer = "";

  for (;;) {
    const { done, value } = await reader.read();
    if (done) break;
    buffer += decoder.decode(value, { stream: true });

    let sep: number;
    while ((sep = buffer.indexOf("\n\n")) !== -1) {
      const raw = buffer.slice(0, sep);
      buffer = buffer.slice(sep + 2);
      const dataLine = raw
        .split("\n")
        .find((line) => line.startsWith("data: "));
      if (!dataLine) continue;
      try {
        onEvent(JSON.parse(dataLine.slice(6)) as AgentEvent);
      } catch {
        // ignore malformed frames
      }
    }
  }
}

function wireChat(): void {
  const messagesEl = document.querySelector<HTMLDivElement>("#messages")!;
  const form = document.querySelector<HTMLFormElement>("#chat-form")!;
  const input = document.querySelector<HTMLInputElement>("#chat-input")!;
  const sendBtn = document.querySelector<HTMLButtonElement>("#chat-send")!;

  form.addEventListener("submit", async (e) => {
    e.preventDefault();
    const text = input.value.trim();
    if (!text || sendBtn.disabled || !activeChatId) return;

    input.value = "";
    sendBtn.disabled = true;
    addMessage("user", text);

    let assistantEl: HTMLDivElement | null = null;
    let currentTool: HTMLDivElement | null = null;

    try {
      await streamChat(text, (ev) => {
        switch (ev.type) {
          case "text": {
            if (!assistantEl) assistantEl = addMessage("assistant");
            const textEl = assistantEl.querySelector<HTMLElement>(".msg-text")!;
            textEl.textContent += ev.text ?? "";
            messagesEl.scrollTop = messagesEl.scrollHeight;
            break;
          }
          case "tool_use":
            assistantEl = null; // next text goes into a fresh bubble
            currentTool = addTool(ev.name ?? "tool");
            break;
          case "tool_result":
            if (currentTool) {
              currentTool.className = `tool ${ev.ok ? "ok" : "failed"}`;
              currentTool = null;
            }
            break;
          case "error":
            addMessage("error", ev.message ?? "something went wrong");
            break;
          case "done":
            break;
        }
      });
    } catch (err) {
      addMessage("error", err instanceof Error ? err.message : String(err));
    } finally {
      sendBtn.disabled = false;
      input.focus();
      refreshChatList(); // the first message sets the chat title
    }
  });

  input.focus();
}

boot();
