//go:build testhttp

// Mode de test (jamais inclus dans la version distribuée) : expose les méthodes
// de l'application et l'interface par HTTP sur 127.0.0.1, pour piloter
// l'interface réelle avec un navigateur automatisé.
//   go build -tags testhttp && ./interdisk -test-http 48000

package main

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"net/http"
	"reflect"
	"strings"
	"time"
)

func init() { testHTTPServe = serveTestHTTP }

const testShim = `<script>
window.go = {main: {App: new Proxy({}, {get: (_, method) => async (...args) => {
  const r = await fetch('/call', {method: 'POST', body: JSON.stringify({method, args})});
  const j = await r.json();
  if (j.error) throw j.error;
  return j.result;
}})}};
window.runtime = {
  _ev: {},
  EventsOn(name, cb) {
    if (name === 'state') { const es = new EventSource('/events'); es.onmessage = e => cb(JSON.parse(e.data)); }
    else (this._ev[name] = this._ev[name] || []).push(cb);
  },
  OnFileDrop(cb) { window.__testDrop = paths => cb(0, 0, paths); },
  WindowMinimise() {}, WindowToggleMaximise() {}, WindowIsMaximised() { return Promise.resolve(false); },
  Quit() { window.__quit = true; },
};
window.__emit = (name, data) => (window.runtime._ev[name] || []).forEach(cb => cb(data));
</script>`

func serveTestHTTP(a *App, port int) error {
	ui, _ := fs.Sub(uiFiles, "ui")
	page, err := fs.ReadFile(ui, "index.html")
	if err != nil {
		return err
	}
	html := strings.Replace(string(page), "<head>", "<head>"+testShim, 1)
	mux := http.NewServeMux()
	files := http.FileServer(http.FS(ui))
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			files.ServeHTTP(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, html)
	})
	mux.HandleFunc("/events", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		for {
			b, _ := json.Marshal(a.State())
			if _, err := fmt.Fprintf(w, "data: %s\n\n", b); err != nil {
				return
			}
			w.(http.Flusher).Flush()
			select {
			case <-r.Context().Done():
				return
			case <-time.After(500 * time.Millisecond):
			}
		}
	})
	mux.HandleFunc("/call", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Method string            `json:"method"`
			Args   []json.RawMessage `json:"args"`
		}
		reply := func(v any) { w.Header().Set("Content-Type", "application/json"); json.NewEncoder(w).Encode(v) }
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			reply(map[string]any{"error": err.Error()})
			return
		}
		m := reflect.ValueOf(a).MethodByName(req.Method)
		if !m.IsValid() || m.Type().NumIn() != len(req.Args) {
			reply(map[string]any{"error": "méthode inconnue : " + req.Method})
			return
		}
		in := make([]reflect.Value, len(req.Args))
		for i, raw := range req.Args {
			v := reflect.New(m.Type().In(i))
			if err := json.Unmarshal(raw, v.Interface()); err != nil {
				reply(map[string]any{"error": err.Error()})
				return
			}
			in[i] = v.Elem()
		}
		out := m.Call(in)
		res := map[string]any{}
		for _, o := range out {
			if e, ok := o.Interface().(error); ok && e != nil {
				res["error"] = e.Error()
			} else if o.Type().String() != "error" {
				res["result"] = o.Interface()
			}
		}
		reply(res)
	})
	return http.ListenAndServe(fmt.Sprintf("127.0.0.1:%d", port), mux)
}
