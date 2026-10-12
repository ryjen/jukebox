import test from 'node:test';
import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { runInNewContext } from 'node:vm';

const source = readFileSync(new URL('../cmd/jukebox/web/app.js', import.meta.url), 'utf8');

function deferred() {
  let resolve;
  let reject;
  const promise = new Promise((res, rej) => { resolve = res; reject = rej; });
  return { promise, resolve, reject };
}

function fixture() {
  const nodes = new Map();
  const storage = new Map([['jukebox-session', 'room-token'], ['jukebox-room', 'living']]);
  class Element {
    constructor() {
      this.textContent = '';
      this.children = [];
      this.value = '';
      this.handlers = new Map();
      this.classList = { add() {}, remove() {} };
    }
    replaceChildren(...children) { this.children = children; this.textContent = ''; }
    appendChild(child) { this.children.push(child); }
    append(...children) { this.children.push(...children); }
    addEventListener(type, handler) { this.handlers.set(type, handler); }
    focus() {}
  }
  const get = (name) => {
    if (!nodes.has(name)) nodes.set(name, new Element());
    return nodes.get(name);
  };
  const searches = [];
  const submitted = [];
  const context = {
    document: {
      getElementById: get,
      createElement: () => new Element(),
    },
    sessionStorage: {
      getItem: (key) => storage.get(key) || null,
      setItem: (key, val) => storage.set(key, val),
      removeItem: (key) => storage.delete(key),
    },
    fetch: async (path, options = {}) => {
      if (path === '/api/rooms') return { ok: true, json: async () => ({ rooms: [] }) };
      if (path.includes('/requests')) {
        if (options.method === 'POST') submitted.push(JSON.parse(options.body));
        return { ok: true, json: async () => ({ requests: [] }) };
      }
      if (!path.includes('/search?')) throw new Error('unexpected path ' + path);
      const pending = deferred();
      searches.push({ path, ...pending });
      return pending.promise;
    },
    console,
  };
  runInNewContext(source, context);
  return { get, searches, submitted };
}

async function spin() {
  for (let i = 0; i < 15; i++) await Promise.resolve();
}

test('the latest same-room search response wins even if older search finishes last', async () => {
  const { get, searches } = fixture();
  await spin();
  const onSearch = get('catalog-search-form').handlers.get('submit');
  assert.equal(typeof onSearch, 'function');

  get('catalog-query').value = 'first';
  const first = onSearch({ preventDefault() {} });
  get('catalog-query').value = 'second';
  const second = onSearch({ preventDefault() {} });
  assert.equal(searches.length, 2);

  searches[1].resolve({ ok: true, json: async () => ({ tracks: [{ uri: 'cloud://two', name: 'second track', artist: 'Artist', provider: 'cloud' }] }) });
  await second;
  assert.equal(get('catalog-results').children.length, 1);
  assert.match(get('catalog-results').children[0].textContent, /second track/);

  searches[0].resolve({ ok: true, json: async () => ({ tracks: [{ uri: 'local://one', name: 'stale first track', artist: 'Artist', provider: 'local' }] }) });
  await first;
  assert.equal(get('catalog-results').children.length, 1);
  assert.match(get('catalog-results').children[0].textContent, /second track/);
  assert.doesNotMatch(get('catalog-results').children[0].textContent, /stale/);
});

test('catalogue selection is submitted as an opaque ID, not mutable metadata', async () => {
  const { get, searches, submitted } = fixture();
  await spin();
  const onSearch = get('catalog-search-form').handlers.get('submit');
  const onRequest = get('request-form').handlers.get('submit');
  get('catalog-query').value = 'dreams';
  const pending = onSearch({ preventDefault() {} });
  searches[0].resolve({ ok: true, json: async () => ({
    tracks: [{ selectionId: 'signed-choice', name: 'Dreams', artist: 'Fleetwood Mac', provider: 'filesystem' }],
  }) });
  await pending;
  const choose = get('catalog-results').children[0].handlers.get('click');
  choose();
  await onRequest({ preventDefault() {} });
  assert.deepEqual(submitted[0], { selectionId: 'signed-choice' });

  get('catalog-query').value = 'second';
  const more = onSearch({ preventDefault() {} });
  searches[1].resolve({ ok: true, json: async () => ({
    tracks: [{ selectionId: 'other-choice', name: 'Second', artist: 'Artist', provider: 'qobuz' }],
  }) });
  await more;
  get('catalog-results').children[0].handlers.get('click')();
  get('title').value = 'User edited title';
  get('title').handlers.get('input')();
  get('source').value = 'local';
  await onRequest({ preventDefault() {} });
  assert.deepEqual(submitted[1], { title: 'User edited title', artist: 'Artist', source: 'local' });
});
