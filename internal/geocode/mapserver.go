package geocode

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os/exec"
	"runtime"
)

type MapResult struct {
	Lat float64 `json:"lat"`
	Lon float64 `json:"lon"`
}

// PickFromMap opens a browser with an interactive map and returns the selected coordinates.
func PickFromMap(title string, initialLat, initialLon float64) (*MapResult, error) {
	resultCh := make(chan MapResult, 1)

	mux := http.NewServeMux()

	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, mapHTML(title, initialLat, initialLon))
	})

	mux.HandleFunc("/confirm", func(w http.ResponseWriter, r *http.Request) {
		var result MapResult
		if err := json.NewDecoder(r.Body).Decode(&result); err != nil {
			http.Error(w, "bad request", 400)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
		resultCh <- result
	})

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, fmt.Errorf("start server: %w", err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	url := fmt.Sprintf("http://127.0.0.1:%d", port)

	server := &http.Server{Handler: mux}
	go server.Serve(listener)

	openBrowser(url)

	result := <-resultCh
	server.Shutdown(context.Background())

	return &result, nil
}

func openBrowser(url string) {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", url)
	case "linux":
		cmd = exec.Command("xdg-open", url)
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	}
	if cmd != nil {
		cmd.Start()
	}
}

func mapHTML(title string, lat, lon float64) string {
	return fmt.Sprintf(`<!DOCTYPE html>
<html>
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>woffuk — %s</title>
<link rel="stylesheet" href="https://unpkg.com/leaflet@1.9.4/dist/leaflet.css"/>
<script src="https://unpkg.com/leaflet@1.9.4/dist/leaflet.js"></script>
<style>
  * { margin: 0; padding: 0; box-sizing: border-box; }
  body { font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, sans-serif; background: #111; color: #e0e0e0; overflow: hidden; height: 100vh; }

  /* Search */
  #search-wrapper { position: absolute; top: 16px; left: 50%%; transform: translateX(-50%%); z-index: 1000; width: 440px; }
  #search-box { display: flex; background: #1e1e2e; border-radius: 12px; box-shadow: 0 4px 24px rgba(0,0,0,0.5); overflow: hidden; border: 1px solid #333; transition: border-color 0.2s; }
  #search-box:focus-within { border-color: #c084fc; }
  #search-input { flex: 1; padding: 12px 16px; border: none; background: transparent; color: #e0e0e0; font-size: 15px; outline: none; }
  #search-input::placeholder { color: #666; }
  #search-clear { padding: 12px; background: transparent; border: none; color: #666; cursor: pointer; font-size: 18px; display: none; }
  #search-clear:hover { color: #e0e0e0; }
  #suggestions { position: absolute; top: 52px; left: 0; right: 0; background: #1e1e2e; border-radius: 12px; box-shadow: 0 4px 24px rgba(0,0,0,0.5); border: 1px solid #333; display: none; overflow: hidden; max-height: 320px; overflow-y: auto; }
  .suggestion { padding: 12px 16px; cursor: pointer; font-size: 14px; border-bottom: 1px solid #2a2a3e; display: flex; align-items: flex-start; gap: 10px; }
  .suggestion:last-child { border-bottom: none; }
  .suggestion:hover { background: #2a2a3e; }
  .suggestion-icon { color: #c084fc; font-size: 16px; margin-top: 2px; flex-shrink: 0; }
  .suggestion-text { flex: 1; }
  .suggestion-name { color: #e0e0e0; }
  .suggestion-detail { color: #666; font-size: 12px; margin-top: 2px; }
  .suggestion-loading { padding: 16px; text-align: center; color: #666; font-size: 14px; }

  /* Map */
  #map { height: 100vh; width: 100vw; }
  .leaflet-container { background: #111; }

  /* My location button */
  #locate-btn { position: absolute; bottom: 100px; right: 16px; z-index: 1000; width: 44px; height: 44px; border-radius: 12px; border: 1px solid #333; background: #1e1e2e; color: #e0e0e0; font-size: 20px; cursor: pointer; display: flex; align-items: center; justify-content: center; box-shadow: 0 2px 12px rgba(0,0,0,0.4); transition: all 0.2s; }
  #locate-btn:hover { background: #2a2a3e; border-color: #c084fc; }
  #locate-btn.locating { animation: pulse 1s infinite; }
  @keyframes pulse { 0%%,100%% { opacity: 1; } 50%% { opacity: 0.5; } }

  /* Bottom bar */
  #bottom-bar { position: absolute; bottom: 0; left: 0; right: 0; z-index: 1000; padding: 16px 20px; background: linear-gradient(transparent, #111 30%%); pointer-events: none; display: flex; align-items: flex-end; justify-content: space-between; }
  #coord-display { pointer-events: auto; background: #1e1e2e; border-radius: 10px; padding: 10px 16px; border: 1px solid #333; font-family: 'SF Mono', 'Fira Code', monospace; font-size: 14px; color: #a0a0a0; box-shadow: 0 2px 12px rgba(0,0,0,0.4); }
  #confirm-btn { pointer-events: auto; padding: 12px 36px; border-radius: 12px; border: none; background: #c084fc; color: #111; font-weight: 700; font-size: 15px; cursor: pointer; box-shadow: 0 2px 16px rgba(192,132,252,0.3); transition: all 0.2s; letter-spacing: 0.3px; }
  #confirm-btn:hover { background: #a855f7; transform: translateY(-1px); box-shadow: 0 4px 20px rgba(192,132,252,0.4); }

  /* Done screen */
  #done-screen { display: none; position: fixed; inset: 0; background: #111; z-index: 9999; align-items: center; justify-content: center; flex-direction: column; gap: 8px; }
  #done-screen.visible { display: flex; }
  #done-icon { font-size: 48px; color: #22c55e; }
  #done-text { font-size: 18px; color: #888; }
</style>
</head>
<body>

<div id="search-wrapper">
  <div id="search-box">
    <input id="search-input" type="text" placeholder="Search for an address..." autocomplete="off" spellcheck="false"/>
    <button id="search-clear" onclick="clearSearch()">&times;</button>
  </div>
  <div id="suggestions"></div>
</div>

<div id="map"></div>

<button id="locate-btn" onclick="locateMe()" title="My location">
  <svg width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><circle cx="12" cy="12" r="4"/><line x1="12" y1="2" x2="12" y2="6"/><line x1="12" y1="18" x2="12" y2="22"/><line x1="2" y1="12" x2="6" y2="12"/><line x1="18" y1="12" x2="22" y2="12"/></svg>
</button>

<div id="bottom-bar">
  <div id="coord-display">—</div>
  <button id="confirm-btn" onclick="confirmLocation()">Confirm</button>
</div>

<div id="done-screen">
  <div id="done-icon">&#10003;</div>
  <div id="done-text">Location confirmed — you can close this tab</div>
</div>

<script>
// ── Map setup ──
const map = L.map('map', { zoomControl: false }).setView([%.6f, %.6f], 13);

L.control.zoom({ position: 'topright' }).addTo(map);

L.tileLayer('https://{s}.basemaps.cartocdn.com/dark_all/{z}/{x}/{y}{r}.png', {
  attribution: '',
  maxZoom: 19
}).addTo(map);

const markerIcon = L.divIcon({
  className: '',
  html: '<div style="width:24px;height:24px;background:#c084fc;border:3px solid #fff;border-radius:50%%;box-shadow:0 2px 8px rgba(0,0,0,0.5);transform:translate(-50%%,-50%%)"></div>',
  iconSize: [0, 0]
});

let marker = L.marker([%.6f, %.6f], { icon: markerIcon, draggable: true }).addTo(map);
let selectedLat = %.6f;
let selectedLon = %.6f;
updateCoords();

marker.on('dragend', () => {
  const p = marker.getLatLng();
  selectedLat = p.lat; selectedLon = p.lng;
  updateCoords();
});

map.on('click', e => {
  selectedLat = e.latlng.lat; selectedLon = e.latlng.lng;
  marker.setLatLng(e.latlng);
  updateCoords();
});

function updateCoords() {
  document.getElementById('coord-display').textContent = selectedLat.toFixed(6) + ', ' + selectedLon.toFixed(6);
}

function moveTo(lat, lon, zoom) {
  selectedLat = lat; selectedLon = lon;
  marker.setLatLng([lat, lon]);
  map.flyTo([lat, lon], zoom || 17, { duration: 0.8 });
  updateCoords();
}

// ── Geolocation ──
function locateMe() {
  const btn = document.getElementById('locate-btn');
  btn.classList.add('locating');

  if (!navigator.geolocation) {
    btn.classList.remove('locating');
    alert('Geolocation not supported');
    return;
  }

  navigator.geolocation.getCurrentPosition(
    pos => {
      btn.classList.remove('locating');
      moveTo(pos.coords.latitude, pos.coords.longitude, 17);
    },
    err => {
      btn.classList.remove('locating');
      alert('Could not get your location. Make sure location access is allowed.');
    },
    { enableHighAccuracy: true, timeout: 10000 }
  );
}

// ── Search with autocomplete ──
const searchInput = document.getElementById('search-input');
const suggestionsEl = document.getElementById('suggestions');
const clearBtn = document.getElementById('search-clear');
let debounceTimer = null;

searchInput.addEventListener('input', () => {
  const q = searchInput.value.trim();
  clearBtn.style.display = q ? 'block' : 'none';

  clearTimeout(debounceTimer);
  if (q.length < 3) { suggestionsEl.style.display = 'none'; return; }

  suggestionsEl.innerHTML = '<div class="suggestion-loading">Searching...</div>';
  suggestionsEl.style.display = 'block';

  debounceTimer = setTimeout(() => searchPlaces(q), 300);
});

searchInput.addEventListener('keydown', e => {
  if (e.key === 'Escape') {
    suggestionsEl.style.display = 'none';
    searchInput.blur();
  }
});

function clearSearch() {
  searchInput.value = '';
  clearBtn.style.display = 'none';
  suggestionsEl.style.display = 'none';
  searchInput.focus();
}

async function searchPlaces(q) {
  try {
    // Try Photon first (better autocomplete)
    const photonRes = await fetch('https://photon.komoot.io/api/?q=' + encodeURIComponent(q) + '&limit=5&lang=es');
    const photonData = await photonRes.json();

    if (photonData.features && photonData.features.length > 0) {
      renderSuggestions(photonData.features.map(f => ({
        name: buildPhotonName(f.properties),
        detail: buildPhotonDetail(f.properties),
        lat: f.geometry.coordinates[1],
        lon: f.geometry.coordinates[0]
      })));
      return;
    }

    // Fallback to Nominatim
    const nomRes = await fetch('https://nominatim.openstreetmap.org/search?q=' + encodeURIComponent(q) + '&format=json&limit=5&addressdetails=1', {
      headers: { 'Accept-Language': 'es,en' }
    });
    const nomData = await nomRes.json();

    if (nomData.length > 0) {
      renderSuggestions(nomData.map(r => ({
        name: r.display_name.split(',').slice(0, 2).join(','),
        detail: r.display_name.split(',').slice(2, 5).join(',').trim(),
        lat: parseFloat(r.lat),
        lon: parseFloat(r.lon)
      })));
      return;
    }

    suggestionsEl.innerHTML = '<div class="suggestion-loading">No results found</div>';
  } catch(e) {
    suggestionsEl.innerHTML = '<div class="suggestion-loading">Search error</div>';
  }
}

function renderSuggestions(items) {
  suggestionsEl.innerHTML = '';
  items.forEach(item => {
    const div = document.createElement('div');
    div.className = 'suggestion';
    div.innerHTML = '<div class="suggestion-icon">\u{1F4CD}</div><div class="suggestion-text"><div class="suggestion-name">' +
      escapeHtml(item.name) + '</div><div class="suggestion-detail">' + escapeHtml(item.detail) + '</div></div>';
    div.onclick = () => {
      moveTo(item.lat, item.lon, 17);
      searchInput.value = item.name;
      suggestionsEl.style.display = 'none';
    };
    suggestionsEl.appendChild(div);
  });
  suggestionsEl.style.display = 'block';
}

function buildPhotonName(p) {
  return [p.name, p.street, p.housenumber].filter(Boolean).join(' ') || p.name || '';
}

function buildPhotonDetail(p) {
  return [p.city, p.state, p.country].filter(Boolean).join(', ');
}

function escapeHtml(s) {
  const div = document.createElement('div');
  div.textContent = s || '';
  return div.innerHTML;
}

// Close suggestions on outside click
document.addEventListener('click', e => {
  if (!e.target.closest('#search-wrapper')) {
    suggestionsEl.style.display = 'none';
  }
});

// ── Confirm ──
async function confirmLocation() {
  await fetch('/confirm', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ lat: selectedLat, lon: selectedLon })
  });
  document.getElementById('done-screen').classList.add('visible');
}
</script>
</body>
</html>`, title, lat, lon, lat, lon, lat, lon)
}
