# Architecture

Quarterlog is a [Wails v2](https://wails.io) desktop app: a Go backend bound to a
vanilla-TypeScript frontend rendered in a native WebView2 window. The window is
frameless, translucent (acrylic), always-on-top, and hidden by default — the app
lives in the system tray.

## Process & threading model

- **`main.go`** boots Wails and, in `OnStartup`, launches the system tray
  (`github.com/energye/systray`) in a goroutine. A named-mutex **single-instance
  guard** (`winutil.SingleInstance`) prevents a second copy from running.
- Tray click callbacks run on the tray's own message-loop thread, so each handler
  **dispatches its real work to a goroutine** (`go app.CaptureNow()`, etc.). This keeps
  the menu responsive and ensures window operations run off the tray thread.
- **`app.go`** is the `App` struct whose exported methods are bound into the frontend
  (`window.go.main.App.*`). The frontend calls them; Go emits events
  (`runtime.EventsEmit`) back — `"tick"` when a new interval is captured and
  `"navigate"` when the tray opens the queue/settings.

## Packages (`internal/`)

| Package | Responsibility |
|---|---|
| `config` | Load/save `Config` as JSON at `%APPDATA%\Quarterlog\config.json`; defaults; cached current value. |
| `queue` | Persistent, mutex-guarded store of pending intervals + their PNGs under `%LOCALAPPDATA%\Quarterlog\queue`. Survives restarts. |
| `capture` | Grab a display via `kbinani/screenshot`; write a full PNG; produce downscaled JPEG data URIs for the preview and the API upload. |
| `ticker` | Wall-clock-aligned ticker with sleep/resume catch-up. |
| `minimax` | MiniMax-M3 client (OpenAI-compatible `/chat/completions`): `Describe` (vision → description + Type) and `CorrectText` (spelling/diacritics). |
| `xlsxlog` | Append one styled row per entry to the local `.xlsx` (`xuri/excelize`). |
| `winutil` | Windows helpers: session-lock detection, single-instance mutex, `HKCU\…\Run` autostart, and physical work-area query. |

## Data flow

```
ticker fires ─▶ capture screenshot ─▶ queue.Add(interval)
                                        │
                              emit "tick" ─▶ popup card (frontend)
                                        │
   ┌── Suggest with AI ─▶ minimax.Describe(image) ─▶ editable draft + Type
   │
   └── Log it ─▶ xlsxlog.Append(worklog.xlsx) ─▶ queue.Remove(interval)
```

An interval is only removed from the queue after a **successful** write, so if the
Excel file is open (locked) the entry stays queued and the user sees a friendly toast.

## The wall-clock ticker (`internal/ticker`)

Rather than a naïve `time.Ticker`, the ticker computes the next quarter-hour boundary
from local midnight and sleeps until then. On wake it emits **every** boundary between
the last one it handled and now — so if the laptop slept through several intervals, each
missed quarter is queued on resume instead of being lost. Capturing is skipped while the
config is `Paused`.

## Confidentiality model

The two paths are mutually exclusive by design:

- **Suggest with AI** → the screenshot is sent to MiniMax. The entry is *not* confidential.
- **Type it yourself** (never touch AI) → nothing leaves the machine.
- **Locked-screen intervals** have no useful screenshot, so they skip the AI path entirely.
- **Retake** lets the user hide confidential content (the window hides itself during a
  short countdown) and re-shoot before sending.

## DPI-aware popup positioning (`app.go`)

This is the subtle part. On Windows, Wails' `WindowSetSize` takes **logical** pixels
(and scales them up by the monitor DPI), but `WindowSetPosition` takes **physical**
pixels **relative to the work area** and does *no* scaling. Mixing the two puts the
window in the wrong place on scaled displays.

`showPopup` therefore:

1. Reads the physical **work area** via `winutil.WorkArea` (`SystemParametersInfo`),
   which already excludes the taskbar.
2. Computes the DPI scale from `runtime.ScreenGetAll` (`PhysicalSize / Size`).
3. Converts the logical popup size (`380×560`) to physical (`× scale`) and places it in
   the chosen zone with a scaled margin (`popupCoords`).

Because the app manifest declares `permonitorv2` awareness, both the work-area query and
`WindowSetPosition` operate in the same physical-pixel space, so the math is consistent.

## Excel output (`internal/xlsxlog`)

`Append` opens the workbook (or builds a freshly styled one), finds the next empty row,
and writes the six columns with cached style IDs: a bold header with a light fill and
bottom border, a frozen header row, an autofilter, thin borders, `m/d/yyyy` for Day,
`0.00` for Hours, and wrapped Description. Writes are serialized behind a package mutex.

## Frontend (`frontend/src`)

A single `main.ts` renders three views into `#app` — the interval **editor** (used for
both the live popup and queue items), the **queue** list, and **settings** — plus a
toast helper. Styling is hand-written macOS-inspired CSS in `style.css` (translucent
card, SF-Pro-ish system font stack, light/dark via `prefers-color-scheme`). The frontend
talks to Go through `window.go.main.App` and listens for `window.runtime` events.
