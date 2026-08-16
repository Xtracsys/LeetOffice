# LeetOffice desktop app

The human client (D14): an Electron wrapper around the daemon's localhost UI
(`http://127.0.0.1:7667` by default — override with `LEETOFFICE_URL`).
Electron pins its own Chromium, so browser/OS updates cannot break the client.

## Run

1. Start the daemon first: `leetd serve` (from the Go module root).
2. Then:

   ```sh
   cd app
   npm install   # pins the Electron version from package.json
   npm start
   ```

Read-only fallback: open any store file (`docs/<slug>.html`) in a browser —
no write path, per the spec.
