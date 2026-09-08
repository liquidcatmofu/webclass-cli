# webclass-cli

WebClass をコマンドラインから扱うための非公式 CLI クライアントです。

特定の学校専用ではなく、WebClass 12.x 系の一般的な構成を対象にしています。現在は開発・実環境での動作確認に `https://webclass.kosen-k.go.jp/webclass/` を使用しているため、この URL を後方互換の既定値にしています。別の WebClass インスタンスでは `--base-url` を指定できます。

`webclass auth` は Microsoft / SAML のユーザー名やパスワードを保存しません。インストール済みの Chromium 系ブラウザを開き、通常どおりログインと多要素認証を完了したあと、WebClass ホストのCookieだけを保存します。

## 状態

現在は初期MVPです。

```console
webclass auth
webclass courses
webclass assignments
webclass pull --dir ./materials <course-id>
webclass completion bash|zsh|fish|powershell
```

`pull` は安全のため必ず1コースずつ実行します。コースIDは `webclass courses` で取得できます。

また、教材一覧ではカテゴリが厳密に `資料` のものだけを対象にします。`アンケート`、`レポート`、`自習`、および未対応のカテゴリは `pull` では開きません。

ダウンロードURLは一時的なものがあるため保存せず、毎回現在のURLを解決して取得します。取得したファイルは `.webclass-manifest.json` に SHA-256、サイズ、保存先などを記録します。既知資料そのものもmanifestに記録するため、ファイルが1つもない資料を毎回開き直すことはありません。

WebClassへの負荷を抑えるため、`pull` はHTTPリクエストを並列送信しません。既定ではリクエスト間に最低1秒の間隔を空けます。教材一覧を先に解析したあと、必要な資料だけを1件ずつ `開始 → 取得 → 終了` の順で処理します。開始済み資料を正常に閉じられない場合は、次の資料を開かず停止します。進捗は `[現在/総数]` で表示します。

レポート提出は副作用があるため、現時点のMVPにはまだ含めていません。

## ビルド

Go 1.23 以降と、Chrome / Chromium / Edge などの Chromium 系ブラウザが必要です。

macOS / Linux:

```sh
go build -o webclass ./cmd/webclass
```

Windows:

```powershell
go build -o webclass.exe ./cmd/webclass
```

## 認証

```sh
./webclass auth
```

`auth` はインストール済みのChromium系ブラウザを自動検出します。RodによるChromiumの自動ダウンロードは使いません。またWindows Defenderなどで誤検知されることがある `leakless` ヘルパーも無効化しています。

特定のブラウザを指定する場合:

```sh
./webclass auth --browser /path/to/chrome
```

Windowsの例:

```powershell
.\webclass.exe auth --browser "C:\Program Files\Google\Chrome\Application\chrome.exe"
```

ブラウザが開いたら、普段どおりWebClassへログインしてください。SAMLやMicrosoft 365、多要素認証などもブラウザ上でそのまま行います。

認証後はWebClassホスト用のCookieをOSのユーザー設定ディレクトリへ保存します。対応OSではファイルモード `0600` を使用します。

再認証時にブラウザ側のログイン状態を再利用できるよう、専用ブラウザプロファイルもアプリ設定ディレクトリに保存します。CLIのHTTPクライアント自体が使うのは保存したWebClass Cookieだけです。

### 別のWebClassインスタンスを使う

すべての主要コマンドで `--base-url` を指定できます。

```sh
./webclass auth --base-url 'https://example.ac.jp/webclass/'
./webclass courses --base-url 'https://example.ac.jp/webclass/'
./webclass assignments --base-url 'https://example.ac.jp/webclass/'
./webclass pull --base-url 'https://example.ac.jp/webclass/' --dir ./materials <course-id>
```

URL構成やHTMLが大きく異なるWebClassでは追加対応が必要な場合があります。

## 課題一覧を取得する

全コースの課題一覧を取得します。

```sh
./webclass assignments
```

1コースだけ確認する場合はコースIDを指定します。

```sh
./webclass assignments 02_26036
```

`assignments` はコース一覧ページと `/scores` の成績表だけを読みます。課題本文や `do_contents.php` は開かないため、アンケート・レポート・自習などの実行回数を消費しません。カテゴリが `資料` の項目は課題一覧から除外します。

表示する提出状態は `提出済` / `未提出` / `再提出` / `不明` です。成績表にタイトルが存在する場合は、WebClassの `未` を未提出、それ以外の得点表示を提出済として扱います。コース一覧に「再提出が必要です。」と表示されている場合は `再提出` を優先します。導入先で成績表を取得できない場合など、確実に判定できない項目は `不明` と表示します。

期限は `締め切り: YYYY/MM/DD HH:MM` または公開期間の終了日時を読み取り、期限が早い順に表示します。期限がない課題は後ろに並びます。

WebClassへの連続アクセスを避けるため、既定ではHTTPリクエスト間に1秒空けます。

```sh
./webclass assignments --interval 2s
```

## 資料を取得する

まずコース一覧を取得します。

```sh
./webclass courses
```

次にコースIDを1つ指定します。

```sh
./webclass pull --dir ~/Documents/webclass <course-id>
```

### pullモード

`pull` は3つのモードを切り替えられます。

```text
new    未知の資料だけ開く
files  すべての対象資料を開くが、manifestにない新規ファイルだけダウンロードする
full   すべてのファイルを再取得してSHA-256を比較し、差し替えも検出する
```

通常のデフォルトは `new` です。

```sh
./webclass pull --dir ~/Documents/webclass <course-id>
# = --mode new
```

`new` はコース一覧とローカルmanifestを比較し、既知資料は開きません。新しい資料が見つかった場合だけその資料を開き、中にあるファイルを取得します。普段の新着確認向けです。

既存資料に新しい添付ファイルが追加されていないか確認したい場合は `files` を使います。

```sh
./webclass pull --mode files --dir ~/Documents/webclass <course-id>
```

`files` は各資料を1件ずつ開きますが、manifestにありローカルにも存在する既知ファイルは本体を再ダウンロードしません。同じファイル名の中身だけが差し替えられた場合は検出できません。

差し替えまで確実に検出したい場合は `full` を使います。

```sh
./webclass pull --mode full --dir ~/Documents/webclass <course-id>
```

`full` は従来どおり全ファイルを取得してSHA-256を比較します。3モードの中でサーバ負荷と通信量が最も大きくなります。

既定のリクエスト間隔は1秒です。Goのduration形式で変更できます。

```sh
./webclass pull --interval 2s --dir ~/Documents/webclass <course-id>
```

### 特定の資料だけ取得・更新する

`--material` はコースIDより前に指定します。

```sh
./webclass pull --dir ~/Documents/webclass --material '第2回 正規表現，NFAへの変換' 02_26036
```

`--material` を指定して `--mode` を省略した場合は、その資料を実際に確認できるよう自動的に `files` モードになります。差し替えまで確認する場合は明示的に `--mode full` を指定してください。

```sh
./webclass pull --mode full --dir ~/Documents/webclass --material '第2回 正規表現，NFAへの変換' 02_26036
```

資料の選択は次の順で行います。

1. 資料タイトルの完全一致
2. WebClass contents ID の完全一致
3. 資料タイトル、`group/title`、contents ID の一意な部分一致

0件または複数件に一致した場合は、資料を1つも開かず終了します。複数候補がある場合は候補を表示します。

1つの資料に本文PDF、本文HTML、添付資料など複数ファイルがある場合は、その資料に属する対象ファイルをまとめて扱います。

## 保存ディレクトリ

既定の構成は次のとおりです。

```text
<dir>/<course>/<material>/<file>
```

WebClass側のグループ/フォルダ構成も保存したい場合は `--group-dirs` を使います。

```sh
./webclass pull --group-dirs --dir ~/Documents/webclass <course-id>
```

保存先は次のようになります。

```text
<dir>/<course>/<group>/<material>/<file>
```

例:

```text
02_言語解析演習(2026)/
  達成度試験対策/
    模範解答（第8回演習問題）/
      模範解答（第8回演習問題）.pdf
  資料/
    第1回 形式言語，正規言語/
      第1回 形式言語，正規言語.pdf
  演習問題/
    第1回 演習問題/
      第1回 演習問題.pdf
```

既に `--group-dirs` なしで取得済みの場合でも、同じ内容のファイルは再ダウンロード扱いにせず新しい階層へ移動し、manifestのパスを更新します。移動やリネームで空になった旧ディレクトリは削除しますが、ダウンロードルート自体や他のファイルが残っているディレクトリは削除しません。

## ファイル名と教材本文

WebClassでは教材本文と添付資料でファイルの扱いが異なります。

添付資料は `file_name` で元ファイル名が公開されているため、その名前を使用します。

教材本文PDFでは、`b0b6db7f1cf1e354.pdf` のような内部保存名しか取得できない場合があります。この場合、明示的な `file_name` がなければ資料タイトルをファイル名として使用します。

教材本文がPDFではなくHTMLの場合も保存対象です。1ページなら `資料タイトル.html`、複数ページなら `資料タイトル - 01.html`、`資料タイトル - 02.html` のように保存します。HTMLから参照される外部CDNや画像などを完全にオフラインミラーする機能はまだありません。

既存manifestに内部hex名で保存済みのPDFがある場合は、同じ資料IDとSHA-256が一致すれば新規ファイルにせずリネームとして移行します。

manifestには安定した資料ID、既知資料、保存パス、サイズ、コンテンツハッシュを保存し、一時的なダウンロードURLは保存しません。

## シェル補完

追加依存なしで補完スクリプトを生成できます。

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

コマンド名とオプションは直接補完されます。`--mode` の値も `new` / `files` / `full` から補完されます。

`webclass courses`、`pull`、`assignments` は取得したコース一覧をアプリ設定ディレクトリへキャッシュします。`pull <course-id>` と `assignments <course-id>` のコースID補完はこのキャッシュを使用します。

資料タイトルの補完は、指定した `--dir` 配下の `.webclass-manifest.json` を使用します。

**補完処理そのものはWebClassへアクセスしません。** Tabキーを繰り返し押してもサーバへのリクエストは発生しません。

旧版から更新した場合は、一度 `webclass courses` を実行すると、まだpullしていないコースも補完候補に入ります。

## 注意点

WebClassの学生向けHTML画面は安定した公開APIではありません。このCLIはWebClass 12.xで確認したHTML構造やフォーム動作を解析しているため、WebClassのバージョン、導入先の設定、カスタマイズによっては動作しない箇所があります。

現在のダウンロード探索は、同一オリジンのframe/srcdoc、教材開始フォーム、`filedownload(...)`、教材本文PDF/HTML、添付資料などを扱いますが、すべての教材形式を網羅しているわけではありません。

問題が出る場合は、個人情報や認証情報を除いたHTML構造やCLI出力をもとに対応を追加していく方針です。