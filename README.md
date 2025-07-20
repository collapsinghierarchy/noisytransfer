# noisytransfer
noisytransfer – ultra‑light, post‑quantum file beaming over a single share‑link. Works with short authentication strings (SAS) and a commit then reveal to counteract MitM-Attacks by the server.


> **Status:** early WIP — Breaking changes every week.

## ✨ What it is

* **One–click send link** – pick any file in your browser and get a short URL (`https://noisy.t/abc123`).  
  Open the link on any other device and the file streams **end‑to‑end‑encrypted** directly—never touches disk on the relay.

* **File‑request link** – generate the reverse URL (`https://noisy.t/upload/xyz987`).  
  Give it to somebody; whatever they drop in is pushed straight back to **you**, encrypted with the same hybrid Kyber + X25519 HPKE.

* **Ephemeral by design** – everything lives only in memory; when both tabs close, the relay forgets.  
  No accounts, no quota, no GDPR headaches.

* **Post‑quantum ready** – hybrid **ML‑KEM‑768 + X25519** key exchange, ChaCha20‑Poly1305 chunks.

* **Zero install** – ships as a tiny PWA (Progressive Web App) you can “Add to Home Screen” or run in any desktop browser.


## 🛠 How it works

1. **Browser A** chooses a file → creates random `channelID` → opens `wss://relay/ws?appID=<ID>`.
2. **Share‑link** `https://noisy.t/get/<ID>` is shown.
3. **Browser B** visits the link → joins the same room.
4. Live HPKE **commit‑then‑reveal + 6‑digit SAS** stops MitM.
5. File is chunked (64 kB), each chunk HPKE‑sealed and streamed through the relay.
6. Relay code = 300 lines of Go (`service`, `hub`, `handler`, `main`) – fully in‑memory, cap two sockets.



## 🌱 Road‑map

| Stage | Status |
|-------|--------|
| Browser‑to‑browser link (send & request) | ✅ working prototype |
| PWA packaging (manifest, service‑worker) | ◽ in progress |
| Android share‑sheet target | ◽ |
| Desktop shell helpers (Explorer/Finder “Send with noisytransfer”) | ◽ |
| Optional sealed‑nonce mode (zero SAS UI) | ◽ |
| Chunk resume / multi‑GB restart | ◽ |



## ⚡ Bootstrapping the PWA

You don’t need a heavy front‑end stack, but a helper that outputs **manifest.json + service‑worker** and gives you a live‑reload dev server saves hours.

| Tool | Why it’s perfect for *noisytransfer* | One‑liner to start |
|------|--------------------------------------|-------------------|
| **PWABuilder** | Generates manifest, icons, and a TypeScript Workbox SW from your existing HTML in 30 seconds; no framework lock‑in. | Paste `http://localhost:8081` into <https://www.pwabuilder.com> & download bundle. |
| **Vite + `vite-plugin-pwa`** | React‑level DX without React. Fast HMR; plugin injects SW/manifest automatically. | `npm create vite@latest noisy-ui` → *vanilla* → `npm i -D vite-plugin-pwa` |
| **Svelte Kit** + `@sveltejs/pwa` | Tiny, reactive component DSL; PWA baked in. | `npm create svelte@latest noisytransfer` |
| **Quasar CLI** (Vue) | Material‑styled widgets, dark‑mode; PWA flag toggled at init. | `quasar create noisytransfer --kit pwa` |

*Recommendation*: start with **PWABuilder** to get a running installable PWA quickly, then move to **Vite + plugin‑pwa** once you want hot‑reload and TypeScript support.



## 📂 Repo structure
```
/cmd/server/ Go main + handlers (in‑memory relay)
/web/ index.html, send.html, request.html, app.js, style.css
/web/sw.js (generated) service‑worker cache logic
/web/manifest.json (generated) PWA manifest
/README.md ← you are here
```

```bash
git clone https://github.com/yourname/noisytransfer
cd noisytransfer/cmd/server
go run .

# in another terminal (UI dev)
cd ../web
npm i
npm run dev     # vite dev server on :5173

Open http://localhost:5173/send.html, pick a file, copy the link to another browser tab—done!
```