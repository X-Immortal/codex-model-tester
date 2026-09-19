const { app, BrowserWindow, Menu, Tray, dialog, ipcMain, nativeImage, session, shell } = require("electron");
const { spawn } = require("node:child_process");
const fs = require("node:fs");
const http = require("node:http");
const net = require("node:net");
const path = require("node:path");

const APP_ID = "dev.codexmodeltester.desktop";
const MAX_BACKEND_OUTPUT = 24_000;

let backend = null;
let backendExit = Promise.resolve();
let backendOutput = "";
let backendURL = "";
let mainWindow = null;
let tray = null;
let quitting = false;
let allowQuit = false;

if (!app.requestSingleInstanceLock()) {
  app.quit();
} else {
  app.on("second-instance", () => showMainWindow());
}

function appendBackendOutput(chunk) {
  backendOutput = (backendOutput + chunk.toString()).slice(-MAX_BACKEND_OUTPUT);
}

function findFreePort() {
  return new Promise((resolve, reject) => {
    const probe = net.createServer();
    probe.unref();
    probe.once("error", reject);
    probe.listen(0, "127.0.0.1", () => {
      const address = probe.address();
      probe.close((error) => {
        if (error) reject(error);
        else resolve(address.port);
      });
    });
  });
}

function packagedBackendPath() {
  const name = process.platform === "win32" ? "Codex-Model-Tester.exe" : "Codex-Model-Tester";
  return path.join(process.resourcesPath, "backend", name);
}

async function startBackend() {
  const port = await findFreePort();
  const executable = process.env.CODEX_BACKEND_PATH || packagedBackendPath();
  if (!fs.existsSync(executable)) throw new Error(`找不到后端程序：${executable}`);

  const dataDir = path.join(app.getPath("userData"), "data");
  fs.mkdirSync(dataDir, { recursive: true });
  backendURL = `http://127.0.0.1:${port}`;
  backend = spawn(executable, [], {
    cwd: path.dirname(executable),
    env: {
      ...process.env,
      CODEX_DESKTOP_PARENT: "1",
      DATA_DIR: dataDir,
      DEBUG_LOG_PAYLOADS: "false",
      OPEN_BROWSER: "false",
      PORT: String(port),
      PROXY_API_KEY: "",
    },
    stdio: ["pipe", "pipe", "pipe"],
    windowsHide: true,
  });
  backend.stdout.on("data", appendBackendOutput);
  backend.stderr.on("data", appendBackendOutput);
  backendExit = new Promise((resolve) => backend.once("exit", resolve));
  backend.once("error", appendBackendOutput);
  backend.once("exit", (code, signal) => {
    if (quitting) return;
    const detail = backendOutput.trim() || `退出码 ${code ?? "未知"}，信号 ${signal || "无"}`;
    dialog.showErrorBox("Codex Model Tester 后端已停止", detail);
    allowQuit = true;
    app.quit();
  });

  await waitForBackend();
}

function healthCheck() {
  return new Promise((resolve) => {
    const request = http.get(`${backendURL}/health/live`, { timeout: 800 }, (response) => {
      response.resume();
      resolve(response.statusCode >= 200 && response.statusCode < 300);
    });
    request.on("timeout", () => request.destroy());
    request.on("error", () => resolve(false));
  });
}

async function waitForBackend() {
  const deadline = Date.now() + 20_000;
  while (Date.now() < deadline) {
    if (backend && backend.exitCode !== null) break;
    if (await healthCheck()) return;
    await new Promise((resolve) => setTimeout(resolve, 160));
  }
  throw new Error(`后端启动失败。\n${backendOutput.trim()}`);
}

function iconPath() {
  return path.join(__dirname, "assets", "icon.png");
}

function isBackendPage(url) {
  try {
    return new URL(url).origin === backendURL;
  } catch {
    return false;
  }
}

function showMainWindow() {
  if (!mainWindow || mainWindow.isDestroyed()) return;
  if (mainWindow.isMinimized()) mainWindow.restore();
  mainWindow.show();
  mainWindow.focus();
}

function createMainWindow() {
  mainWindow = new BrowserWindow({
    width: 1440,
    height: 900,
    minWidth: 1040,
    minHeight: 700,
    show: false,
    autoHideMenuBar: process.platform !== "darwin",
    backgroundColor: "#e7e2f4",
    icon: iconPath(),
    title: "Codex Backend Model Tester",
    webPreferences: {
      contextIsolation: true,
      nodeIntegration: false,
      sandbox: true,
      preload: path.join(__dirname, "preload.js"),
    },
  });
  mainWindow.once("ready-to-show", () => mainWindow.show());
  mainWindow.on("close", (event) => {
    if (allowQuit || quitting) return;
    event.preventDefault();
    mainWindow.hide();
  });
  mainWindow.webContents.setWindowOpenHandler(({ url }) => {
    if (url.startsWith("https://")) void shell.openExternal(url);
    return { action: "deny" };
  });
  mainWindow.webContents.on("will-navigate", (event, url) => {
    if (isBackendPage(url)) return;
    event.preventDefault();
    if (url.startsWith("https://")) void shell.openExternal(url);
  });
  void mainWindow.loadURL(`${backendURL}/`);
}

function createTray() {
  const image = nativeImage.createFromPath(iconPath());
  tray = new Tray(image.resize({ width: process.platform === "darwin" ? 18 : 20 }));
  tray.setToolTip("Codex Backend Model Tester");
  tray.setContextMenu(Menu.buildFromTemplate([
    { label: "打开界面", click: showMainWindow },
    { type: "separator" },
    { label: "退出", click: () => void shutdownAndQuit() },
  ]));
  tray.on("double-click", showMainWindow);
}

function createApplicationMenu() {
  if (process.platform !== "darwin") {
    Menu.setApplicationMenu(null);
    return;
  }
  Menu.setApplicationMenu(Menu.buildFromTemplate([
    {
      label: app.name,
      submenu: [
        { role: "about" },
        { type: "separator" },
        { label: "打开界面", click: showMainWindow },
        { type: "separator" },
        { label: "退出", accelerator: "CmdOrCtrl+Q", click: () => void shutdownAndQuit() },
      ],
    },
    { role: "editMenu" },
    { role: "windowMenu" },
  ]));
}

async function shutdownAndQuit() {
  if (quitting) return;
  quitting = true;
  if (tray) tray.destroy();
  if (backend && backend.exitCode === null) {
    try {
      backend.stdin.end("shutdown\n");
    } catch {}
    const stopped = await Promise.race([
      backendExit.then(() => true),
      new Promise((resolve) => setTimeout(() => resolve(false), 4_000)),
    ]);
    if (!stopped && backend.exitCode === null) backend.kill();
  }
  allowQuit = true;
  app.quit();
}

ipcMain.handle("codex:open-external", async (_event, value) => {
  const target = new URL(String(value));
  if (target.protocol !== "https:" || target.hostname !== "auth.openai.com") {
    throw new Error("只允许打开 OpenAI 登录地址");
  }
  await shell.openExternal(target.toString());
});

app.setAppUserModelId(APP_ID);
app.on("before-quit", (event) => {
  if (allowQuit || quitting) return;
  event.preventDefault();
  void shutdownAndQuit();
});
app.on("activate", showMainWindow);

app.whenReady().then(async () => {
  session.defaultSession.setPermissionRequestHandler((webContents, permission, callback) => {
    callback(permission === "notifications" && isBackendPage(webContents.getURL()));
  });
  try {
    await startBackend();
    createApplicationMenu();
    createMainWindow();
    createTray();
  } catch (error) {
    dialog.showErrorBox("Codex Model Tester 启动失败", error instanceof Error ? error.message : String(error));
    allowQuit = true;
    app.quit();
  }
});
