# webclass-cli

Unofficial CLI client for WebClass, initially targeting the National Institute of Technology (KOSEN) WebClass instance.

The CLI deliberately does **not** store your Microsoft / SAML username or password. `webclass auth` opens a normal Chromium window so you can complete SAML login and two-factor authentication yourself, then stores only cookies for the WebClass host.

## Status

Early MVP. The current commands are:

```console
webclass auth
webclass courses
webclass pull --dir ./materials <course-id>
```

`pull` is deliberately scoped to exactly one course. Use `webclass courses` to obtain its course ID.

For safety, pull discovery only considers entries whose WebClass category is exactly `資料`. Questionnaire, report, self-study, and any unknown/future categories are not opened by `pull`.

For directly downloadable material entries, `pull` resolves the current download link each time rather than persisting short-lived download URLs, downloads files, and records SHA-256 hashes in `.webclass-manifest.json`. Re-running it reports files as new, changed, or unchanged.

Materials that require pressing WebClass's `開始` button are not entered yet. Starting a WebClass material can update usage history and may consume an execution-count limit, so this must be handled conservatively rather than treating every material as safe to start.

Assignment submission is intentionally not included in the first MVP because it has side effects and needs captured/verified WebClass form behavior before automating it.

## Build

Requires Go 1.23+ and a Chromium-compatible browser that Rod can launch.

```sh
go build -o webclass ./cmd/webclass
```

## Authentication

```sh
./webclass auth
```

A Chromium window opens. Complete the normal SAML/Microsoft sign-in and 2FA flow. Once WebClass is reached, the CLI stores WebClass cookies under your OS user config directory with file mode `0600`.

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

Files are stored below the selected directory by course and material title. The manifest stores stable material identity, file path, size and content hash; it does not store resolved short-lived download URLs.

## Instance override

All commands accept `--base-url`. For example:

```sh
./webclass courses --base-url 'https://example.ac.jp/webclass/'
```

## Caveats

WebClass is not a stable public HTML API. This project currently uses selectors observed on WebClass 12.x and may need parser updates when the site changes. The download discovery code follows same-origin frames and WebClass `filedownload(...)` links, but not every material type is covered yet.
