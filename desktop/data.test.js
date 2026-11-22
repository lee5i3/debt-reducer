const test = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const os = require('node:os');
const path = require('node:path');
const data = require('./data');

function tmp(t) {
  const dir = fs.mkdtempSync(path.join(os.tmpdir(), 'debt-reducer-data-'));
  t.after(() => fs.rmSync(dir, { recursive: true, force: true }));
  return dir;
}

function writeDb(db, text, { wal = true } = {}) {
  fs.mkdirSync(path.dirname(db), { recursive: true });
  fs.writeFileSync(db, text);
  if (wal) {
    fs.writeFileSync(db + '-wal', text + ' wal');
    fs.writeFileSync(db + '-shm', text + ' shm');
  }
}

test('dbPath: env wins, then the chosen folder, then the default', () => {
  assert.equal(data.dbPath({ env: '/x/my.db', config: { dataDir: '/chosen' }, defaultDir: '/def' }), '/x/my.db');
  assert.equal(data.dbPath({ env: '', config: { dataDir: '/chosen' }, defaultDir: '/def' }), path.join('/chosen', 'debt-reducer.db'));
  assert.equal(data.dbPath({ env: undefined, config: {}, defaultDir: '/def' }), path.join('/def', 'debt-reducer.db'));
});

test('config round-trips and a missing or broken file reads as empty', (t) => {
  const dir = tmp(t);
  const file = path.join(dir, 'sub', 'config.json');
  assert.deepEqual(data.loadConfig(file), {});
  data.saveConfig(file, { dataDir: '/somewhere' });
  assert.deepEqual(data.loadConfig(file), { dataDir: '/somewhere' });
  fs.writeFileSync(file, '{not json');
  assert.deepEqual(data.loadConfig(file), {});
  fs.writeFileSync(file, 'null');
  assert.deepEqual(data.loadConfig(file), {});
});

test('copyData copies the database with its -wal and -shm files', (t) => {
  const dir = tmp(t);
  const from = path.join(dir, 'a', 'debt-reducer.db');
  const to = path.join(dir, 'b', 'new', 'debt-reducer.db');
  writeDb(from, 'mine');
  const written = data.copyData(from, to);
  assert.deepEqual(written, [to, to + '-wal', to + '-shm']);
  assert.equal(fs.readFileSync(to + '-wal', 'utf8'), 'mine wal');
  assert.ok(fs.existsSync(from), 'copy leaves the original');

  const plain = path.join(dir, 'c', 'debt-reducer.db');
  writeDb(plain, 'no wal', { wal: false });
  assert.deepEqual(data.copyData(plain, path.join(dir, 'd', 'debt-reducer.db')), [path.join(dir, 'd', 'debt-reducer.db')]);
});

test('backup moves the files aside and restore puts them back', (t) => {
  const dir = tmp(t);
  const db = path.join(dir, 'debt-reducer.db');
  writeDb(db, 'theirs');
  const when = new Date(2026, 10, 22, 9, 5, 7);
  const moves = data.backup(db, when);
  assert.equal(moves.length, 3);
  assert.ok(!data.hasData(dir));
  assert.equal(fs.readFileSync(path.join(dir, 'debt-reducer.db.backup-20261122-090507'), 'utf8'), 'theirs');
  assert.ok(fs.existsSync(path.join(dir, 'debt-reducer.db.backup-20261122-090507-wal')));

  data.restore(moves);
  assert.equal(fs.readFileSync(db, 'utf8'), 'theirs');
  assert.equal(fs.readFileSync(db + '-shm', 'utf8'), 'theirs shm');
});

test('removeData deletes all three files and ignores missing ones', (t) => {
  const dir = tmp(t);
  const db = path.join(dir, 'debt-reducer.db');
  writeDb(db, 'x');
  data.removeData(db);
  assert.deepEqual(fs.readdirSync(dir), []);
  data.removeData(db); // nothing left: no error
});

test('sameFile and lastChanged', (t) => {
  const dir = tmp(t);
  assert.ok(data.sameFile(path.join(dir, 'a', '..', 'x.db'), path.join(dir, 'x.db')));
  assert.ok(!data.sameFile(path.join(dir, 'x.db'), path.join(dir, 'y.db')));
  assert.equal(data.lastChanged(path.join(dir, 'missing.db')), null);
  writeDb(path.join(dir, 'x.db'), 'x', { wal: false });
  assert.ok(data.lastChanged(path.join(dir, 'x.db')) instanceof Date);
});
