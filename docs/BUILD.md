# Building Quarterlog

## Prerequisites

| Tool | Version | Notes |
|---|---|---|
| Windows | 10 / 11 | The app is Windows-only (Win32 + WebView2). |
| WebView2 runtime | latest | Pre-installed on modern Windows; otherwise from Microsoft. |
| Go | 1.26+ | https://go.dev/dl/ |
| Node.js | 18+ | Needed to build the frontend. |
| Wails CLI | v2 | `go install github.com/wailsapp/wails/v2/cmd/wails@latest` |

Verify your toolchain:

```powershell
wails doctor
```

It should report WebView2, Node, and npm as installed and end with
*"Your system is ready for Wails development!"*. No C compiler is required — the
Windows build uses the pure-Go WebView2 loader.

## Develop

```powershell
wails dev
```

Hot-reloads the frontend and rebuilds the Go backend on change. The app launches to the
system tray; use **Log now** to force an interval popup without waiting 15 minutes.

## Build a release binary

```powershell
wails build
# → build\bin\quarterlog.exe
```

## First run

1. Launch `quarterlog.exe`. Find the icon in the system tray (it may be in the `^`
   overflow).
2. Open **Settings**, paste your MiniMax API key, set your categories/types and the
   worklog file path, and save.
3. Use **Log now** to test the full flow end-to-end.

## Regenerating the README screenshot

The main screenshot (`docs/img/main.png`) is a capture of the real popup. To retake it,
run the app, trigger **Log now**, and capture the popup window — or drive the frontend
with mock data in a browser. Keep the image at ~380px wide for a crisp README.

## Troubleshooting

- **Tray icon missing** — check the hidden-icons overflow (`^`) next to the clock; drag
  it onto the taskbar to keep it visible.
- **Popup appears in the wrong place** — the position picker is DPI-aware; if a display
  is added/removed, reopen Settings and re-pick the zone.
- **"The worklog file is open in Excel"** — close the workbook and log again; the
  interval stays queued until the write succeeds.
- **AI returns nothing / `<think>` text** — ensure the model is `MiniMax-M3` and the
  base URL is the OpenAI-compatible endpoint; the app already disables reasoning output.
