(() => {
  const state = {
    accounts: [],
    accountID: localStorage.getItem("codex-test-account") || "",
    models: [],
    heartbeats: [],
    heartbeatPage: 0,
    loginTimer: null,
    loginID: "",
    loginWindow: null,
    quotaRequestID: 0,
    testRequestID: 0,
    rawJSON: "",
  };

  const $ = (id) => document.getElementById(id);
  const accountsList = $("accounts-list");
  const accountCount = $("account-count");
  const modelSelect = $("model-select");
  const testButton = $("test-model");
  const selectedAccount = $("selected-account");
  const result = $("result");
  const rawEvent = $("raw-event");
  const rawEventPre = rawEvent.querySelector("pre");
  const toast = $("toast");
  const quotaContent = $("quota-content");
  const refreshQuotaButton = $("refresh-quota");
  const heartbeatList = $("heartbeat-list");
  const heartbeatInterval = $("heartbeat-interval");
  const heartbeatIntervalPicker = $("heartbeat-interval-picker");
  const addHeartbeatButton = $("add-heartbeat");
  const heartbeatCount = $("heartbeat-count");
  const heartbeatPagination = $("heartbeat-pagination");
  const heartbeatPage = $("heartbeat-page");
  const heartbeatPageDots = $("heartbeat-page-dots");
  const heartbeatNotificationsButton = $("heartbeat-notifications-button");
  const heartbeatAlertCount = $("heartbeat-alert-count");
  const heartbeatNotifications = $("heartbeat-notifications");
  const heartbeatNotificationList = $("heartbeat-notification-list");
  const heartbeatManager = $("heartbeat-manager");
  const heartbeatManagerList = $("heartbeat-manager-list");
  const heartbeatSelectAll = $("heartbeat-select-all");
  const deleteSelectedHeartbeatsButton = $("delete-selected-heartbeats");
  const heartbeatRefreshMilliseconds = 10000;
  let heartbeatLoadPromise = null;
  let heartbeatWheelDelta = 0;
  let heartbeatWheelResetTimer = null;
  const selectedHeartbeatIDs = new Set();
  const readHeartbeatAlertKeys = new Set(loadReadHeartbeatAlertKeys());

  function showToast(message, isError) {
    toast.textContent = message;
    toast.className = "toast " + (isError ? "error" : "visible");
    window.clearTimeout(showToast.timer);
    showToast.timer = window.setTimeout(() => { toast.className = "toast"; }, 3200);
  }

  async function api(path, options) {
    options = options || {};
    const headers = new Headers(options.headers || {});
    if (options.body) headers.set("Content-Type", "application/json");
    const response = await fetch(path, Object.assign({}, options, { headers, credentials: "same-origin" }));
    const text = await response.text();
    let body = {};
    if (text) {
      try { body = JSON.parse(text); } catch (_) { body = { message: text }; }
    }
    if (!response.ok) {
      const error = new Error(body.message || body.error || ("请求失败（" + response.status + "）"));
      error.status = response.status;
      error.code = body.error || "";
      error.retryAfterSeconds = body.retry_after_seconds || 0;
      error.accountRemoved = body.account_removed === true;
      throw error;
    }
    return body;
  }

  function accountName(account) {
    return account.label || account.email || account.upstream_account_id || account.id;
  }

  function loadReadHeartbeatAlertKeys() {
    try {
      const stored = JSON.parse(localStorage.getItem("codex-heartbeat-read-alerts") || "[]");
      return Array.isArray(stored) ? stored : [];
    } catch (_) {
      return [];
    }
  }

  function saveReadHeartbeatAlertKeys() {
    try {
      localStorage.setItem("codex-heartbeat-read-alerts", JSON.stringify(Array.from(readHeartbeatAlertKeys)));
    } catch (_) {
      // The current page can still clear notifications for this session.
    }
  }

  async function copyText(value, successMessage) {
    try {
      await navigator.clipboard.writeText(value);
      showToast(successMessage);
    } catch (_) {
      showToast("剪贴板访问被拒绝。", true);
    }
  }

  function accountInitial(account) {
    return (accountName(account).trim().charAt(0) || "?").toUpperCase();
  }

  function accountWarning(account) {
    if (account.status === "active") return account.eligible_now === false ? "冷却中" : "";
    return {
      disabled: "已停用",
      expired: "登录已过期",
      banned: "账号受限",
    }[account.status] || "账号不可用";
  }

  function updateSelectedAccountChip() {
    const current = state.accounts.find((account) => account.id === state.accountID);
    selectedAccount.replaceChildren();
    const dot = document.createElement("span");
    dot.className = "chip-dot";
    selectedAccount.append(dot, document.createTextNode(current ? accountName(current) : "未选择账号"));
  }

  function renderAccounts() {
    accountsList.replaceChildren();
    accountCount.textContent = String(state.accounts.length);
    updateSelectedAccountChip();

    if (!state.accounts.length) {
      const empty = document.createElement("div");
      empty.className = "empty-state compact";
      const glyph = document.createElement("span");
      glyph.className = "empty-glyph";
      glyph.textContent = "◎";
      const text = document.createElement("p");
      text.textContent = "还没有已登录账号，添加一个开始测试。";
      empty.append(glyph, text);
      accountsList.append(empty);
      return;
    }

    for (const account of state.accounts) {
      const card = document.createElement("article");
      card.className = "account-card" + (account.id === state.accountID ? " selected" : "");

      const selectTarget = document.createElement("button");
      selectTarget.type = "button";
      selectTarget.className = "account-select-target";
      selectTarget.title = accountName(account);
      selectTarget.setAttribute("aria-pressed", account.id === state.accountID ? "true" : "false");
      selectTarget.addEventListener("click", () => {
        if (account.id !== state.accountID) void selectAccount(account.id);
      });

      const avatar = document.createElement("span");
      avatar.className = "account-avatar";
      avatar.textContent = accountInitial(account);

      const identity = document.createElement("span");
      identity.className = "account-identity";
      const title = document.createElement("strong");
      title.className = "account-title";
      title.textContent = accountName(account);
      identity.append(title);

      const warning = accountWarning(account);
      if (warning) {
        const status = document.createElement("span");
        status.className = "account-status";
        const dot = document.createElement("span");
        dot.className = "status-dot muted-dot";
        status.append(dot, document.createTextNode(warning));
        identity.append(status);
      }
      selectTarget.append(avatar, identity);

      const logout = document.createElement("button");
      logout.type = "button";
      logout.className = "icon-button account-logout";
      logout.setAttribute("aria-label", "退出登录 " + accountName(account));
      logout.title = "退出登录";
      logout.innerHTML = '<svg viewBox="0 0 24 24" aria-hidden="true"><path d="M10 17l5-5-5-5M15 12H3M15 4h4a2 2 0 0 1 2 2v12a2 2 0 0 1-2 2h-4"/></svg>';
      logout.addEventListener("click", () => logoutAccount(account.id, logout));
      card.append(selectTarget, logout);
      accountsList.append(card);
    }
  }

  async function selectAccount(accountID) {
    state.testRequestID++;
    resetTestResult("已切换账号，请重新选择模型并测试。");
    state.accountID = accountID;
    localStorage.setItem("codex-test-account", accountID);
    renderAccounts();
    await loadModels();
    await loadQuota(false);
  }

  function resetTestResult(message) {
    result.className = "result empty";
    result.replaceChildren();
    const text = document.createElement("p");
    text.textContent = message || "选择模型后点击“测试”。";
    result.append(text);
    state.rawJSON = "";
    rawEventPre.textContent = "";
    rawEvent.open = false;
    rawEvent.className = "raw-event hidden";
    testButton.classList.remove("loading");
    testButton.removeAttribute("aria-busy");
    const label = testButton.querySelector(".test-button-label");
    if (label) label.textContent = "测试";
  }

  function clearQuota(message) {
    quotaContent.className = "quota-content empty-quota";
    quotaContent.replaceChildren();
    const glyph = document.createElement("span");
    glyph.className = "detail-glyph";
    glyph.textContent = "◌";
    quotaContent.append(glyph, document.createTextNode(message || "选择账号后加载额度。测试完成后会自动更新。"));
  }

  function formatQuotaReset(resetAt) {
    if (!resetAt) return "重置时间：上游未返回";
    const date = new Date(resetAt);
    if (Number.isNaN(date.getTime())) return "重置时间：上游未返回";
    return "重置于 " + date.toLocaleString();
  }

  function quotaWindowLabel(seconds) {
    if (!seconds) return "使用窗口";
    if (seconds >= 86400) return Math.round(seconds / 86400) + " 天窗口";
    if (seconds >= 3600) return Math.round(seconds / 3600) + " 小时窗口";
    return Math.round(seconds / 60) + " 分钟窗口";
  }

  function renderQuotaWindow(parent, title, window) {
    if (!window) return;
    const item = document.createElement("div");
    item.className = "quota-window";
    const head = document.createElement("div");
    head.className = "quota-window-head";
    const name = document.createElement("span");
    name.textContent = title;
    const windowName = document.createElement("small");
    windowName.textContent = quotaWindowLabel(window.limit_window_seconds);
    head.append(name, windowName);

    const used = typeof window.used_percent === "number" ? Math.max(0, Math.min(100, window.used_percent)) : null;
    const remaining = used === null ? null : Math.max(0, 100 - used);
    const track = document.createElement("div");
    track.className = "quota-track";
    const fill = document.createElement("div");
    fill.className = "quota-fill" + (remaining !== null && remaining <= 10 ? " danger" : remaining !== null && remaining <= 30 ? " warning" : "");
    fill.style.width = (remaining === null ? 0 : remaining) + "%";
    track.append(fill);

    const values = document.createElement("div");
    values.className = "quota-window-values";
    const remainingText = document.createElement("span");
    remainingText.className = "quota-remaining";
    remainingText.textContent = remaining === null ? "剩余：上游未返回" : remaining.toFixed(1) + "% 剩余";
    const usedText = document.createElement("small");
    usedText.textContent = used === null ? "使用率：上游未返回" : used.toFixed(1) + "% 已使用";
    values.append(remainingText, usedText);

    const foot = document.createElement("div");
    foot.className = "quota-foot";
    foot.textContent = formatQuotaReset(window.reset_at);
    item.append(head, track, values, foot);
    parent.append(item);
  }

  function renderQuota(body) {
    const quota = body.quota_runtime || body.cached_quota;
    if (!quota) {
      clearQuota("当前账号暂无可用额度数据，请点击“刷新额度”获取最新值。");
      return;
    }
    quotaContent.className = "quota-content loaded";
    quotaContent.replaceChildren();
    renderQuotaWindow(quotaContent, "主额度", quota.rate_limit);
    renderQuotaWindow(quotaContent, "次级额度", quota.secondary_rate_limit);
    renderQuotaWindow(quotaContent, "Code Review", quota.code_review_rate_limit);
    if (quota.credits) {
      const credit = document.createElement("div");
      credit.className = "quota-foot";
      credit.textContent = quota.credits.unlimited ? "Credits：无限" : "Credits：余额 " + (quota.credits.balance == null ? "未知" : quota.credits.balance);
      quotaContent.append(credit);
    }
    if (!quotaContent.children.length) clearQuota("上游返回了额度对象，但没有可显示的窗口。");
  }

  function quotaNeedsLiveRefresh(body) {
    const quota = body.quota_runtime || body.cached_quota;
    if (!quota) return true;
    const windows = [quota.rate_limit, quota.secondary_rate_limit, quota.code_review_rate_limit].filter(Boolean);
    if (!windows.length) return true;
    return windows.some((window) => {
      if (typeof window.used_percent !== "number") return true;
      if (!window.reset_at) return true;
      return Number.isNaN(new Date(window.reset_at).getTime());
    });
  }

  async function handleRemovedAccount(accountID, reason) {
    state.accounts = state.accounts.filter((account) => account.id !== accountID);
    if (state.accountID === accountID) {
      state.accountID = state.accounts[0] ? state.accounts[0].id : "";
      if (state.accountID) localStorage.setItem("codex-test-account", state.accountID);
      else localStorage.removeItem("codex-test-account");
    }
    renderAccounts();
    state.testRequestID++;
    resetTestResult("账号已移除，请重新选择模型并测试。");
    if (state.accountID) {
      await loadModels();
      await loadQuota(false);
    } else {
      clearModels();
      clearQuota();
    }
    await loadHeartbeats();
    showToast(reason || "账号 OAuth 凭据已失效，已自动删除。请重新添加账号。", true);
  }

  async function removeExpiredAccount(accountID, error) {
    if (!error.accountRemoved && error.code !== "upstream_unauthorized") return false;
    if (!error.accountRemoved) {
      try {
        await api("/admin/accounts/" + encodeURIComponent(accountID), { method: "DELETE" });
      } catch (_) {
        // The account may already have been removed by the backend.
      }
    }
    await handleRemovedAccount(accountID);
    return true;
  }

  async function loadQuota(fresh) {
    if (!state.accountID) {
      clearQuota();
      return;
    }
    const accountID = state.accountID;
    const requestID = ++state.quotaRequestID;
    refreshQuotaButton.disabled = true;
    refreshQuotaButton.textContent = fresh ? "刷新中…" : "加载中…";
    quotaContent.className = "quota-content empty-quota";
    quotaContent.textContent = fresh ? "正在刷新额度" : "正在加载额度";
    try {
      const suffix = fresh ? "" : "?cached=true";
      const body = await api("/admin/accounts/" + encodeURIComponent(accountID) + "/usage" + suffix);
      if (requestID !== state.quotaRequestID || accountID !== state.accountID) return;
      if (!fresh && quotaNeedsLiveRefresh(body)) {
        return loadQuota(true);
      }
      renderQuota(body);
    } catch (error) {
      if (requestID !== state.quotaRequestID || accountID !== state.accountID) return;
      if (await removeExpiredAccount(accountID, error)) return;
      const message = "额度获取失败：" + error.message;
      quotaContent.className = "quota-content";
      quotaContent.replaceChildren();
      const errorBox = document.createElement("div");
      errorBox.className = "quota-error";
      errorBox.textContent = message;
      quotaContent.append(errorBox);
      if (fresh) showToast(message, true);
    } finally {
      if (requestID === state.quotaRequestID) {
        refreshQuotaButton.disabled = false;
        refreshQuotaButton.textContent = "刷新";
      }
    }
  }

  function clearModels(message) {
    state.models = [];
    modelSelect.replaceChildren(new Option(message || "请先选择账号", ""));
    modelSelect.disabled = true;
    testButton.disabled = true;
    updateHeartbeatButton();
  }

  async function loadModels() {
    clearModels("正在加载账号模型");
    if (!state.accountID) return;
    try {
      const body = await api("/admin/accounts/" + encodeURIComponent(state.accountID) + "/models");
      state.models = body.models || [];
      modelSelect.replaceChildren(new Option(state.models.length ? "请选择模型" : "没有返回模型", ""));
      for (const model of state.models) {
        const label = model.id;
        modelSelect.append(new Option(label, model.id));
      }
      modelSelect.disabled = state.models.length === 0;
      updateHeartbeatButton();
    } catch (error) {
      if (await removeExpiredAccount(state.accountID, error)) return;
      clearModels("模型加载失败");
      updateHeartbeatButton();
      showToast(error.message, true);
    }
  }

  async function loadAccounts() {
    accountsList.innerHTML = '<div class="empty-state compact"><span class="empty-glyph">◌</span><p>正在加载账号</p></div>';
    try {
      const body = await api("/admin/accounts");
      state.accounts = body.accounts || [];
      if (!state.accounts.some((account) => account.id === state.accountID)) {
        state.accountID = state.accounts[0] ? state.accounts[0].id : "";
        if (state.accountID) localStorage.setItem("codex-test-account", state.accountID);
        else localStorage.removeItem("codex-test-account");
      }
      renderAccounts();
      await loadModels();
      await loadQuota(false);
      await loadHeartbeats();
    } catch (error) {
      state.accounts = [];
      renderAccounts();
      state.heartbeats = [];
      renderHeartbeats();
      showToast(error.message, true);
    }
  }

  function setDeviceLoginActive(active) {
    const addButton = $("add-account");
    addButton.disabled = active;
    addButton.textContent = active ? "登录进行中" : "添加账号";
  }

  function finishDeviceLogin() {
    const box = $("device-login");
    window.clearInterval(state.loginTimer);
    state.loginTimer = null;
    state.loginID = "";
    state.loginWindow = null;
    box.replaceChildren();
    box.className = "login-box hidden";
    setDeviceLoginActive(false);
  }

  function renderDeviceLoginActions(login) {
    const box = $("device-login");
    const actions = document.createElement("div");
    actions.className = "login-actions";

    const copyLink = document.createElement("button");
    copyLink.type = "button";
    copyLink.className = "button secondary";
    copyLink.textContent = "复制登录链接";
    copyLink.addEventListener("click", () => void copyText(login.auth_url, "登录链接已复制。"));

    const copyCode = document.createElement("button");
    copyCode.type = "button";
    copyCode.className = "button secondary";
    copyCode.textContent = "复制设备码";
    copyCode.addEventListener("click", () => void copyText(login.user_code, "设备码已复制。"));

    const cancel = document.createElement("button");
    cancel.type = "button";
    cancel.className = "button secondary login-cancel";
    cancel.textContent = "取消登录";
    cancel.addEventListener("click", () => void cancelDeviceLogin(cancel));

    actions.append(copyLink, copyCode, cancel);
    box.replaceChildren(actions);
    box.className = "login-box";
  }

  async function startDeviceLogin() {
    const box = $("device-login");
    const desktopOpenExternal = window.codexDesktop && typeof window.codexDesktop.openExternal === "function"
      ? window.codexDesktop.openExternal
      : null;
    const loginWindow = desktopOpenExternal ? null : window.open("about:blank", "_blank");
    if (loginWindow) loginWindow.opener = null;
    setDeviceLoginActive(true);
    box.className = "login-box hidden";
    box.replaceChildren();
    try {
      const login = await api("/admin/accounts/device-login/start", { method: "POST", body: "{}" });
      state.loginID = login.login_id;
      state.loginWindow = loginWindow;
      renderDeviceLoginActions(login);
      window.clearInterval(state.loginTimer);
      state.loginTimer = window.setInterval(() => pollDeviceLogin(login.login_id), 2000);
      if (desktopOpenExternal) {
        try {
          await desktopOpenExternal(login.auth_url);
        } catch (error) {
          showToast("登录页打开失败，可复制登录链接手动打开。", true);
        }
      } else if (loginWindow) {
        loginWindow.location.replace(login.auth_url);
      } else {
        showToast("登录页被浏览器拦截，可复制登录链接打开。", true);
      }
    } catch (error) {
      if (loginWindow && !loginWindow.closed) loginWindow.close();
      setDeviceLoginActive(false);
      box.className = "login-box error-box";
      box.textContent = error.message;
    }
  }

  async function cancelDeviceLogin(button) {
    const loginID = state.loginID;
    if (!loginID) {
      finishDeviceLogin();
      return;
    }
    window.clearInterval(state.loginTimer);
    button.disabled = true;
    try {
      await api("/admin/accounts/device-login/" + encodeURIComponent(loginID), { method: "DELETE" });
      if (state.loginWindow && !state.loginWindow.closed) state.loginWindow.close();
      finishDeviceLogin();
      showToast("已取消登录。");
    } catch (error) {
      if (error.status === 404) {
        finishDeviceLogin();
        showToast("登录已结束。");
        return;
      }
      button.disabled = false;
      state.loginTimer = window.setInterval(() => pollDeviceLogin(loginID), 2000);
      showToast(error.message, true);
    }
  }

  async function pollDeviceLogin(loginID) {
    try {
      const login = await api("/admin/accounts/device-login/" + encodeURIComponent(loginID));
      if (login.status === "ready") {
        finishDeviceLogin();
        showToast("账号已添加。");
        await loadAccounts();
      } else if (login.status === "error" || login.status === "expired") {
        window.clearInterval(state.loginTimer);
        state.loginID = "";
        state.loginWindow = null;
        setDeviceLoginActive(false);
        $("device-login").className = "login-box error-box";
        $("device-login").textContent = login.error || ("设备登录" + login.status);
      }
    } catch (error) {
      window.clearInterval(state.loginTimer);
      state.loginID = "";
      state.loginWindow = null;
      setDeviceLoginActive(false);
      $("device-login").className = "login-box error-box";
      $("device-login").textContent = error.message;
    }
  }

  async function logoutAccount(accountID, button) {
    if (!window.confirm("确定退出这个账号吗？这会撤销 OAuth 会话并删除本地凭据。")) return;
    button.disabled = true;
    button.textContent = "正在退出";
    try {
      await api("/admin/accounts/" + encodeURIComponent(accountID) + "/logout", { method: "POST" });
      if (state.accountID === accountID) {
        state.accountID = "";
        localStorage.removeItem("codex-test-account");
        clearModels();
        clearQuota();
        state.testRequestID++;
        resetTestResult("账号已退出，请重新选择模型并测试。");
      }
      await loadAccounts();
      showToast("已退出登录并撤销 OAuth 凭据。");
    } catch (error) {
      showToast("退出失败：" + error.message, true);
    } finally {
      button.disabled = false;
      button.textContent = "退出登录";
    }
  }


  function renderResult(body) {
    const requested = body.requested_model || "—";
    const upstream = body.upstream_model || "（缺失）";
    const match = body.match === true;
    result.className = "result verdict";
    result.replaceChildren();

    const verdictRow = document.createElement("div");
    verdictRow.className = "verdict-row";
    const mark = document.createElement("div");
    mark.className = "verdict-mark " + (match ? "match" : "mismatch");
    const verdict = document.createElement("strong");
    verdict.className = "verdict-label";
    verdict.textContent = match ? "MATCH" : "MISMATCH";
    mark.append(verdict);

    const time = document.createElement("div");
    time.className = "verdict-time";
    const timeValue = document.createElement("strong");
    timeValue.className = "metric-value";
    timeValue.textContent = body.timestamp ? new Date(body.timestamp).toLocaleString() : "—";
    time.append(timeValue);
    verdictRow.append(mark, time);

    const comparison = document.createElement("div");
    comparison.className = "comparison";
    for (const item of [["请求模型", requested], ["原始上游模型", upstream]]) {
      const cell = document.createElement("div");
      cell.className = "comparison-cell";
      const label = document.createElement("span");
      label.className = "comparison-label";
      label.textContent = item[0];
      const value = document.createElement("strong");
      value.className = "comparison-value result-value mono";
      value.title = item[1];
      value.textContent = item[1];
      cell.append(label, value);
      comparison.append(cell);
    }
    result.append(verdictRow, comparison);

    state.rawJSON = JSON.stringify(body.raw_event || {}, null, 2);
    rawEventPre.textContent = state.rawJSON;
    rawEvent.className = "raw-event";
  }

  async function testModel() {
    const model = modelSelect.value;
    if (!state.accountID || !model) return;

    const accountID = state.accountID;
    const requestID = ++state.testRequestID;

    const buttonLabel = testButton.querySelector(".test-button-label");
    testButton.disabled = true;
    testButton.classList.add("loading");
    testButton.setAttribute("aria-busy", "true");
    buttonLabel.textContent = "等待响应";
    result.className = "result loading";
    result.replaceChildren();
    const waiting = document.createElement("div");
    waiting.className = "loading-state";
    const spinner = document.createElement("span");
    spinner.className = "loading-spinner";
    spinner.setAttribute("aria-hidden", "true");
    const waitingText = document.createElement("span");
    waitingText.textContent = "正在等待 Codex 上游返回 response.completed";
    waiting.append(spinner, waitingText);
    result.append(waiting);
    rawEvent.className = "raw-event hidden";

    try {
      const body = await api("/admin/model-test", {
        method: "POST",
        body: JSON.stringify({ account_id: accountID, model }),
      });
      if (requestID !== state.testRequestID || accountID !== state.accountID) return;
      renderResult(body);
      void loadQuota(true);
    } catch (error) {
      if (requestID !== state.testRequestID || accountID !== state.accountID) return;
      result.className = "result error-box";
      result.textContent = formatModelTestError(error);
      if (!(await removeExpiredAccount(accountID, error))) {
        showToast(error.message, true);
      }
    } finally {
      if (requestID !== state.testRequestID || accountID !== state.accountID) return;
      testButton.classList.remove("loading");
      testButton.removeAttribute("aria-busy");
      buttonLabel.textContent = "测试";
      testButton.disabled = !modelSelect.value;
    }
  }

  function formatModelTestError(error) {
    const prefix = {
      quota_exhausted: "额度已用尽",
      rate_limited: "请求被限流",
      upstream_unauthorized: "账号授权已失效",
      upstream_error: "Codex 上游返回异常",
      account_not_ready: "账号暂时不可用",
    }[error.code] || "模型测试失败";
    const retry = error.retryAfterSeconds ? "\n建议等待 " + error.retryAfterSeconds + " 秒后重试。" : "";
    const removed = error.accountRemoved ? "\n账号 OAuth 凭据已失效，账号已自动删除，请重新添加账号。" : "";
    return prefix + "\n" + error.message + "\n错误代码：" + (error.code || "unknown") + " · HTTP " + (error.status || "unknown") + retry + removed + "\n本次请求已停止，不会切换到其他账号。";
  }

  async function copyRaw() {
    if (!state.rawJSON) return;
    try {
      await navigator.clipboard.writeText(state.rawJSON);
      showToast("原始事件已复制。");
    } catch (_) {
      showToast("剪贴板访问被拒绝。", true);
    }
  }

  function heartbeatStatus(monitor) {
    if (monitor.last_status === "healthy") return { label: "正常", className: "healthy" };
    if (monitor.last_status === "mismatch") return { label: "模型不一致", className: "mismatch" };
    if (monitor.last_status === "error") return { label: "测试失败", className: "error" };
    return { label: "待检查", className: "pending" };
  }

  function heartbeatIntervalLabel(seconds) {
    const labels = {
      60: "1–10 分钟",
      300: "5–15 分钟",
      600: "10–30 分钟",
      900: "15–45 分钟",
      1800: "30–90 分钟",
      3600: "1–3 小时",
      10800: "3–6 小时",
      21600: "6–12 小时",
      43200: "12–24 小时",
      86400: "24–48 小时",
    };
    return labels[Number(seconds)] || "随机间隔";
  }

  function compactDateTime(value) {
    if (!value) return "—";
    return new Intl.DateTimeFormat(undefined, {
      month: "2-digit",
      day: "2-digit",
      hour: "2-digit",
      minute: "2-digit",
      hour12: false,
    }).format(new Date(value));
  }

  function renderHeartbeats(direction) {
    heartbeatList.replaceChildren();
    heartbeatCount.textContent = String(state.heartbeats.length);
    renderHeartbeatNotifications();
    renderHeartbeatManager();
    if (!state.heartbeats.length) {
      state.heartbeatPage = 0;
      const empty = document.createElement("div");
      empty.className = "heartbeat-empty";
      empty.textContent = "尚未添加监测。";
      heartbeatList.append(empty);
      heartbeatPagination.classList.add("hidden");
      heartbeatPageDots.replaceChildren();
      return;
    }
    state.heartbeatPage = Math.min(Math.max(state.heartbeatPage, 0), state.heartbeats.length - 1);
    const monitor = state.heartbeats[state.heartbeatPage];
    const account = state.accounts.find((item) => item.id === monitor.account_id);
    const item = document.createElement("article");
    item.className = "heartbeat-item";
    if (direction) item.classList.add(direction > 0 ? "slide-from-right" : "slide-from-left");

      const accountLine = document.createElement("div");
      accountLine.className = "heartbeat-account";
      accountLine.textContent = account ? accountName(account) : "已删除账号";
      accountLine.title = accountLine.textContent;

      const avatar = document.createElement("span");
      avatar.className = "account-avatar heartbeat-avatar";
      avatar.textContent = account ? accountInitial(account) : "?";

      const title = document.createElement("strong");
      title.className = "heartbeat-model";
      title.textContent = monitor.model;
      const status = heartbeatStatus(monitor);
      const badge = document.createElement("span");
      badge.className = "heartbeat-status " + status.className;
      badge.textContent = status.label;
      const head = document.createElement("div");
      head.className = "heartbeat-item-head";
      const identityCopy = document.createElement("div");
      identityCopy.className = "heartbeat-identity-copy";
      identityCopy.append(accountLine, title);
      const identity = document.createElement("div");
      identity.className = "heartbeat-identity";
      identity.append(avatar, identityCopy);
      head.append(identity, badge);

      item.append(head);

      if (monitor.last_status === "mismatch") {
        const upstream = document.createElement("div");
        upstream.className = "heartbeat-upstream";
        upstream.textContent = "→ " + (monitor.last_upstream_model || "未返回模型");
        item.append(upstream);
      } else if (monitor.last_status === "error") {
        const error = document.createElement("div");
        error.className = "heartbeat-error-message";
        error.textContent = monitor.last_error || "检查失败";
        item.append(error);
      }

      const times = document.createElement("div");
      times.className = "heartbeat-times";
      appendHeartbeatTime(times, "上次检查", compactDateTime(monitor.last_run_at));
      appendHeartbeatTime(times, "下次检查", compactDateTime(monitor.next_run_at));
      item.append(times);

      const footer = document.createElement("div");
      footer.className = "heartbeat-footer";
      const actions = document.createElement("div");
      actions.className = "heartbeat-actions";
      const run = document.createElement("button");
      run.type = "button";
      run.className = "button secondary small-button";
      run.textContent = "立即检查";
      run.addEventListener("click", () => runHeartbeat(monitor.id, run));
      const remove = document.createElement("button");
      remove.type = "button";
      remove.className = "button small-button heartbeat-stop";
      remove.textContent = "停止";
      remove.addEventListener("click", () => deleteHeartbeat(monitor.id, remove));
      actions.append(run, remove);
      const interval = document.createElement("span");
      interval.className = "heartbeat-interval-badge";
      interval.textContent = heartbeatIntervalLabel(monitor.interval_seconds);
      footer.append(actions, interval);
      item.append(footer);
    heartbeatList.append(item);
    heartbeatPagination.classList.toggle("hidden", state.heartbeats.length <= 1);
    heartbeatPage.textContent = (state.heartbeatPage + 1) + " / " + state.heartbeats.length;
    heartbeatList.setAttribute("aria-label", "心跳监测 " + (state.heartbeatPage + 1) + " / " + state.heartbeats.length);
    heartbeatPageDots.replaceChildren();
    const dotTrack = document.createElement("span");
    dotTrack.className = "heartbeat-page-dot-track";
    for (let index = 0; index < 2; index++) {
      const spacer = document.createElement("span");
      spacer.className = "heartbeat-page-dot-spacer";
      dotTrack.append(spacer);
    }
    for (let index = 0; index < state.heartbeats.length; index++) {
      const dot = document.createElement("button");
      dot.type = "button";
      dot.className = "heartbeat-page-dot" + (index === state.heartbeatPage ? " active" : "");
      dot.setAttribute("aria-label", "查看第 " + (index + 1) + " 条监测");
      dot.setAttribute("aria-current", index === state.heartbeatPage ? "true" : "false");
      if (Math.abs(index - state.heartbeatPage) > 2) dot.tabIndex = -1;
      dot.addEventListener("click", () => goToHeartbeatPage(index));
      dotTrack.append(dot);
    }
    for (let index = 0; index < 2; index++) {
      const spacer = document.createElement("span");
      spacer.className = "heartbeat-page-dot-spacer";
      dotTrack.append(spacer);
    }
    const previousPage = direction ? state.heartbeatPage - direction : state.heartbeatPage;
    dotTrack.style.transform = "translateX(" + (-previousPage * 13) + "px)";
    heartbeatPageDots.append(dotTrack);
    if (direction) window.requestAnimationFrame(() => {
      dotTrack.style.transform = "translateX(" + (-state.heartbeatPage * 13) + "px)";
    });
  }

  function heartbeatAlerts() {
    return state.heartbeats.filter((monitor) => monitor.last_status === "mismatch" || monitor.last_status === "error");
  }

  function heartbeatAlertKey(monitor) {
    return [monitor.id, monitor.last_status, monitor.last_error_code || "", monitor.last_upstream_model || ""].join("|");
  }

  function syncReadHeartbeatAlertKeys() {
    const currentKeys = new Set(heartbeatAlerts().map(heartbeatAlertKey));
    let changed = false;
    for (const key of readHeartbeatAlertKeys) {
      if (currentKeys.has(key)) continue;
      readHeartbeatAlertKeys.delete(key);
      changed = true;
    }
    if (changed) saveReadHeartbeatAlertKeys();
  }

  function unreadHeartbeatAlerts() {
    syncReadHeartbeatAlertKeys();
    return heartbeatAlerts().filter((monitor) => !readHeartbeatAlertKeys.has(heartbeatAlertKey(monitor)));
  }

  function markHeartbeatNotificationsRead() {
    let changed = false;
    for (const monitor of heartbeatAlerts()) {
      const key = heartbeatAlertKey(monitor);
      if (readHeartbeatAlertKeys.has(key)) continue;
      readHeartbeatAlertKeys.add(key);
      changed = true;
    }
    if (changed) saveReadHeartbeatAlertKeys();
    renderHeartbeatNotifications();
  }

  function renderHeartbeatNotifications() {
    const alerts = unreadHeartbeatAlerts();
    heartbeatAlertCount.textContent = alerts.length > 99 ? "99+" : String(alerts.length);
    heartbeatAlertCount.classList.toggle("hidden", alerts.length === 0);
    heartbeatNotificationList.replaceChildren();

    if (!alerts.length) {
      const empty = document.createElement("div");
      empty.className = "notification-empty";
      empty.textContent = "暂无异常";
      heartbeatNotificationList.append(empty);
      return;
    }

    for (const monitor of alerts) {
      const account = state.accounts.find((item) => item.id === monitor.account_id);
      const notification = document.createElement("article");
      notification.className = "notification-item";
      const title = document.createElement("strong");
      title.textContent = (account ? accountName(account) : "已删除账号") + " · " + monitor.model;
      const message = document.createElement("div");
      message.className = "notification-message";
      message.textContent = monitor.last_status === "mismatch"
        ? "模型不一致：" + (monitor.last_upstream_model || "未返回模型")
        : (monitor.last_error || "检查失败");
      const time = document.createElement("time");
      time.textContent = compactDateTime(monitor.last_run_at);
      notification.append(title, message, time);
      heartbeatNotificationList.append(notification);
    }
  }

  function renderHeartbeatManager() {
    const currentIDs = new Set(state.heartbeats.map((monitor) => monitor.id));
    for (const id of selectedHeartbeatIDs) {
      if (!currentIDs.has(id)) selectedHeartbeatIDs.delete(id);
    }

    heartbeatManagerList.replaceChildren();
    heartbeatSelectAll.disabled = state.heartbeats.length === 0;
    heartbeatSelectAll.checked = state.heartbeats.length > 0 && selectedHeartbeatIDs.size === state.heartbeats.length;
    heartbeatSelectAll.indeterminate = selectedHeartbeatIDs.size > 0 && selectedHeartbeatIDs.size < state.heartbeats.length;
    deleteSelectedHeartbeatsButton.disabled = selectedHeartbeatIDs.size === 0;
    deleteSelectedHeartbeatsButton.textContent = selectedHeartbeatIDs.size
      ? "删除所选（" + selectedHeartbeatIDs.size + "）"
      : "删除所选";

    if (!state.heartbeats.length) {
      const empty = document.createElement("div");
      empty.className = "notification-empty";
      empty.textContent = "暂无监测";
      heartbeatManagerList.append(empty);
      return;
    }

    for (const monitor of state.heartbeats) {
      const account = state.accounts.find((item) => item.id === monitor.account_id);
      const item = document.createElement("label");
      item.className = "heartbeat-manager-item";
      const checkbox = document.createElement("input");
      checkbox.type = "checkbox";
      checkbox.checked = selectedHeartbeatIDs.has(monitor.id);
      checkbox.addEventListener("change", () => {
        if (checkbox.checked) selectedHeartbeatIDs.add(monitor.id);
        else selectedHeartbeatIDs.delete(monitor.id);
        renderHeartbeatManager();
      });
      const copy = document.createElement("span");
      copy.className = "heartbeat-manager-copy";
      const model = document.createElement("strong");
      model.textContent = monitor.model;
      const accountNameElement = document.createElement("span");
      accountNameElement.textContent = account ? accountName(account) : "已删除账号";
      copy.append(model, accountNameElement);
      item.append(checkbox, copy);
      heartbeatManagerList.append(item);
    }
  }

  function setHeartbeatManagerOpen(open) {
    if (open) setHeartbeatNotificationsOpen(false);
    heartbeatManager.classList.toggle("hidden", !open);
    heartbeatCount.setAttribute("aria-expanded", open ? "true" : "false");
    if (open) renderHeartbeatManager();
  }

  async function deleteSelectedHeartbeats() {
    const ids = Array.from(selectedHeartbeatIDs);
    if (!ids.length) return;
    if (!window.confirm("确定停止所选的 " + ids.length + " 个心跳监测吗？")) return;

    deleteSelectedHeartbeatsButton.disabled = true;
    deleteSelectedHeartbeatsButton.textContent = "正在删除…";
    const results = await Promise.allSettled(ids.map((id) =>
      api("/admin/heartbeats/" + encodeURIComponent(id), { method: "DELETE" })
    ));
    const removed = new Set();
    let failed = 0;
    results.forEach((result, index) => {
      if (result.status === "fulfilled") removed.add(ids[index]);
      else failed++;
    });
    state.heartbeats = state.heartbeats.filter((monitor) => !removed.has(monitor.id));
    for (const id of removed) selectedHeartbeatIDs.delete(id);
    state.heartbeatPage = Math.min(state.heartbeatPage, Math.max(0, state.heartbeats.length - 1));
    renderHeartbeats();
    if (failed) showToast("已删除 " + removed.size + " 个，" + failed + " 个删除失败。", true);
    else showToast("已停止所选监测。");
  }

  function setHeartbeatNotificationsOpen(open) {
    if (open) setHeartbeatManagerOpen(false);
    const wasOpen = !heartbeatNotifications.classList.contains("hidden");
    if (!open && wasOpen) markHeartbeatNotificationsRead();
    heartbeatNotifications.classList.toggle("hidden", !open);
    heartbeatNotificationsButton.setAttribute("aria-expanded", open ? "true" : "false");
    if (open) renderHeartbeatNotifications();
  }

  function goToHeartbeatPage(page) {
    if (page < 0 || page >= state.heartbeats.length || page === state.heartbeatPage) return false;
    const direction = page - state.heartbeatPage;
    state.heartbeatPage = page;
    renderHeartbeats(direction);
    return true;
  }

  function changeHeartbeatPage(offset) {
    return goToHeartbeatPage(state.heartbeatPage + offset);
  }

  function handleHeartbeatWheel(event) {
    if (state.heartbeats.length <= 1) return;
    const delta = Math.abs(event.deltaX) > Math.abs(event.deltaY) ? event.deltaX : event.deltaY;
    if (!delta) return;
    const direction = delta > 0 ? 1 : -1;
    const canMove = direction > 0
      ? state.heartbeatPage < state.heartbeats.length - 1
      : state.heartbeatPage > 0;
    if (!canMove) return;

    event.preventDefault();
    heartbeatWheelDelta += delta;
    window.clearTimeout(heartbeatWheelResetTimer);
    heartbeatWheelResetTimer = window.setTimeout(() => { heartbeatWheelDelta = 0; }, 180);
    if (Math.abs(heartbeatWheelDelta) < 90) return;

    const stepDirection = heartbeatWheelDelta > 0 ? 1 : -1;
    const requestedSteps = Math.floor(Math.abs(heartbeatWheelDelta) / 90);
    const targetPage = Math.max(0, Math.min(
      state.heartbeats.length - 1,
      state.heartbeatPage + stepDirection * requestedSteps
    ));
    const movedPages = Math.abs(targetPage - state.heartbeatPage);
    if (!movedPages) return;
    heartbeatWheelDelta -= stepDirection * movedPages * 90;
    goToHeartbeatPage(targetPage);
    if (targetPage === 0 || targetPage === state.heartbeats.length - 1) heartbeatWheelDelta = 0;
  }

  function appendHeartbeatTime(container, label, value) {
    const labelElement = document.createElement("span");
    labelElement.textContent = label;
    const valueElement = document.createElement("time");
    valueElement.textContent = value;
    container.append(labelElement, valueElement);
  }

  function closeHeartbeatIntervalPicker() {
    heartbeatInterval.value = "";
    heartbeatInterval.disabled = false;
    heartbeatIntervalPicker.classList.add("hidden");
    addHeartbeatButton.classList.remove("hidden");
  }

  function openHeartbeatIntervalPicker() {
    if (addHeartbeatButton.disabled) return;
    addHeartbeatButton.classList.add("hidden");
    heartbeatIntervalPicker.classList.remove("hidden");
    heartbeatInterval.focus();
    if (typeof heartbeatInterval.showPicker === "function") {
      try { heartbeatInterval.showPicker(); } catch (_) { /* Browser may block programmatic opening. */ }
    }
  }

  function updateHeartbeatButton() {
    addHeartbeatButton.disabled = !state.accountID || !modelSelect.value;
    if (addHeartbeatButton.disabled) closeHeartbeatIntervalPicker();
  }

  async function loadHeartbeats(silent) {
    if (heartbeatLoadPromise) return heartbeatLoadPromise;
    heartbeatLoadPromise = (async () => {
      try {
        const body = await api("/admin/heartbeats", { cache: "no-store" });
        const visibleID = state.heartbeats[state.heartbeatPage] && state.heartbeats[state.heartbeatPage].id;
        state.heartbeats = body.heartbeats || [];
        const preservedPage = visibleID ? state.heartbeats.findIndex((monitor) => monitor.id === visibleID) : -1;
        if (preservedPage >= 0) state.heartbeatPage = preservedPage;
        else state.heartbeatPage = Math.min(state.heartbeatPage, Math.max(0, state.heartbeats.length - 1));
        renderHeartbeats();
      } catch (error) {
        if (silent) return;
        state.heartbeats = [];
        renderHeartbeats();
        heartbeatList.textContent = "监测加载失败：" + error.message;
      }
    })();
    try {
      return await heartbeatLoadPromise;
    } finally {
      heartbeatLoadPromise = null;
    }
  }

  function startHeartbeatRefresh() {
    window.setInterval(() => {
      if (document.visibilityState === "visible") void loadHeartbeats(true);
    }, heartbeatRefreshMilliseconds);
    document.addEventListener("visibilitychange", () => {
      if (document.visibilityState === "visible") void loadHeartbeats(true);
    });
  }

  async function createHeartbeat() {
    const model = modelSelect.value;
    const intervalSeconds = Number(heartbeatInterval.value);
    if (!state.accountID || !model || !intervalSeconds) return;
    heartbeatInterval.disabled = true;
    try {
      const created = await api("/admin/heartbeats", {
        method: "POST",
        body: JSON.stringify({
          account_id: state.accountID,
          model,
          interval_seconds: intervalSeconds,
        }),
      });
      await loadHeartbeats();
      const createdPage = state.heartbeats.findIndex((monitor) => monitor.id === created.id);
      if (createdPage >= 0) {
        state.heartbeatPage = createdPage;
        renderHeartbeats();
      }
      showToast("已开始监测当前账号和模型。");
    } catch (error) {
      showToast("添加监测失败：" + error.message, true);
    } finally {
      closeHeartbeatIntervalPicker();
      updateHeartbeatButton();
    }
  }

  async function runHeartbeat(id, button) {
    button.disabled = true;
    button.textContent = "检查中…";
    try {
      const monitor = await api("/admin/heartbeats/" + encodeURIComponent(id) + "/run", { method: "POST" });
      await loadHeartbeats();
      const status = heartbeatStatus(monitor);
      showToast("检查完成：" + status.label, monitor.last_status !== "healthy");
    } catch (error) {
      showToast("检查失败：" + error.message, true);
    } finally {
      button.disabled = false;
      button.textContent = "立即检查";
    }
  }

  async function deleteHeartbeat(id, button) {
    if (!window.confirm("确定停止这个心跳监测吗？停止后将不再自动检查该账号和模型。")) return;
    button.disabled = true;
    try {
      await api("/admin/heartbeats/" + encodeURIComponent(id), { method: "DELETE" });
      state.heartbeats = state.heartbeats.filter((monitor) => monitor.id !== id);
      selectedHeartbeatIDs.delete(id);
      state.heartbeatPage = Math.min(state.heartbeatPage, Math.max(0, state.heartbeats.length - 1));
      renderHeartbeats();
      showToast("已停止监测。");
    } catch (error) {
      button.disabled = false;
      showToast("停止监测失败：" + error.message, true);
    }
  }

  $("add-account").addEventListener("click", startDeviceLogin);
  modelSelect.addEventListener("change", () => {
    state.testRequestID++;
    resetTestResult(modelSelect.value ? "已更换模型，请重新测试。" : "选择模型后点击“测试”。");
    testButton.disabled = !modelSelect.value;
    updateHeartbeatButton();
  });
  testButton.addEventListener("click", testModel);
  $("copy-raw").addEventListener("click", copyRaw);
  $("refresh-quota").addEventListener("click", () => loadQuota(true));
  addHeartbeatButton.addEventListener("click", openHeartbeatIntervalPicker);
  heartbeatInterval.addEventListener("change", createHeartbeat);
  heartbeatInterval.addEventListener("blur", () => {
    window.setTimeout(() => {
      if (!heartbeatInterval.value && !heartbeatInterval.disabled) closeHeartbeatIntervalPicker();
    }, 0);
  });
  heartbeatList.addEventListener("wheel", handleHeartbeatWheel, { passive: false });
  heartbeatList.addEventListener("keydown", (event) => {
    if (event.key !== "ArrowLeft" && event.key !== "ArrowRight") return;
    if (changeHeartbeatPage(event.key === "ArrowRight" ? 1 : -1)) event.preventDefault();
  });
  heartbeatNotificationsButton.addEventListener("click", () => {
    setHeartbeatNotificationsOpen(heartbeatNotifications.classList.contains("hidden"));
  });
  heartbeatCount.addEventListener("click", () => {
    setHeartbeatManagerOpen(heartbeatManager.classList.contains("hidden"));
  });
  $("close-heartbeat-notifications").addEventListener("click", () => setHeartbeatNotificationsOpen(false));
  $("close-heartbeat-manager").addEventListener("click", () => setHeartbeatManagerOpen(false));
  heartbeatSelectAll.addEventListener("change", () => {
    selectedHeartbeatIDs.clear();
    if (heartbeatSelectAll.checked) {
      for (const monitor of state.heartbeats) selectedHeartbeatIDs.add(monitor.id);
    }
    renderHeartbeatManager();
  });
  deleteSelectedHeartbeatsButton.addEventListener("click", deleteSelectedHeartbeats);
  document.addEventListener("click", (event) => {
    if (!heartbeatNotifications.classList.contains("hidden") &&
        !heartbeatNotifications.contains(event.target) &&
        !heartbeatNotificationsButton.contains(event.target)) {
      setHeartbeatNotificationsOpen(false);
    }
    if (!heartbeatManager.classList.contains("hidden") &&
        !heartbeatManager.contains(event.target) &&
        !heartbeatCount.contains(event.target)) {
      setHeartbeatManagerOpen(false);
    }
  });
  document.addEventListener("keydown", (event) => {
    if (event.key !== "Escape") return;
    setHeartbeatNotificationsOpen(false);
    setHeartbeatManagerOpen(false);
  });
  startHeartbeatRefresh();
  loadAccounts();
})();
