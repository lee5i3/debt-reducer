// Where the desktop app keeps its database, and moving it to another folder.
// No Electron here so it can be tested with `node --test`.
const fs = require('node:fs');
const path = require('node:path');

const DB_NAME = 'debt-reducer.db';
// SQLite in WAL mode keeps recent changes in -wal (and an index in -shm), so
// the three files always move together.
const PARTS = ['', '-wal', '-shm'];

const dbIn = (dir) => path.join(dir, DB_NAME);
const hasData = (dir) => fs.existsSync(dbIn(dir));
const sameFile = (a, b) => path.resolve(a) === path.resolve(b);

// The chosen folder is kept in config.json next to Electron's other files,
// since the database can't hold its own location.
function loadConfig(file) {
  try {
    const c = JSON.parse(fs.readFileSync(file, 'utf8'));
    return c && typeof c === 'object' ? c : {};
  } catch {
    return {};
  }
}

function saveConfig(file, config) {
  fs.mkdirSync(path.dirname(file), { recursive: true });
  const tmp = file + '.tmp';
  fs.writeFileSync(tmp, JSON.stringify(config, null, 2));
  fs.renameSync(tmp, file);
}

// dbPath is the database to use: DEBT_REDUCER_DB, else the chosen folder,
// else the app's own data folder.
function dbPath({ env, config, defaultDir }) {
  if (env) return env;
  if (config.dataDir) return dbIn(config.dataDir);
  return dbIn(defaultDir);
}

function stamp(d) {
  const p = (n) => String(n).padStart(2, '0');
  return `${d.getFullYear()}${p(d.getMonth() + 1)}${p(d.getDate())}-${p(d.getHours())}${p(d.getMinutes())}${p(d.getSeconds())}`;
}

// backup renames a database's files to <name>.backup-<time>, returning the
// renames so they can be undone.
function backup(db, when = new Date()) {
  const moves = [];
  for (const part of PARTS) {
    const from = db + part;
    if (!fs.existsSync(from)) continue;
    const to = `${db}.backup-${stamp(when)}${part}`;
    fs.renameSync(from, to);
    moves.push([from, to]);
  }
  return moves;
}

function restore(moves) {
  for (const [from, to] of [...moves].reverse()) fs.renameSync(to, from);
}

// copyData copies a database's files from one path to another and returns
// the files it wrote.
function copyData(from, to) {
  fs.mkdirSync(path.dirname(to), { recursive: true });
  const written = [];
  for (const part of PARTS) {
    if (!fs.existsSync(from + part)) continue;
    fs.copyFileSync(from + part, to + part);
    written.push(to + part);
  }
  return written;
}

function removeData(db) {
  for (const part of PARTS) fs.rmSync(db + part, { force: true });
}

function lastChanged(db) {
  try {
    return fs.statSync(db).mtime;
  } catch {
    return null;
  }
}

module.exports = {
  DB_NAME, dbIn, hasData, sameFile, loadConfig, saveConfig, dbPath,
  backup, restore, copyData, removeData, lastChanged,
};
