async function load() {
 const status = document.querySelector('#status');
 const list = document.querySelector('#locations');
 list.replaceChildren(); status.textContent = 'Lagerorte werden geladen …';
 try {
  const response = await fetch('/api/v1/locations', {cache: 'no-store'});
  if (!response.ok) throw new Error('unavailable');
  const locations = await response.json();
  for (const location of locations) {
   const item = document.createElement('li');
   const name = document.createElement('strong'); name.textContent = location.name;
   const detail = document.createElement('p'); detail.textContent = `${location.site} · ${location.operator}`;
   item.append(name, detail); list.append(item);
  }
  status.textContent = locations.length ? `${locations.length} Lagerorte geladen.` : 'Noch keine Lagerorte vorhanden.';
 } catch {
  status.textContent = 'Daten konnten nicht geladen werden. Verbindung prüfen und erneut laden.';
 }
}
document.querySelector('#reload').addEventListener('click', load);
load();
