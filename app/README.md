# LeetOffice desktop app

The human client (D14): an Electron wrapper around the daemon's localhost UI
(`http://127.0.0.1:7667` by default — override with `LEETOFFICE_URL`).
Electron pins its own Chromium, so browser/OS updates cannot break the client.

## Run (dev)

1. Start the daemon first: `leetd serve` (or rely on the auto-start below).
2. Then:

   ```sh
   cd app
   npm install   # pins the Electron version from package.json
   npm start
   ```

## Ship the app (end users)

Build the platform binaries first, then the installer:

```sh
./scripts/dist.sh          # produces dist/leetd-<os>-<arch>
cd app && npm install && npm run dist
# → dist/LeetOffice.dmg (macOS), AppImage (Linux), NSIS installer (Windows)
```

electron-builder bundles the platform's `leetd` binary into the app
(`extraResources`). On launch the app checks `http://127.0.0.1:7667` and
spawns the bundled daemon if it isn't running — end users double-click the
app and get the first-run wizard; no terminal, no Go toolchain.

Read-only fallback: open any store file (`docs/<slug>.html`) in a browser —
no write path, per the spec.
