'use strict';
const el = (id) => document.getElementById(id);
let currentRoom = null;
let bearer = sessionStorage.getItem('jukebox-session');
const say = (message) => { el('message').textContent = message; };
const request = async (path, options = {}) => {
  const headers = { ...(options.body ? { 'Content-Type': 'application/json' } : {}), ...(bearer ? { Authorization: `Bearer ${bearer}` } : {}) };
  const response = await fetch(path, { ...options, headers, cache: 'no-store' });
  const body = await response.json().catch(() => ({}));
  if (!response.ok) throw new Error(body.error || `HTTP ${response.status}`);
  return body;
};
const reset = () => {
  sessionStorage.removeItem('jukebox-session');
  sessionStorage.removeItem('jukebox-room');
  bearer = null;
  currentRoom = null;
  el('live-queue').replaceChildren();
  el('catalog-results').replaceChildren();
  el('board').classList.add('hidden');
  el('join-panel').classList.remove('hidden');
};
async function loadRooms() {
  const { rooms } = await request('/api/rooms');
  el('rooms').replaceChildren(...rooms.map((r) => {
    const card = document.createElement('div');
    card.className = 'room';
    const name = document.createElement('strong');
    name.textContent = r.name;
    const hint = document.createElement('small');
    hint.textContent = 'Independent request board';
    card.append(name, hint);
    return card;
  }));
}
async function refresh() {
  if (!bearer || !currentRoom) return;
  try {
    const { requests } = await request(`/api/rooms/${encodeURIComponent(currentRoom)}/requests`);
    el('requests').replaceChildren(...requests.map((r) => {
      const div = document.createElement('div');
      div.className = 'item';
      const title = document.createElement('strong');
      title.textContent = r.title;
      const description = document.createElement('span');
      description.textContent = `${r.artist || 'Unknown artist'} · ${r.source} · request only`;
      div.append(title, description);
      return div;
    }));
    if (!requests.length) el('requests').textContent = 'No requests yet.';
  } catch (error) {
    say(error.message);
    if (error.message.includes('access denied')) reset();
  }
}
const joined = async (room) => {
  currentRoom = room;
  el('live-queue').replaceChildren();
  el('catalog-results').replaceChildren();
  sessionStorage.setItem('jukebox-room', room);
  el('board-heading').textContent = `Requests · ${room}`;
  el('join-panel').classList.add('hidden');
  el('board').classList.remove('hidden');
  await refresh();
};
el('join-form').addEventListener('submit', async (event) => {
  event.preventDefault();
  try {
    const { token, roomId } = await request('/api/sessions', { method: 'POST', body: JSON.stringify({ inviteCode: el('invite').value.trim() }) });
    bearer = token;
    sessionStorage.setItem('jukebox-session', token);
    el('invite').value = '';
    say('Room joined.');
    await joined(roomId);
  } catch (error) { say(error.message); }
});
el('request-form').addEventListener('submit', async (event) => {
  event.preventDefault();
  try {
    const values = { title: el('title').value, artist: el('artist').value, source: el('source').value };
    await request(`/api/rooms/${encodeURIComponent(currentRoom)}/requests`, { method: 'POST', body: JSON.stringify(values) });
    el('title').value = '';
    el('artist').value = '';
    say('Request recorded locally. No music was queued.');
    await refresh();
  } catch (error) { say(error.message); }
});
el('catalog-search-form').addEventListener('submit', async (event) => {
  event.preventDefault();
  if (!currentRoom || !bearer) return;
  const output = el('catalog-results');
  const room = currentRoom;
  const session = bearer;
  output.textContent = 'Searching configured music providers…';
  try {
    const q = el('catalog-query').value.trim();
    const { tracks } = await request(`/api/rooms/${encodeURIComponent(room)}/search?q=${encodeURIComponent(q)}`);
    if (room !== currentRoom || session !== bearer) return;
    output.replaceChildren();
    for (const track of tracks) {
      const button = document.createElement('button');
      button.type = 'button';
      button.className = 'secondary';
      button.textContent = `${track.name} — ${track.artist || 'Unknown artist'} (${track.provider || 'library'}) · Use as request`;
      button.addEventListener('click', () => {
        el('title').value = track.name;
        el('artist').value = track.artist || '';
        say('Search result copied to local request form. No music has been queued.');
        el('title').focus();
      });
      output.appendChild(button);
    }
    if (!tracks.length) output.textContent = 'No matching tracks.';
  } catch (error) {
    if (room === currentRoom && session === bearer) output.textContent = `Search unavailable: ${error.message}`;
  }
});
el('read-queue').addEventListener('click', async () => {
  if (!currentRoom || !bearer) return;
  const output = el('live-queue');
  const room = currentRoom;
  const session = bearer;
  output.textContent = 'Contacting the configured room player…';
  try {
    const snapshot = await request(`/api/rooms/${encodeURIComponent(room)}/queue`);
    if (room !== currentRoom || session !== bearer) return;
    const label = document.createElement('p');
    label.textContent = `Player: ${snapshot.queueId} · ${snapshot.state}`;
    const list = document.createElement('ol');
    for (const item of snapshot.items) {
      const li = document.createElement('li');
      li.textContent = item.name || 'Unknown track';
      list.appendChild(li);
    }
    output.replaceChildren(label, list);
  } catch (error) {
    if (room === currentRoom && session === bearer) output.textContent = `Playback preview unavailable: ${error.message}`;
  }
});
el('leave').addEventListener('click', () => { reset(); say('Local session cleared. Ask your host to revoke it on the server if needed.'); });
(async () => {
  try {
    await loadRooms();
    const saved = sessionStorage.getItem('jukebox-room');
    if (bearer && saved) await joined(saved);
  } catch (error) { say(error.message); }
})();
