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
		fmt.Fprintf(w, mapHTML(title, initialLat, initialLon))
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

	// Find a free port
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, fmt.Errorf("start server: %w", err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	url := fmt.Sprintf("http://127.0.0.1:%d", port)

	server := &http.Server{Handler: mux}

	go server.Serve(listener)

	// Open browser
	openBrowser(url)

	// Wait for result
	result := <-resultCh

	// Shutdown server
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
  body { font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, sans-serif; background: #0f0f0f; color: #e0e0e0; }
  #header { padding: 16px 24px; display: flex; align-items: center; justify-content: space-between; background: #1a1a2e; border-bottom: 1px solid #2a2a3e; }
  #header h1 { font-size: 18px; font-weight: 600; color: #e879f9; }
  #coords { font-size: 14px; color: #888; font-family: monospace; }
  #search-bar { padding: 12px 24px; background: #1a1a2e; border-bottom: 1px solid #2a2a3e; display: flex; gap: 8px; }
  #search-input { flex: 1; padding: 8px 14px; border-radius: 8px; border: 1px solid #333; background: #0f0f0f; color: #e0e0e0; font-size: 14px; outline: none; }
  #search-input:focus { border-color: #e879f9; }
  #search-input::placeholder { color: #555; }
  #search-btn { padding: 8px 16px; border-radius: 8px; border: none; background: #333; color: #e0e0e0; cursor: pointer; font-size: 14px; }
  #search-btn:hover { background: #444; }
  #search-results { position: absolute; top: 100px; left: 24px; z-index: 1000; background: #1a1a2e; border: 1px solid #2a2a3e; border-radius: 8px; max-width: 500px; display: none; }
  .search-result { padding: 10px 14px; cursor: pointer; font-size: 13px; border-bottom: 1px solid #2a2a3e; }
  .search-result:hover { background: #2a2a3e; }
  .search-result:last-child { border-bottom: none; }
  #map { height: calc(100vh - 155px); }
  #confirm-bar { position: fixed; bottom: 0; left: 0; right: 0; padding: 16px 24px; background: #1a1a2e; border-top: 1px solid #2a2a3e; display: flex; align-items: center; justify-content: space-between; z-index: 1000; }
  #confirm-btn { padding: 10px 32px; border-radius: 8px; border: none; background: #e879f9; color: #0f0f0f; font-weight: 600; font-size: 15px; cursor: pointer; transition: background 0.2s; }
  #confirm-btn:hover { background: #d946ef; }
  #hint { font-size: 13px; color: #666; }
  .leaflet-container { background: #1a1a2e; }
</style>
</head>
<body>
<div id="header">
  <h1>%s</h1>
  <div id="coords">—</div>
</div>
<div id="search-bar">
  <input id="search-input" type="text" placeholder="Search address..." autocomplete="off"/>
  <button id="search-btn" onclick="doSearch()">Search</button>
</div>
<div id="search-results"></div>
<div id="map"></div>
<div id="confirm-bar">
  <div id="hint">Click on the map or search an address. Drag the marker to adjust.</div>
  <button id="confirm-btn" onclick="confirm()">Confirm location</button>
</div>

<script>
const map = L.map('map', { zoomControl: true }).setView([%.6f, %.6f], 14);
L.tileLayer('https://{s}.basemaps.cartocdn.com/dark_all/{z}/{x}/{y}{r}.png', {
  attribution: '© OpenStreetMap © CARTO',
  maxZoom: 19
}).addTo(map);

let marker = L.marker([%.6f, %.6f], { draggable: true }).addTo(map);
let selectedLat = %.6f;
let selectedLon = %.6f;
updateCoords(selectedLat, selectedLon);

marker.on('dragend', function(e) {
  const pos = e.target.getLatLng();
  selectedLat = pos.lat;
  selectedLon = pos.lng;
  updateCoords(selectedLat, selectedLon);
});

map.on('click', function(e) {
  selectedLat = e.latlng.lat;
  selectedLon = e.latlng.lng;
  marker.setLatLng(e.latlng);
  updateCoords(selectedLat, selectedLon);
});

function updateCoords(lat, lon) {
  document.getElementById('coords').textContent = lat.toFixed(6) + ', ' + lon.toFixed(6);
}

async function doSearch() {
  const q = document.getElementById('search-input').value.trim();
  if (!q) return;
  const res = await fetch('https://nominatim.openstreetmap.org/search?q=' + encodeURIComponent(q) + '&format=json&limit=5', {
    headers: { 'Accept-Language': 'es,en' }
  });
  const data = await res.json();
  const container = document.getElementById('search-results');
  container.innerHTML = '';
  if (data.length === 0) {
    container.innerHTML = '<div class="search-result">No results found</div>';
    container.style.display = 'block';
    setTimeout(() => container.style.display = 'none', 2000);
    return;
  }
  data.forEach(r => {
    const div = document.createElement('div');
    div.className = 'search-result';
    div.textContent = r.display_name;
    div.onclick = () => {
      const lat = parseFloat(r.lat);
      const lon = parseFloat(r.lon);
      selectedLat = lat;
      selectedLon = lon;
      marker.setLatLng([lat, lon]);
      map.setView([lat, lon], 16);
      updateCoords(lat, lon);
      container.style.display = 'none';
    };
    container.appendChild(div);
  });
  container.style.display = 'block';
}

document.getElementById('search-input').addEventListener('keydown', e => {
  if (e.key === 'Enter') doSearch();
});

document.addEventListener('click', e => {
  if (!e.target.closest('#search-bar') && !e.target.closest('#search-results')) {
    document.getElementById('search-results').style.display = 'none';
  }
});

async function confirm() {
  await fetch('/confirm', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ lat: selectedLat, lon: selectedLon })
  });
  document.body.innerHTML = '<div style="display:flex;align-items:center;justify-content:center;height:100vh;font-size:24px;color:#e879f9;">✓ Location confirmed — you can close this tab</div>';
}
</script>
</body>
</html>`, title, title, lat, lon, lat, lon, lat, lon)
}
