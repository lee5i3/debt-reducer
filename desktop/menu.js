// The app menu. Page items load the same URLs the sidebar links to, so the
// menu needs nothing from the server beyond its base URL.
const { app, dialog, Menu, shell } = require('electron');

const isMac = process.platform === 'darwin';

// buildMenu sets the application menu. getWindow returns the main window
// (or null), getBase the server's current URL (e.g. http://127.0.0.1:41234;
// it changes when the data folder moves).
function buildMenu({ getWindow, getBase, openDataFolder, logFile }) {
  const win = () => getWindow();
  const go = (p) => () => win()?.loadURL(getBase() + p);

  // Settings opens as a popup over whatever page is showing.
  const openSettings = () => {
    const w = win();
    if (!w) return;
    const url = new URL(w.webContents.getURL() || getBase() + '/');
    url.searchParams.set('settings', '1');
    w.loadURL(url.toString());
  };

  const about = () =>
    dialog.showMessageBox(win(), {
      type: 'info',
      title: 'About Debt Reducer',
      message: 'Debt Reducer',
      detail: `Version ${app.getVersion()}\nElectron ${process.versions.electron}`,
    });

  const settingsItem = { label: isMac ? 'Settings…' : 'Settings', accelerator: 'CmdOrCtrl+,', click: openSettings };

  const template = [
    ...(isMac
      ? [{
          label: app.name,
          submenu: [
            { label: 'About Debt Reducer', click: about },
            { type: 'separator' },
            settingsItem,
            { type: 'separator' },
            { role: 'services' },
            { type: 'separator' },
            { role: 'hide' },
            { role: 'hideOthers' },
            { role: 'unhide' },
            { type: 'separator' },
            { role: 'quit' },
          ],
        }]
      : []),
    {
      label: '&File',
      submenu: [
        { label: 'Add Debt…', accelerator: 'CmdOrCtrl+N', click: go('/debts/new') },
        { id: 'plan-setup', label: 'Plan Setup…', click: go('/plan/setup') },
        ...(isMac ? [] : [{ type: 'separator' }, settingsItem]),
        { type: 'separator' },
        { label: 'Open Data Folder', click: () => openDataFolder() },
        { label: 'Open Log', click: () => shell.openPath(logFile) },
        { type: 'separator' },
        isMac ? { role: 'close' } : { role: 'quit', label: 'E&xit' },
      ],
    },
    { label: '&Edit', submenu: [
      { role: 'undo' }, { role: 'redo' }, { type: 'separator' },
      { role: 'cut' }, { role: 'copy' }, { role: 'paste' }, { role: 'selectAll' },
    ] },
    {
      label: '&Go',
      submenu: [
        { id: 'plan', label: 'Plan', accelerator: 'CmdOrCtrl+1', click: go('/') },
        { id: 'simulate', label: 'Simulate', accelerator: 'CmdOrCtrl+2', click: go('/simulate') },
        { label: 'Debts', accelerator: 'CmdOrCtrl+3', click: go('/debts') },
        { type: 'separator' },
        {
          label: 'Back',
          accelerator: isMac ? 'Cmd+[' : 'Alt+Left',
          click: () => win()?.webContents.navigationHistory.goBack(),
        },
        {
          label: 'Forward',
          accelerator: isMac ? 'Cmd+]' : 'Alt+Right',
          click: () => win()?.webContents.navigationHistory.goForward(),
        },
      ],
    },
    {
      label: '&View',
      submenu: [
        { role: 'reload' },
        { type: 'separator' },
        { role: 'resetZoom' },
        { role: 'zoomIn' },
        { role: 'zoomOut' },
        { type: 'separator' },
        { role: 'togglefullscreen' },
        ...(app.isPackaged ? [] : [{ type: 'separator' }, { role: 'toggleDevTools' }]),
      ],
    },
    { role: 'windowMenu' },
    {
      role: 'help',
      label: '&Help',
      submenu: [
        { label: 'Open in Browser', click: () => {
          const w = win();
          shell.openExternal(w ? w.webContents.getURL() : getBase() + '/');
        } },
        ...(isMac ? [] : [{ type: 'separator' }, { label: 'About Debt Reducer', click: about }]),
      ],
    },
  ];

  Menu.setApplicationMenu(Menu.buildFromTemplate(template));
}

// refreshMenu greys out Plan, Simulate and Plan Setup while there are no
// active debts, matching the sidebar. Called after every page load.
async function refreshMenu(base) {
  let canPlan = true;
  try {
    const debts = await (await fetch(base + '/api/debts')).json();
    canPlan = debts.some((d) => !d.inactive);
  } catch {
    // leave the menu as it is if the server can't be reached
    return;
  }
  const menu = Menu.getApplicationMenu();
  for (const id of ['plan', 'simulate', 'plan-setup']) {
    const item = menu?.getMenuItemById(id);
    if (item) item.enabled = canPlan;
  }
}

module.exports = { buildMenu, refreshMenu };
