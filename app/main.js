// LeetOffice desktop client (D14): a bundled Electron app that pins its own
// Chromium so system-browser updates cannot break the UI. It renders the
// daemon's localhost editor UI; all data stays local. The store's tabbed HTML
// files remain the read-only fallback in any browser.
const { app, BrowserWindow, Menu } = require("electron");

const NODE_URL = process.env.LEETOFFICE_URL || "http://127.0.0.1:7667";

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

app.whenReady().then(() => {
  Menu.setApplicationMenu(
    Menu.buildFromTemplate([
      {
        label: "LeetOffice",
        submenu: [{ role: "reload" }, { role: "quit" }],
      },
    ])
  );
  createWindow();
  app.on("activate", () => {
    if (BrowserWindow.getAllWindows().length === 0) createWindow();
  });
});

app.on("window-all-closed", () => {
  app.quit();
});
