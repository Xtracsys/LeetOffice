// LeetOffice desktop client (D14): a bundled Electron app that pins its own
// Chromium so system-browser updates cannot break the UI. It renders the
// daemon's localhost team UI and starts the bundled daemon if none is
// running — double-click and you're in. The store's tabbed HTML files remain
// the read-only fallback in any browser.
const { app, BrowserWindow, Menu } = require("electron");
const { spawn } = require("child_process");
const http = require("http");
const path = require("path");
const fs = require("fs");

const NODE_URL = process.env.LEETOFFICE_URL || "http://127.0.0.1:7667";

function bundledDaemon() {
  // Packaged: extraResources copies dist/electron/${os}-${arch}/leetd → resources/leetd
  // Dev: dist.sh writes leetd-<ver>-<goos>-<goarch> (and the electron/ alias).
  const goos = process.platform === "win32" ? "windows" : process.platform;
  const goarch = process.arch === "x64" ? "amd64" : process.arch;
  const ext = process.platform === "win32" ? ".exe" : "";
  const res = process.resourcesPath || "";
  const dist = path.join(__dirname, "..", "dist");
  const candidates = [
    path.join(res, "leetd" + ext),
    path.join(res, "leetd"),
    path.join(res, "leetd", "leetd" + ext),
  ];
  if (fs.existsSync(dist)) {
    const ver = (() => {
      try { return require("./package.json").version; } catch { return ""; }
    })();
    if (ver) {
      candidates.push(path.join(dist, `leetd-${ver}-${goos}-${goarch}${ext}`));
    }
    for (const name of fs.readdirSync(dist)) {
      if (name.startsWith("leetd-") && name.endsWith(`-${goos}-${goarch}${ext}`)) {
        candidates.push(path.join(dist, name));
      }
    }
  }
  return candidates.find((p) => p && fs.existsSync(p));
}

function nodeUp() {
  return new Promise((resolve) => {
    const req = http.get(NODE_URL + "/", { timeout: 700 }, (res) => {
      res.resume();
      resolve(res.statusCode === 200);
    });
    req.on("error", () => resolve(false));
    req.on("timeout", () => { req.destroy(); resolve(false); });
  });
}

async function ensureDaemon() {
  if (await nodeUp()) return;
  const bin = bundledDaemon();
  if (!bin) {
    console.error("no bundled leetd found — start it manually: leetd serve");
    return;
  }
  const child = spawn(bin, ["serve"], { stdio: "ignore", detached: false });
  child.on("error", (e) => console.error("daemon spawn failed:", e));
  for (let i = 0; i < 40; i++) {
    await new Promise((r) => setTimeout(r, 250));
    if (await nodeUp()) return;
  }
}

function createWindow() {
  const win = new BrowserWindow({
    width: 1200,
    height: 800,
    title: "LeetOffice",
    autoHideMenuBar: false,
  });
  win.loadURL(NODE_URL);
  win.on("page-title-updated", (e) => e.preventDefault());
}

app.whenReady().then(async () => {
  Menu.setApplicationMenu(
    Menu.buildFromTemplate([
      { label: "LeetOffice", submenu: [{ role: "reload" }, { role: "quit" }] },
    ])
  );
  await ensureDaemon();
  createWindow();
  app.on("activate", () => {
    if (BrowserWindow.getAllWindows().length === 0) createWindow();
  });
});

app.on("window-all-closed", () => {
  app.quit();
});
