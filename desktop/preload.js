// Gives the app's pages the few desktop-only actions they need. The Settings
// popup shows its Data buttons only when window.debtReducer exists.
const { contextBridge, ipcRenderer } = require('electron');

contextBridge.exposeInMainWorld('debtReducer', {
  // chooseDataFolder asks for a folder, moves the data there and restarts.
  // It resolves with {} (or {error}) and the page reloads on success.
  chooseDataFolder: () => ipcRenderer.invoke('data:choose'),
  openDataFolder: () => ipcRenderer.invoke('data:open'),
});
