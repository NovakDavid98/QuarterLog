# Configuration

All settings are editable in the app's **Settings** screen (tray → *Settings…*, or the
⚙️ button on the popup). They are persisted as JSON at:

```
%APPDATA%\Quarterlog\config.json
```

Screenshots and the pending-interval queue live separately at:

```
%LOCALAPPDATA%\Quarterlog\queue\
```

## Fields

| JSON key | Setting | Default | Notes |
|---|---|---|---|
| `miniMaxApiKey` | MiniMax API key | *(empty)* | Bearer token for the vision API. Stored in plaintext today (see Security). |
| `miniMaxBaseUrl` | MiniMax base URL | `https://api.minimax.io/v1` | OpenAI-compatible endpoint root; the app calls `{base}/chat/completions`. |
| `miniMaxModel` | MiniMax model | `MiniMax-M3` | Vision + reasoning model. |
| `filePath` | Worklog file path | `%USERPROFILE%\Documents\Quarterlog\worklog.xlsx` | The local Excel file rows are appended to. Blank falls back to the default. |
| `categories` | Categories | `VAPOMAN` | Newline/comma-separated options for the *Category from order* dropdown. |
| `types` | Types | `New` | Newline/comma-separated options for the *Type* dropdown; the AI picks one of these. |
| `intervalMinutes` | Interval (minutes) | `15` | Cadence of the automatic popup. Changing it restarts the ticker. |
| `monitor` | Monitor | `-1` | `-1` primary, `-2` all displays stitched, `>=0` a specific display index. |
| `popupPosition` | Popup position | `bottom-right` | One of `top-left`, `top-center`, `top-right`, `center-left`, `center`, `center-right`, `bottom-left`, `bottom-center`, `bottom-right`. Set via the 3×3 picker. |
| `language` | Description language | `Czech` | Language the AI writes descriptions in. |
| `prompt` | AI guidance prompt | *(see below)* | System prompt appended to each vision request. |
| `paused` | Pause capturing | `false` | Toggled from the tray; skips automatic captures. |
| `autostart` | Launch at startup | `false` | Adds/removes an `HKCU\Software\Microsoft\Windows\CurrentVersion\Run` entry. |

Default AI prompt:

> You are helping fill in a work timesheet. Look at the screenshot and write ONE concise
> sentence, in first person past tense, describing the concrete work task the user was
> doing (e.g. which app, document, ticket, or topic). Do not describe the screenshot
> itself; describe the work. No preamble.

## Worked examples

**Multiple projects** — put each order/category on its own line so they appear in the
dropdown:

```
ISDOC-CONVERTOR
LBE-RECON
INTERNAL
```

**Work types the AI can choose from:**

```
New
Support
Meeting
Bugfix
```

**Popup in the top-right at a 10-minute cadence:** set *Interval* to `10` and click the
top-right zone in the position picker.

## Security note

The API key is currently stored in plaintext in `config.json`. If the machine is shared
or backed up, treat the key accordingly. Encrypting it at rest with Windows DPIAPI
(`CryptProtectData`) is on the roadmap.
