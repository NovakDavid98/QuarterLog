package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	wrt "github.com/wailsapp/wails/v2/pkg/runtime"

	"quarterlog/internal/capture"
	"quarterlog/internal/config"
	"quarterlog/internal/minimax"
	"quarterlog/internal/queue"
	"quarterlog/internal/ticker"
	"quarterlog/internal/winutil"
	"quarterlog/internal/xlsxlog"
)

// App is the Wails-bound application backend.
type App struct {
	ctx    context.Context
	store  *queue.Store
	tick   *ticker.Ticker

	mu      sync.Mutex
	recent  []string          // last few submitted descriptions, for AI continuity
	thumbs  map[string]string // id -> preview data URI, so we decode each PNG only once
	visible bool              // whether the window is currently showing an app view

	trayUpdate func(count int) // set by the tray once it's ready
}

func (a *App) setVisible(v bool) {
	a.mu.Lock()
	a.visible = v
	a.mu.Unlock()
}

func (a *App) isVisible() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.visible
}

// cachedThumb returns the preview for an interval, decoding the PNG at most once.
func (a *App) cachedThumb(id, path string) string {
	a.mu.Lock()
	if t, ok := a.thumbs[id]; ok {
		a.mu.Unlock()
		return t
	}
	a.mu.Unlock()

	t, _ := capture.ThumbFromFile(path, 900) // decode outside the lock
	a.mu.Lock()
	a.thumbs[id] = t
	a.mu.Unlock()
	return t
}

// putThumb stores an already-generated preview (from capture/recapture).
func (a *App) putThumb(id, thumb string) {
	a.mu.Lock()
	a.thumbs[id] = thumb
	a.mu.Unlock()
}

// forgetThumb drops a cached preview once its interval is gone.
func (a *App) forgetThumb(id string) {
	a.mu.Lock()
	delete(a.thumbs, id)
	a.mu.Unlock()
}

// updateTrayTitle refreshes the tray tooltip/label with the pending count.
func (a *App) updateTrayTitle() {
	if a.trayUpdate != nil {
		a.trayUpdate(a.PendingCount())
	}
}

// NewApp constructs the app with its persistent queue.
func NewApp() *App {
	store, err := queue.Open()
	if err != nil {
		// Fatal-ish: without a queue the app can't function. Surface and continue
		// with an in-memory-only store is not worth it; log and let startup show it.
		fmt.Println("queue open error:", err)
	}
	return &App{store: store, thumbs: map[string]string{}}
}

// startup wires runtime context and starts the ticker.
func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
	cfg := config.Current()
	a.tick = ticker.New(cfg.IntervalMinutes, a.onTick, func() bool {
		return config.Current().Paused
	})
	a.tick.Start()
}

// PendingView is the shape sent to the frontend for each queued interval.
type PendingView struct {
	ID     string  `json:"id"`
	Date   string  `json:"date"`
	From   string  `json:"from"`
	To     string  `json:"to"`
	Hours  float64 `json:"hours"`
	Status string  `json:"status"`
	Thumb  string  `json:"thumb"`
	Locked bool    `json:"locked"`
}

// onTick captures the screen for a closed interval and enqueues it.
func (a *App) onTick(from, to time.Time) {
	cfg := config.Current()
	iv := &queue.Interval{
		ID:      uuid.NewString(),
		Date:    from.Format("2006-01-02"),
		From:    from.Format("15:04"),
		To:      to.Format("15:04"),
		Hours:   float64(cfg.IntervalMinutes) / 60.0,
		Created: time.Now(),
		Status:  queue.StatusPending,
	}

	var thumb string
	if winutil.SessionLocked() {
		iv.Status = queue.StatusLocked // no useful screenshot on the lock screen
	} else {
		pngPath := filepath.Join(a.store.Dir(), iv.ID+".png")
		res, err := capture.Capture(cfg.Monitor, pngPath)
		if err != nil {
			fmt.Println("capture error:", err)
			iv.Status = queue.StatusLocked
		} else {
			iv.ImagePath = res.PNGPath
			thumb = res.ThumbB64
		}
	}

	if err := a.store.Add(iv); err != nil {
		fmt.Println("queue add error:", err)
		return
	}
	a.putThumb(iv.ID, thumb) // reuse in GetPending instead of re-decoding the PNG

	a.updateTrayTitle()
	if a.ctx != nil {
		a.showPopup()
		wrt.EventsEmit(a.ctx, "tick", PendingView{
			ID: iv.ID, Date: iv.Date, From: iv.From, To: iv.To,
			Hours: iv.Hours, Status: iv.Status, Thumb: thumb,
			Locked: iv.Status == queue.StatusLocked,
		})
	}
}

// GetPending returns all queued intervals with freshly generated thumbnails.
func (a *App) GetPending() []PendingView {
	if a.store == nil {
		return nil
	}
	items := a.store.List()
	out := make([]PendingView, 0, len(items))
	for _, it := range items {
		thumb := a.cachedThumb(it.ID, it.ImagePath)
		out = append(out, PendingView{
			ID: it.ID, Date: it.Date, From: it.From, To: it.To,
			Hours: it.Hours, Status: it.Status, Thumb: thumb,
			Locked: it.Status == queue.StatusLocked,
		})
	}
	return out
}

// PendingCount returns the number of queued intervals (used by the tray).
func (a *App) PendingCount() int {
	if a.store == nil {
		return 0
	}
	return a.store.Count()
}

// CaptureNow forces an immediate capture of the current quarter, for testing.
func (a *App) CaptureNow() {
	now := time.Now()
	step := time.Duration(config.Current().IntervalMinutes) * time.Minute
	a.onTick(now.Add(-step), now)
}

// Describe runs the screenshot for an interval through the MiniMax vision API,
// returning a suggested description and (from the configured list) a Type.
func (a *App) Describe(id string) (minimax.Suggestion, error) {
	// Defense in depth: never send a screenshot while the confidentiality regime is on.
	if config.Current().Confidential {
		return minimax.Suggestion{}, fmt.Errorf("Confidentiality regime is ON — screenshots are never sent to the AI. Type the description yourself.")
	}
	it, ok := a.store.Get(id)
	if !ok {
		return minimax.Suggestion{}, fmt.Errorf("interval not found")
	}
	if it.ImagePath == "" {
		return minimax.Suggestion{}, fmt.Errorf("no screenshot available for this interval")
	}
	img, err := capture.UploadFromFile(it.ImagePath, 1280)
	if err != nil {
		return minimax.Suggestion{}, err
	}
	cfg := config.Current()
	client := minimax.New(cfg.MiniMaxAPIKey, cfg.MiniMaxBaseURL, cfg.MiniMaxModel)
	prompt := cfg.Prompt
	if cfg.Language != "" {
		prompt += "\nWrite the description in " + cfg.Language + "."
	}
	ctx, cancel := context.WithTimeout(a.ctx, 65*time.Second)
	defer cancel()
	return client.Describe(ctx, img, prompt, a.recentCopy(), splitLines(cfg.Types))
}

// Recapture takes a fresh screenshot for an interval, replacing its image. The
// app window is hidden during the shot so the popup itself isn't captured; the
// caller is expected to have given the user time to cover confidential content.
// Returns the new preview thumbnail (data URI).
func (a *App) Recapture(id string) (string, error) {
	it, ok := a.store.Get(id)
	if !ok {
		return "", fmt.Errorf("interval not found")
	}

	// Hide the window and let it disappear before grabbing the screen.
	if a.ctx != nil {
		wrt.WindowHide(a.ctx)
	}
	time.Sleep(400 * time.Millisecond)

	pngPath := it.ImagePath
	if pngPath == "" {
		pngPath = filepath.Join(a.store.Dir(), it.ID+".png")
	}
	res, err := capture.Capture(config.Current().Monitor, pngPath)

	// Bring the window back regardless of capture outcome.
	if a.ctx != nil {
		wrt.WindowShow(a.ctx)
	}
	if err != nil {
		return "", err
	}

	it.ImagePath = res.PNGPath
	it.Status = queue.StatusPending // a retaken interval always has an image now
	updated := it
	if err := a.store.Add(&updated); err != nil {
		return "", err
	}
	a.putThumb(id, res.ThumbB64)
	return res.ThumbB64, nil
}

// Correct runs the given text through MiniMax to fix spelling/diacritics, in the
// configured language. Used by the Shift+R shortcut in the description box.
func (a *App) Correct(text string) (string, error) {
	cfg := config.Current()
	client := minimax.New(cfg.MiniMaxAPIKey, cfg.MiniMaxBaseURL, cfg.MiniMaxModel)
	ctx, cancel := context.WithTimeout(a.ctx, 30*time.Second)
	defer cancel()
	return client.CorrectText(ctx, text, cfg.Language)
}

// Submit logs an interval to the worklog file and removes it from the queue.
func (a *App) Submit(id, description, category, typ string) error {
	it, ok := a.store.Get(id)
	if !ok {
		return fmt.Errorf("interval not found")
	}
	if description == "" {
		return fmt.Errorf("description is required")
	}
	day, err := time.ParseInLocation("2006-01-02", it.Date, time.Local)
	if err != nil {
		return fmt.Errorf("invalid interval date: %w", err)
	}
	if err := xlsxlog.Append(a.worklogPath(), xlsxlog.Entry{
		Day:         day,
		Hours:       it.Hours,
		Category:    category,
		Description: description,
		Type:        typ,
	}); err != nil {
		return err
	}
	a.pushRecent(description)
	if err := a.store.Remove(id); err != nil {
		return err
	}
	a.forgetThumb(id)
	a.updateTrayTitle()
	return nil
}

// SubmitManual logs a hand-entered worklog row that was never captured. date is
// YYYY-MM-DD, hours is the decimal duration. Nothing touches the queue.
func (a *App) SubmitManual(date string, hours float64, category, description, typ string) error {
	if description == "" {
		return fmt.Errorf("description is required")
	}
	if hours <= 0 {
		return fmt.Errorf("duration must be greater than zero")
	}
	if date == "" {
		date = time.Now().Format("2006-01-02")
	}
	day, err := time.ParseInLocation("2006-01-02", date, time.Local)
	if err != nil {
		return fmt.Errorf("invalid date (use YYYY-MM-DD): %w", err)
	}

	if err := xlsxlog.Append(a.worklogPath(), xlsxlog.Entry{
		Day:         day,
		Hours:       hours,
		Category:    category,
		Description: description,
		Type:        typ,
	}); err != nil {
		return err
	}
	a.pushRecent(description)
	return nil
}

// worklogPath returns the configured file path, falling back to the default.
func (a *App) worklogPath() string {
	if p := strings.TrimSpace(config.Current().FilePath); p != "" {
		return p
	}
	return config.DefaultWorklogPath()
}

// SetFilePath persists only the worklog file path (used by the Open/Show buttons
// so they act on the path currently typed in Settings).
func (a *App) SetFilePath(path string) error {
	cfg := config.Current()
	cfg.FilePath = strings.TrimSpace(path)
	return config.Save(cfg)
}

// ClearWorklog empties the worklog Excel file (keeps the styled header).
func (a *App) ClearWorklog() error {
	return xlsxlog.Clear(a.worklogPath())
}

// OpenWorklogFile opens the worklog in the default app (Excel), creating an empty
// styled file first if it doesn't exist yet.
func (a *App) OpenWorklogFile() error {
	path := a.worklogPath()
	if _, err := os.Stat(path); os.IsNotExist(err) {
		if err := xlsxlog.EnsureFile(path); err != nil {
			return err
		}
	}
	return exec.Command("cmd", "/c", "start", "", path).Start()
}

// RevealWorklogFolder opens Explorer with the worklog file selected.
func (a *App) RevealWorklogFolder() error {
	path := a.worklogPath()
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return exec.Command("explorer", filepath.Dir(path)).Start()
	}
	return exec.Command("explorer", "/select,"+path).Start()
}

// NowParts returns today's date, for pre-filling the manual-entry form.
func (a *App) NowParts() map[string]string {
	return map[string]string{"date": time.Now().Format("2006-01-02")}
}

// ShowManual opens the manual-entry form (invoked from the tray).
func (a *App) ShowManual() { a.showLarge("manual") }

// ShowSettings opens the settings view (invoked from the popup's gear button).
func (a *App) ShowSettings() { a.showLarge("settings") }

// Dismiss drops an interval without logging it.
func (a *App) Dismiss(id string) error {
	err := a.store.Remove(id)
	a.forgetThumb(id)
	a.updateTrayTitle()
	return err
}

// ClearQueue discards every pending interval.
func (a *App) ClearQueue() error {
	for _, it := range a.store.List() {
		_ = a.store.Remove(it.ID)
		a.forgetThumb(it.ID)
	}
	a.updateTrayTitle()
	return nil
}

// GetConfig returns the current settings.
func (a *App) GetConfig() config.Config { return config.Current() }

// SaveConfig persists settings and applies autostart + ticker changes.
func (a *App) SaveConfig(cfg config.Config) error {
	prev := config.Current()
	if err := config.Save(cfg); err != nil {
		return err
	}
	if cfg.Autostart != prev.Autostart {
		if exe, err := os.Executable(); err == nil {
			_ = winutil.SetAutostart(cfg.Autostart, exe)
		}
	}
	if cfg.IntervalMinutes != prev.IntervalMinutes && a.tick != nil {
		a.tick.Stop()
		a.tick = ticker.New(cfg.IntervalMinutes, a.onTick, func() bool { return config.Current().Paused })
		a.tick.Start()
	}
	return nil
}

// ToggleConfidential flips the confidentiality regime and returns the new state.
// Bound so the Shift+C shortcut can toggle it.
func (a *App) ToggleConfidential() bool {
	cfg := config.Current()
	cfg.Confidential = !cfg.Confidential
	_ = config.Save(cfg)
	return cfg.Confidential
}

// SetPaused toggles capturing on/off.
func (a *App) SetPaused(paused bool) error {
	cfg := config.Current()
	cfg.Paused = paused
	return config.Save(cfg)
}

// HidePopup hides the window (called when the user finishes/snoozes).
func (a *App) HidePopup() {
	a.setVisible(false)
	if a.ctx != nil {
		wrt.WindowHide(a.ctx)
	}
}

// onConfidentialHotkey toggles the confidentiality regime from the global
// Ctrl+Alt+C hotkey and shows a fading toast — even when the app is hidden.
func (a *App) onConfidentialHotkey() {
	on := a.ToggleConfidential()
	msg := "Confidentiality regime OFF"
	if on {
		msg = "🔒 Confidentiality regime ON"
	}
	if a.ctx == nil {
		return
	}
	wrt.EventsEmit(a.ctx, "confidential-state", on)
	if a.isVisible() {
		wrt.EventsEmit(a.ctx, "toast", msg) // window already open — overlay the toast
	} else {
		a.flashToast(msg)
	}
}

// flashToast briefly shows the window as a small, transparent toast (used when
// the app is otherwise hidden), then hides it again.
func (a *App) flashToast(msg string) {
	if a.ctx == nil {
		return
	}
	const tw, th = 360, 90
	wrt.WindowSetSize(a.ctx, tw, th)
	if ww, wh := winutil.WorkArea(); ww > 0 && wh > 0 {
		scale := a.primaryScale()
		w, h, m := int(float64(tw)*scale), int(float64(th)*scale), int(24*scale)
		wrt.WindowSetPosition(a.ctx, (ww-w)/2, wh-h-m)
	}
	wrt.WindowShow(a.ctx)
	wrt.WindowSetAlwaysOnTop(a.ctx, true)
	wrt.EventsEmit(a.ctx, "flash-toast", msg)
	a.setVisible(false) // it's a transient toast, not an app view
	go func() {
		time.Sleep(2200 * time.Millisecond)
		if a.ctx != nil && !a.isVisible() {
			wrt.WindowHide(a.ctx)
		}
	}()
}

// --- helpers ---

// splitLines turns a newline/comma-separated config string into a trimmed slice.
func splitLines(s string) []string {
	fields := strings.FieldsFunc(s, func(r rune) bool { return r == '\n' || r == '\r' || r == ',' })
	out := make([]string, 0, len(fields))
	for _, f := range fields {
		if t := strings.TrimSpace(f); t != "" {
			out = append(out, t)
		}
	}
	return out
}

func (a *App) recentCopy() []string {
	a.mu.Lock()
	defer a.mu.Unlock()
	out := make([]string, len(a.recent))
	copy(out, a.recent)
	return out
}

func (a *App) pushRecent(desc string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.recent = append(a.recent, desc)
	if len(a.recent) > 3 {
		a.recent = a.recent[len(a.recent)-3:]
	}
}

const popupW, popupH = 380, 560

// showPopup positions the window at the configured screen zone and shows it.
//
// Wails' WindowSetPosition on Windows takes PHYSICAL pixels relative to the work
// area (no DPI scaling), whereas WindowSetSize takes LOGICAL pixels. So we compute
// the position in physical pixels: physical popup size = logical size × DPI scale.
func (a *App) showPopup() {
	if a.ctx == nil {
		return
	}
	wrt.WindowSetSize(a.ctx, popupW, popupH)

	ww, wh := winutil.WorkArea() // physical work-area size (excludes taskbar)
	if ww > 0 && wh > 0 {
		scale := a.primaryScale()
		x, y := popupCoords(config.Current().PopupPosition, ww, wh,
			int(float64(popupW)*scale), int(float64(popupH)*scale), int(24*scale))
		wrt.WindowSetPosition(a.ctx, x, y)
	}
	// Show and force it to the front. After a hide/close the window can otherwise
	// come back behind the foreground app (it has no taskbar button), which makes
	// it look like nothing happened.
	wrt.WindowUnminimise(a.ctx)
	wrt.WindowShow(a.ctx)
	wrt.WindowSetAlwaysOnTop(a.ctx, true)
	a.setVisible(true)
}

// primaryScale returns the primary display's DPI scale (physical/logical), e.g.
// 1.5 at 150%. Falls back to 1.0.
func (a *App) primaryScale() float64 {
	screens, err := wrt.ScreenGetAll(a.ctx)
	if err != nil {
		return 1.0
	}
	for _, s := range screens {
		if s.IsPrimary && s.Size.Width > 0 && s.PhysicalSize.Width > 0 {
			return float64(s.PhysicalSize.Width) / float64(s.Size.Width)
		}
	}
	// No primary flagged: use the first screen if usable.
	if len(screens) > 0 && screens[0].Size.Width > 0 && screens[0].PhysicalSize.Width > 0 {
		return float64(screens[0].PhysicalSize.Width) / float64(screens[0].Size.Width)
	}
	return 1.0
}

// popupCoords maps a zone name (e.g. "bottom-right") to a work-area-relative
// top-left position, all in physical pixels. workW/workH and pw/ph (popup size)
// and margin m are physical.
func popupCoords(pos string, workW, workH, pw, ph, m int) (int, int) {
	if pos == "" {
		pos = "bottom-right"
	}
	parts := strings.SplitN(pos, "-", 2)
	vert, horiz := parts[0], ""
	if len(parts) == 2 {
		horiz = parts[1]
	}

	var x, y int
	switch horiz {
	case "left":
		x = m
	case "right":
		x = workW - pw - m
	default: // center
		x = (workW - pw) / 2
	}
	switch vert {
	case "top":
		y = m
	case "center":
		y = (workH - ph) / 2
	default: // bottom
		y = workH - ph - m
	}
	return x, y
}

// ShowQueue and ShowSettings are invoked from the tray via navigate events;
// they resize/centre the window and reveal it.
func (a *App) showLarge(view string) {
	if a.ctx == nil {
		return
	}
	wrt.WindowSetSize(a.ctx, 720, 640)
	wrt.WindowCenter(a.ctx)
	wrt.WindowUnminimise(a.ctx)
	wrt.WindowShow(a.ctx)
	wrt.WindowSetAlwaysOnTop(a.ctx, true)
	a.setVisible(true)
	wrt.EventsEmit(a.ctx, "navigate", view)
}

