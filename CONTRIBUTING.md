# Contributing

Thanks for your interest in Quarterlog! This is a small, focused Windows utility.

## Getting set up

See [docs/BUILD.md](docs/BUILD.md) for prerequisites and the `wails dev` / `wails build`
workflow. Read [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md) for how the pieces fit.

## Project layout

- **Go backend** — `main.go` (tray + bootstrap), `app.go` (bound methods), and focused
  packages under `internal/`. Keep each package single-purpose.
- **Frontend** — `frontend/src/main.ts` (three views + toast) and `frontend/src/style.css`
  (hand-written macOS-style CSS). The frontend calls Go via `window.go.main.App`.

## Common changes

**Add a setting**
1. Add the field to `config.Config` and a default in `config.Defaults()`
   (`internal/config/config.go`).
2. Add the input to the settings view and include it in the save payload
   (`frontend/src/main.ts`, `renderSettings`).
3. Use `config.Current().YourField` where needed. Document it in
   [docs/CONFIGURATION.md](docs/CONFIGURATION.md).

**Add a bound method**
- Add an exported method on `*App` in `app.go`. Wails regenerates the JS bindings on
  build; call it from the frontend as `window.go.main.App.YourMethod(...)`.

## Conventions

- Match the surrounding Go and TypeScript style; keep comments about *why*, not *what*.
- Long or blocking work triggered from the tray must run in a goroutine (the tray
  message loop must stay responsive).
- Anything that could send data off the machine must be behind an explicit user action.
- Prefer reusing the existing `internal/*` helpers over adding new dependencies.

## Before opening a PR

- `wails build` succeeds.
- Manually exercise the affected flow (**Log now** covers most of it end-to-end).
- Update the relevant docs (`README.md`, `docs/*`) if behaviour or settings changed.
