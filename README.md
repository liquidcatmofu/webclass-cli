# webclass-cli

Unofficial CLI client for WebClass, initially targeting the National Institute of Technology (KOSEN) WebClass instance.

The CLI deliberately does **not** store your Microsoft / SAML username or password. `webclass auth` opens an installed Chromium-based browser so you can complete SAML login and two-factor authentication yourself, then stores only cookies for the WebClass host.

## Status

Early MVP. The current commands are:

```console
webclass auth
webclass courses
webclass pull --dir ./materials <course-id>
webclass completion bash|zsh|fish|powershell
```

`pull` is deliberately scoped to exactly one course. Use `webclass courses` to obtain its course ID.

For safety, pull discovery only considers entries whose WebClass category is exactly `資料`. Questionnaire, report, self-study, and any unknown/future categories are not opened by `pull`.

For downloadable material entries, `pull` resolves the current download link each time rather than persisting short-lived download URLs, downloads files, and records SHA-256 hashes in `.webclass-manifest.json`. Re-running it reports files as new, changed, or unchanged.

`pull` is intentionally polite to the WebClass server. It sends no concurrent HTTP requests and defaults to a one-second quiet interval between requests. Course/index pages are scanned first without entering materials; candidate materials are then processed strictly one at a time. For a started textbook material, the CLI downloads its files and submits the observed WebClass `資料を閉じる` action before opening the next material. If a started material cannot be closed or its close action cannot be recognized, pull stops instead of opening another material. Progress is printed as `[current/total]` for each candidate.

Assignment submission is intentionally not included in the first MVP because it has side effects and needs captured/verified WebClass form behavior before automating it.

## Build

Requires Go 1.23+ and an installed Chromium-based browser (Chrome, Chromium, or Edge).

macOS/Linux:

```sh
go build -o webclass ./cmd/webclass
```

Windows:

```powershell
go build -o webclass.exe ./cmd/webclass
```

## Authentication

```sh
./webclass auth
```

`auth` auto-detects an installed Chromium-based browser and does not ask Rod to download its pinned Chromium build. Rod's `leakless` helper is also disabled for this flow because the browser is closed explicitly and the helper executable can trigger antivirus false positives on Windows.

To use a specific browser executable:

```sh
./webclass auth --browser /path/to/chrome
```

On Windows, for example:

```powershell
.\webclass.exe auth --browser "C:\Program Files\Google\Chrome\Application\chrome.exe"
```

A browser window opens. Complete the normal SAML/Microsoft sign-in and 2FA flow. Once WebClass is reached, the CLI stores WebClass cookies under your OS user config directory with file mode `0600` where supported by the OS.

The dedicated browser profile is also stored under the app config directory so a later re-authentication can reuse browser-side sign-in state. The HTTP client itself uses only the saved WebClass cookies.

## Download materials

First list courses:

```sh
./webclass courses
```

Then select exactly one course by ID:

```sh
./webclass pull --dir ~/Documents/webclass <course-id>
```

The default request interval is one second. It can be changed explicitly with Go duration syntax:

```sh
./webclass pull --interval 2s --dir ~/Documents/webclass <course-id>
```

To update only one WebClass material, use `--material` before the course ID:

```sh
./webclass pull --dir ~/Documents/webclass --material '第2回 正規表現，NFAへの変換' 02_26036
```

The selector checks, in order, an exact material title, an exact WebClass contents ID, then a unique substring of the material title, `group/title`, or contents ID. If the selector matches zero or multiple materials, the command exits before opening any material and prints the ambiguous candidates when applicable. A selected material can contain more than one downloadable file; all downloadable files belonging to that material are updated together.

The default layout is:

```text
<dir>/<course>/<material>/<file>
```

To preserve WebClass's folder/group structure, use `--group-dirs`:

```sh
./webclass pull --group-dirs --dir ~/Documents/webclass <course-id>
```

This produces:

```text
<dir>/<course>/<group>/<material>/<file>
```

For example:

```text
02_言語解析演習(2026)/
  達成度試験対策/
    模範解答（第8回演習問題）/
      answer.pdf
  資料/
    第1回 形式言語，正規言語/
      slides.pdf
  演習問題/
    第1回 演習問題/
      exercise.pdf
```

If an existing manifest was created without `--group-dirs`, running with the option later relocates unchanged files into the grouped layout and updates the manifest path rather than treating them as changed downloads. Empty directories left behind by relocation/renaming are pruned only when they are actually empty; the configured download root and directories containing other files are never removed.

WebClass handles textbook-body PDFs and downloadable attachments differently. Attachments expose their original filename through `file_name`, so that name is preserved. A textbook-body PDF may only expose an internal hexadecimal storage basename such as `b0b6db7f1cf1e354.pdf`; when no explicit `file_name` exists, the CLI names that PDF after the WebClass material title instead. Existing manifest entries using the old opaque basename are migrated by matching the same material ID and SHA-256 hash.

The manifest stores stable material identity, file path, size and content hash; it does not store resolved short-lived download URLs.

## Shell completion

Completion scripts are generated without an extra dependency.

Bash:

```sh
source <(webclass completion bash)
```

Zsh:

```sh
source <(webclass completion zsh)
```

Fish:

```fish
webclass completion fish | source
```

PowerShell:

```powershell
webclass.exe completion powershell | Out-String | Invoke-Expression
```

Commands and flags are completed directly. Course IDs and material titles are read only from the local `.webclass-manifest.json` under the selected `--dir`; shell completion never contacts WebClass, so repeated Tab presses do not create server load. This also means a course/material that has never appeared in the local manifest may need to be typed manually the first time.

## Instance override

All commands accept `--base-url`. For example:

```sh
./webclass courses --base-url 'https://example.ac.jp/webclass/'
```

## Caveats

WebClass is not a stable public HTML API. This project currently uses selectors and material-close form behavior observed on WebClass 12.x and may need parser updates when the site changes. The download discovery code follows same-origin frames and WebClass `filedownload(...)` links, but not every material type is covered yet.
