// Debt Reducer desktop shell. It starts the Go server as a hidden helper on
// a free local port and shows its pages in a window, so the desktop app looks
// and works exactly like the web app. The database lives in the folder chosen
// under Settings > Data (default: the app's data folder) unless
// DEBT_REDUCER_DB says otherwise.
const { app, BrowserWindow, dialog, ipcMain, shell } = require('electron');
const { spawn } = require('node:child_process');
const fs = require('node:fs');
const path = require('node:path');
const data = require('./data');
const { buildMenu, refreshMenu } = require('./menu');

const exe = process.platform === 'win32' ? 'debt-reducer.exe' : 'debt-reducer';

// serverPath is the bundled server when packaged, else bin/ from `make build`.
function serverPath() {
  if (app.isPackaged) return path.join(process.resourcesPath, 'server', exe);
  return path.join(__dirname, '..', 'bin', exe);
}

const configFile = () => path.join(app.getPath('userData'), 'config.json');
const logFile = () => path.join(app.getPath('userData'), 'debt-reducer.log');

function currentDb() {
  return data.dbPath({
    env: process.env.DEBT_REDUCER_DB,
    config: data.loadConfig(configFile()),
    defaultDir: app.getPath('userData'),
  });
}

let server = null;
let base = null; // the running server's URL; it changes when the server restarts
let win = null;
let quitting = false;

// startServer runs the Go server on port 0 with the given database and
// resolves with its base URL, read from the server's "listening" log line.
function startServer(db) {
  fs.mkdirSync(app.getPath('userData'), { recursive: true });
  const log = fs.createWriteStream(logFile(), { flags: 'a' });

  return new Promise((resolve, reject) => {
    const child = spawn(serverPath(), ['-addr', '127.0.0.1:0', '-db', db, '-exit-with-stdin'], {
      stdio: ['pipe', 'pipe', 'pipe'],
      windowsHide: true,
    });
    server = child;
    let found = false;
    let buffered = '';
    child.stdout.on('data', (chunk) => {
      log.write(chunk);
      if (found) return;
      buffered += chunk.toString();
      const m = buffered.match(/msg=listening addr=(\S+)/);
      if (m) {
        found = true;
        resolve(`http://${m[1]}`);
      }
    });
    child.stderr.on('data', (chunk) => log.write(chunk));
    child.on('error', reject);
    child.on('exit', (code) => {
      if (server === child) server = null;
      if (!found) {
        reject(new Error(`the server stopped before it started (exit ${code}); see ${log.path}`));
      } else if (!quitting && !child.stopping) {
        dialog.showErrorBox('Debt Reducer stopped', `The app's server stopped unexpectedly (exit ${code}).\nSee ${log.path}`);
        app.quit();
      }
    });
  });
}

// stopServer asks the server to shut down (it stops when stdin closes) and
// resolves once it has exited, so its database files are closed.
function stopServer() {
  const s = server;
  if (!s) return Promise.resolve();
  s.stopping = true;
  return new Promise((resolve) => {
    s.once('exit', resolve);
    s.stdin.end();
    setTimeout(() => s.exitCode === null && s.kill(), 5000).unref();
  });
}

// Window size and position are remembered between launches.
const boundsFile = () => path.join(app.getPath('userData'), 'window.json');
function loadBounds() {
  try {
    return JSON.parse(fs.readFileSync(boundsFile(), 'utf8'));
  } catch {
    return { width: 1280, height: 860 };
  }
}
function saveBounds(w) {
  try {
    fs.writeFileSync(boundsFile(), JSON.stringify({ ...w.getNormalBounds(), maximized: w.isMaximized() }));
  } catch {
    // not worth failing over
  }
}

const inside = (url) => url === base || url.startsWith(base + '/');

function createWindow() {
  const bounds = loadBounds();
  const w = new BrowserWindow({
    ...bounds,
    minWidth: 900,
    minHeight: 600,
    title: 'Debt Reducer',
    backgroundColor: '#0f1318', // --bg, so there's no white flash
    show: false,
    webPreferences: {
      contextIsolation: true,
      nodeIntegration: false,
      sandbox: true,
      preload: path.join(__dirname, 'preload.js'),
    },
  });
  if (bounds.maximized) w.maximize();
  w.once('ready-to-show', () => w.show());
  w.on('close', () => saveBounds(w));

  // Links that leave the app open in the system browser.
  w.webContents.setWindowOpenHandler(({ url }) => {
    if (!inside(url)) shell.openExternal(url);
    return { action: 'deny' };
  });
  w.webContents.on('will-navigate', (e, url) => {
    if (!inside(url)) {
      e.preventDefault();
      shell.openExternal(url);
    }
  });

  w.webContents.on('did-navigate', () => refreshMenu(base));

  w.loadURL(base + '/');
  return w;
}

function openDataFolder() {
  return shell.openPath(path.dirname(currentDb())).then((err) => (err ? { error: err } : {}));
}

// changeDataFolder asks for a folder and moves the database there: the
// server stops (closing the files), they're copied over, the server starts on
// the new copy and only then are the old files removed. If the folder already
// has data, the person picks between using it and replacing it (the old file
// is kept as a backup).
async function changeDataFolder() {
  if (process.env.DEBT_REDUCER_DB) {
    return { error: 'The data location is set by DEBT_REDUCER_DB, so it can\'t be changed here.' };
  }
  const from = currentDb();
  const picked = await dialog.showOpenDialog(win, {
    title: 'Choose a folder for your data',
    defaultPath: path.dirname(from),
    buttonLabel: 'Use this folder',
    properties: ['openDirectory', 'createDirectory'],
  });
  if (picked.canceled || picked.filePaths.length === 0) return {};
  const dir = picked.filePaths[0];
  const to = data.dbIn(dir);
  if (data.sameFile(from, to)) return {};

  try {
    fs.accessSync(dir, fs.constants.W_OK);
  } catch {
    return { error: `${dir} can't be written to. Pick another folder.` };
  }

  let mode = 'move';
  if (data.hasData(dir)) {
    const changed = data.lastChanged(to);
    const { response } = await dialog.showMessageBox(win, {
      type: 'question',
      title: 'Debt Reducer',
      message: 'That folder already has Debt Reducer data',
      detail: `${dir} has its own debts and plan${changed ? `, last changed ${changed.toLocaleDateString(undefined, { month: 'short', day: 'numeric', year: 'numeric' })}` : ''}.\n\n`
        + 'Use the data that\'s there, or replace it with the data you have now. Replacing keeps a backup of the old file next to it.',
      buttons: ['Use the data there', 'Replace with my data', 'Cancel'],
      defaultId: 0,
      cancelId: 2,
      noLink: true,
    });
    if (response === 2) return {};
    mode = response === 0 ? 'use' : 'replace';
  }

  const page = new URL(win.webContents.getURL() || base + '/');
  await stopServer();
  let backups = [];
  let written = [];
  try {
    if (mode === 'replace') backups = data.backup(to);
    if (mode !== 'use') written = data.copyData(from, to);
    base = await startServer(to);
  } catch (err) {
    // Put everything back and carry on with the old data.
    for (const f of written) fs.rmSync(f, { force: true });
    data.restore(backups);
    base = await startServer(from);
    win.loadURL(base + page.pathname + page.search);
    return { error: `Your data couldn't be moved to ${dir}: ${err.message}` };
  }

  data.saveConfig(configFile(), { ...data.loadConfig(configFile()), dataDir: dir });
  if (mode !== 'use') data.removeData(from);
  win.loadURL(base + page.pathname + page.search);
  return {};
}

// Only the app's own pages may use the desktop actions.
function fromApp(e) {
  return inside(e.senderFrame?.url || '');
}

if (!app.requestSingleInstanceLock()) {
  app.quit();
} else {
  app.on('second-instance', () => {
    if (!win) return;
    if (win.isMinimized()) win.restore();
    win.focus();
  });

  ipcMain.handle('data:choose', (e) => (fromApp(e) ? changeDataFolder() : { error: 'not allowed' }));
  ipcMain.handle('data:open', (e) => (fromApp(e) ? openDataFolder() : { error: 'not allowed' }));

  app.whenReady().then(async () => {
    try {
      base = await startServer(currentDb());
    } catch (err) {
      dialog.showErrorBox('Debt Reducer could not start', String(err.message || err));
      app.quit();
      return;
    }
    buildMenu({ getWindow: () => win, getBase: () => base, openDataFolder, logFile: logFile() });
    win = createWindow();
    app.on('activate', () => {
      if (BrowserWindow.getAllWindows().length === 0) win = createWindow();
    });
  });

  app.on('window-all-closed', () => {
    if (process.platform !== 'darwin') app.quit();
  });
  app.on('before-quit', () => {
    quitting = true;
    stopServer();
  });
}
