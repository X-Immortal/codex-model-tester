const { contextBridge, ipcRenderer } = require("electron");

contextBridge.exposeInMainWorld("codexDesktop", Object.freeze({
  openExternal: (url) => ipcRenderer.invoke("codex:open-external", url),
}));
