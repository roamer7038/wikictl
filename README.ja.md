# wikictl

[English](README.md)

wikictl は、Git リポジトリに置いた Markdown の wiki を操作するコマンドラインツールです。AI エージェントと人間が共有する知識ベースを想定しています。エージェントはシェルからページを検索・記録し、人間は同じページを Git ホストの Web UI や、Obsidian などのエディタで開いたクローンで読み書きします。クローンで編集する人は、通常どおりコミットして push します。wikictl はクローンを使わず、wikictl の変更とクローンからの変更はリポジトリ上で合流します。

リポジトリは Git ホスト（GitHub、GitLab、Gitea など）、SSH で接続できるサーバー、ローカルのディレクトリのいずれにも置けます。ホスティングサービスは必須ではありません。

wikictl はサーバーを起動せず、インデックスも作業ツリーも持ちません。読み取りでは bare ミラーに fetch して `git grep`、`git cat-file`、`git log` を使い、書き込みでは git の plumbing コマンドでコミットを作成して `--force-with-lease` で push します。wiki 自体は wikictl に依存しない素の Markdown なので、どのエディタでも扱えます。

```mermaid
flowchart LR
  agent["AI エージェント / シェル"] -->|wikictl| mirror["bare ミラー<br>~/.cache/wikictl/"]
  mirror <-->|"fetch / push --force-with-lease"| repo[("wiki リポジトリ")]
  person["人間"] <-->|"Web UI、またはクローンして push"| repo
```

## 前提条件

- Linux または macOS。Windows などその他の OS でもビルドはできるが、ミラーをロックできないため、wiki を読み書きするコマンドはすべて終了コード 5 で失敗する
- `PATH` 上に `git` があること
- 対話なしで wiki リポジトリから fetch し、push できること（credential helper、SSH エージェント、ローカルパスならファイルへのアクセス権）。wikictl は git の対話プロンプトを無効にして実行するため、パスワードなどの入力が必要な場合は、入力を待たずに失敗する。すべてのコマンドは最初に fetch するため、読み取りだけでもこの条件が必要
- wiki のブランチへ直接 push できること。プルリクエストを必須にするブランチ保護があると、書き込みはすべて失敗する
- インストールスクリプトを使う場合は `curl` と、`sha256sum` または `shasum`。ソースからビルドする場合は Go 1.26.5 以降

### wiki リポジトリの置き場所

設定の `repo`（[設定](#設定)を参照）はそのまま git に渡されるため、git が push できる URL やパスなら何でも指定できます。

| 置き場所 | `repo` の例 |
|---|---|
| Git ホスト | `git@github.com:you/wiki.git` |
| SSH で接続できるサーバー | `ssh://you@server.example/srv/git/wiki.git` |
| ローカルのディレクトリ | `/home/you/wiki.git` |

サーバーやローカルに置く場合は、初期ブランチを指定して空の bare リポジトリを作成します。

    git init --bare -b main ~/wiki.git

空のリポジトリでは remote HEAD からブランチを決められないため、wikictl は `main` に書き込みます。`-b main` を付けずに作成して HEAD が `master` を指していると、`git clone` は「remote HEAD refers to nonexistent ref」と警告し、チェックアウトしません。`-b main` を付けて作成するか、設定の `branch` に HEAD が指すブランチ名を指定してください。

## インストール

最新リリースを `~/.local/bin` にインストールします。

    curl -fsSL https://raw.githubusercontent.com/roamer7038/wikictl/main/install.sh | sh

スクリプトは OS とアーキテクチャに合うバイナリを選び、チェックサムを検証します。`~/.local/bin` が `PATH` に含まれていなければ警告を表示します。特定のバージョンをインストールするには `WIKICTL_VERSION`（例：`v0.2.0`）を、インストール先を変えるには `WIKICTL_INSTALL_DIR` を設定します。

Go でソースからビルドすることもできます。

    go install github.com/roamer7038/wikictl/cmd/wikictl@latest

Linux と macOS（x86_64、arm64）のバイナリとチェックサムは [Releases ページ](https://github.com/roamer7038/wikictl/releases) にあります。

## クイックスタート

1. Git ホスト上か `git init --bare -b main` で空のリポジトリを作成し（[wiki リポジトリの置き場所](#wiki-リポジトリの置き場所)を参照）、対話なしで `git push` できることを確認します。
2. `~/.config/wikictl/config.yaml` を作成します。

       repo: git@github.com:you/wiki.git
       author:
         name: claude-code@laptop
         email: claude-code@laptop.invalid

3. 設定を確認し、初期ページを作成してから、ページを書き込んで読み出します。

       wikictl context
       wikictl init
       printf -- '---\nsummary: --force-with-lease 付きの push は、リモートの ref が期待する sha のままでなければ拒否される\n---\n# --force-with-lease は何を保証するか\n\n本文。\n\n## Links\n- part_of: [index](index.md)\n' \
         | wikictl put global/git-force-with-lease.md
       wikictl search lease
       wikictl get global/git-force-with-lease.md
       wikictl lint

`help` 以外のどのコマンドも、`--json` を付けるとプログラムで処理しやすい JSON を出力します。

## wiki の構成

### スコープ

最上位の各ディレクトリは、「その知識がどこで有効か」を表すスコープです。

| ディレクトリ | 有効な範囲 |
|---|---|
| `global/` | 全員 |
| `personal/` | このユーザーのみ。マシンやプロジェクトは問わない（コミット規約、用途ごとのアカウント、使うツールなど） |
| `projects/<name>/` | 特定のプロジェクト |
| `machines/<name>/` | 特定の実行環境 |

ページは、当てはまる中で最も狭いスコープに置きます。このプロジェクトだけ → `projects/<name>/`、この実行環境だけ → `machines/<name>/`、このユーザーだけ → `personal/`、それ以外 → `global/` です。

`personal/` には、エージェントが必要になったときに検索して参照する事実を置きます。すべての会話に適用すべきルールは wiki ではなく、エージェントに常に読み込まれる指示（Claude Code なら `CLAUDE.md`）に書きます。

`personal/` は wiki を 1 人で使うことを前提にしています。wiki を共有する全員が同じ `personal/` を検索するため、複数人で共有する wiki では `personal/` を使わないか、設定の `dirs` で検索対象のディレクトリを指定してください。

`init` が作成するのは `global/` だけです。その他のディレクトリは、そのディレクトリに最初のページを `put` したときに作られます。

### 検索対象のディレクトリ

`search`、`ls`、`lint` は、既定で次の最大 4 つのディレクトリを対象にします。

- `global/`
- `personal/`
- `projects/<name>/`：`<name>` は、カレントディレクトリの `origin` リモート URL に含まれるリポジトリ名（最後のパス要素から `.git` を除き、小文字にしたもの）。設定の `projects` で別のディレクトリ名に対応付けられる。カレントディレクトリが git リポジトリの中にない場合や、`origin` リモートがない場合は対象外
- `machines/<name>/`：`<name>` は設定の `machine`。指定がなければ、ホスト名の最初の `.` までを小文字にしたもの

コマンドラインの `--dirs a,b` または設定の `dirs` で、この一覧を置き換えられます。`--dirs .` とすると wiki 全体が対象になります。存在しないディレクトリは無視されます。`wikictl context` で、一覧と各ディレクトリ配下（サブディレクトリを含む）のページ数を確認できます。

### ページ形式

ページは Markdown ファイルで、リポジトリのルート直下ではなく、いずれかのディレクトリの中に置きます。フロントマターには 1 行の `summary` を書くことを推奨します。

```markdown
---
summary: このページが答える問いを 1 文で
type: concept
---
# タイトル

本文。他のページへは相対パスでリンクする: [index](index.md)。

## Links
- part_of: [index](index.md)
- cites: https://example.com/spec | この出典が裏付ける内容
```

フロントマター：

- `summary` は推奨で、必須ではない。`summary` がないページでも `put` は `missing_summary` の警告を出して書き込み、`lint` は `missing_summary` を報告する。`search` と `ls` は代わりにタイトル（最初の見出し、なければファイル名）を表示する。フロントマター自体がないページも同じ扱い
- `description` は `summary` と同じ意味のキーとして扱う。両方ある場合、`summary` が空でない文字列であれば `summary` を優先する
- wikictl が解釈するキーは、`type`（`ls --type`）、YAML のリストとして書いた `tags`（`ls --tag`）、`aliases`（`mv` が元のファイル名を追加する）、`status`。`review_after` などその他のキーも書けるが、wikictl は解釈しない
- `status: deprecated`（引用符なし）の行がファイル内のどこかにあるページは、`--all` を付けない限り `search` と `ls` に表示されない

サイズの上限：

- フロントマターが 64 KiB（65,536 バイト）を超える場合、またはコレクションのネストが 100 段を超える場合は `frontmatter_invalid` とする。ブロック形式のマッピングと配列、フロー形式のコレクション（`[...]`、`{...}`）はそれぞれ 1 段と数える
- 1 MiB（1,048,576 バイト）を超えるページは解析せず、`page_too_large` とする
- `put` はこれらのページを拒否する。wiki に既にあるページは `lint` が報告し、`search`、`ls`、`get` はフロントマターがないページとして扱う。1 MiB を超えるページは本文も空とし、タイトルはファイル名とする

リンク：

- ページへのリンクは `.md` で終わる相対パスで書く。`#fragment` を付けてもよい。絶対パスや wiki の外を指すパスは、ページへのリンクとして扱わない
- `## Links` 見出しは、ページの最後の見出しである場合だけ Links セクションの開始になる。後ろに別の見出しがあると、その行は本文として扱われ、関係は警告なしに無視される
- セクションの各行は `- <type>: <target> | <note>` の形式で、`<target>` は相対パスまたは URL、`| <note>` は省略できる
- `- <target>` のように type を省いた行は `see_also` として扱う。type を省いて URL を書く場合は `<scheme>://...` の形式にする
- 箇条書きの記号は `-`、`*`、`+` のいずれでもよく、インデントしてもよい
- 本文中の `[text](path)` 形式のリンクも読み取る。`get` はリンク先のバックリンクに `mentions` として表示し、`lint` はリンク先が存在しなければ報告する
- ページへのリンクは `[text](path)` の形式で書く。`mv` が書き換えるのはこの形式だけで、`- part_of: index.md` や `- index.md` のような素のパスは書き換えないため、リンク先を移動すると壊れたリンクになる
- コードフェンスの中は解釈しない。フェンス内の `## Links` 見出しはセクションの開始にならない。コードフェンス内のリンクと、本文のインラインコード内のリンクは、本文中のリンクとして読み取らない（ただし `mv` はインラインコード内のリンクも書き換える）

ファイル名とディレクトリ名：

- 空の名前、`.` か `<` で始まる名前、空白・制御文字・``" \ # ? : ( ) ` `` のいずれかを含む名前は使えない。ページのファイル名は `.md` で終わる必要がある。これに反するパスは `put`、`mv`、`rm` が拒否する（`bad_path`、終了コード 4）
- 名前には小文字の ASCII 英字、数字、ハイフンを使い、ハイフンで始めないことを推奨する。それ以外の名前は `lint` が `name_style` として報告する。同じディレクトリ内で大文字と小文字だけが異なる名前は、大文字と小文字を区別しないファイルシステムで衝突するため、`case_collision` として報告する

`index.md`：

各ディレクトリには `index.md` を置き、その `summary` にディレクトリに置く内容を書くことを推奨します。同じディレクトリの他のページからは、`- part_of: [index](index.md)` で `index.md` にリンクします。こうすると `wikictl get <dir>/index.md` がそれらのページを `backlinks` として一覧し、`wikictl dirs` がディレクトリの横にその `summary` を表示します。目次などのファイルを生成しなくても、wiki の構造をページ自身から読み取れます。

## コマンド

| コマンド | 説明 |
|---|---|
| `init` | ブランチがまだないリポジトリに `README.md` と `global/index.md` を作成する |
| `search <word>...` | すべての語を含むページを検索する |
| `get <path>` | 1 ページの sha、フロントマター、本文、リンク、バックリンクを表示する |
| `ls` | 検索対象ディレクトリ配下のページを一覧表示する |
| `put <path> < content` | 標準入力の内容でページを作成または置換する |
| `mv <path> <newpath>` | ページを移動・名前変更し、そのページへのリンクを書き換える |
| `mv <dir>/ <newdir>/` | ディレクトリ配下の全ページを移動する |
| `rm <path>` | ページを削除する |
| `lint [<path>...]` | 形式違反を報告する |
| `dirs [<dir>...]` | wiki 全体のディレクトリを、ページ数と `index.md` の summary とともに一覧表示する |
| `context` | 解決済みの設定と検索対象ディレクトリを、ページ数とともに表示する |
| `help [<command>]` | コマンド一覧、または 1 つのコマンドの説明とフラグを表示する |
| `version` | バージョンを表示する |

コマンドのフラグは、`wikictl search -n 5 lease` のように引数より前に書きます。引数の後ろに書いたフラグは引数として扱われます。たとえば `wikictl search lease -n 5` は、`lease`、`-n`、`5` の 3 語を検索します。

| フラグ | コマンド | 意味 |
|---|---|---|
| `--any` | `search` | すべての語ではなく、いずれかの語を含むページを検索する |
| `-n <N>` | `search` | 最大 N 件を表示する（既定値 20）。N は 1 以上 |
| `--all` | `search`、`ls` | `status: deprecated` のページも含める |
| `--type <type>` | `ls` | `type` がこの値のページだけを表示する |
| `--tag <tag>` | `ls` | このタグを持つページだけを表示する |
| `--base <sha>` | `put` | 既存ページの blob sha（`get` が表示する値）を指定し、ページがその sha のままの場合だけ書き込む |
| `-m <message>` | `put`、`mv`、`rm` | コミットメッセージを指定する（既定値は `wikictl: <command> <arguments>`） |

グローバルフラグ（コマンド名の前後どちらにも指定できる）：

| フラグ | 意味 |
|---|---|
| `--json` | JSON で出力する（`help` を除く） |
| `--dirs a,b` | 指定したディレクトリだけを検索対象にする。`.` は wiki 全体 |
| `--config <path>` | 指定したファイルから設定を読む |
| `--profile <name>` | 指定したプロファイルを使う |
| `--no-fetch` | 読み取り前の fetch を省略する |
| `--version` | バージョンを表示する |

`--no-fetch` を付けても、書き込み時にコミットの前に行う fetch は省略されません。ただし `mv` は fetch していないミラーの内容から変更を作るため、より新しい変更を上書きすることがあります。`mv` と `--no-fetch` は組み合わせないでください。

各コマンドの詳細：

- `init` は 2 つのファイルを 1 つのコミットで対象のブランチに書き込む。ブランチが既に存在する場合は終了コード 1 で失敗する
- `search` は、大文字と小文字（ASCII 以外の文字を含む）を区別せず、各語を固定文字列としてフロントマターを含むファイル全体から探す。結果は更新日時の新しい順に並ぶ。`--any` の場合は、一致した語の多いページが先に並ぶ
- `get` は、保存されているファイルそのものではなく、ページを解析した結果を表示する。テキストでは、フロントマターと Links セクションを除いた本文、リンク、他のページからのバックリンクを表示する。フロントマターは `--json` の場合だけ含まれる
- `put` には、フロントマターを含むページ全体を標準入力で渡す。不正なフロントマター、サイズの上限を超えるページ（`frontmatter_invalid`、`page_too_large`）、不正なパスは終了コード 4 で拒否する。それ以外の問題（`missing_summary`、`broken_link`、`links_syntax`、`name_style`）は警告を表示したうえでページを書き込む。内容が現在のページと同じ場合はコミットを作らず、`commit` には現在のコミットを返す
- `mv` は、移動したページ内のリンクと、他のページからそのページへのリンクを同じコミットで書き換える。このとき wiki 内のすべてのページで、別の書き方をした相対リンクも正規化する（例：`./b.md` → `b.md`）ため、移動と無関係なページがコミットに含まれることがある。書き換えたページの本文は改行が LF になり、BOM は除去される。ファイル名が変わる場合は、元のファイル名（`.md` を除く）を `aliases` に追加する（既に含まれていれば追加しない）。追加するのは、ページのフロントマターが空かブロック形式の YAML のマッピングで、`aliases` がない、配列（ブロック形式またはフロー形式）、または null（`aliases:`、`aliases: ~`、`aliases: null`）の場合だけ。それ以外の場合（`aliases` が文字列の場合や、フロントマターがない場合など）は、警告を出さずに alias を追加しないまま移動する。移動先が既に存在する場合は終了コード 1 で失敗する。ディレクトリを移動する形式では、`<newdir>/` の配下にページが 1 つでもあれば失敗する
- `rm` は、削除したページへリンクしている他のページを書き換えない。それらのリンクは `lint` が `broken_link` として報告する。`rm` が削除するのはページだけで、ファイル名の規則に反するパス（wiki のルートにある `README.md` など）は終了コード 4 で拒否する。そのようなファイルを削除するには、wiki のリポジトリを clone して git で直接操作する
- `lint` は、指定したページ、または検索対象ディレクトリ配下の全ページを検査する。wiki 全体を検査するには `wikictl --dirs . lint` を使う。`case_collision` は常に wiki 全体を対象に検査する
- `dirs` は、ページを直接含むすべてのディレクトリを、直下のページ数（deprecated のページを含む）と `index.md` の `summary`（`index.md` がなければ `(no index)`）とともに一覧表示する。検索対象ディレクトリの設定は無視する。`wikictl dirs projects` のように引数を指定すると、`projects/` 配下のディレクトリだけに絞り込める
- `context` は、設定ファイル、選択されたプロファイルとその選択経緯、リポジトリ、ミラー、ブランチ、author、マシン名とプロジェクト名、カレントディレクトリの `origin` リモート、検索対象ディレクトリとそのページ数を表示する

`wikictl help <command>` で、各コマンドの説明、フラグ、JSON 出力のフィールドを確認できます。

## 使い方

### 他人の変更を上書きせずにページを更新する

1. `wikictl get <path>` を実行し、ページの blob `sha` を確認します。
2. 新しい内容を書き、その sha を `--base` に指定して `put` します。

       wikictl put --base <sha> <path> < page.md

3. その間にページが変更されていた場合、`put` は終了コード 3 で終了し、現在の内容と `sha` を出力します（[出力](#出力)を参照）。内容を読み直して変更を再適用し、もう一度 `put` してください。

既存のページに `--base` なしで書き込んだ場合も、衝突として扱います（終了コード 3）。wikictl が自動でマージすることはなく、競合状態のページが作られることもありません。

`get` の出力はページを解析した結果で、保存されているファイルそのものではありません。保存されている内容そのものは、`put` が衝突を報告したときに出力されます。

同じミラーを使う wikictl はミラーをロックし、1 つずつ書き込みます。ミラーは `repo` の値とキャッシュディレクトリごとに分かれるため、`XDG_CACHE_HOME` が異なる場合や、`repo` の書き方が異なる場合（SSH と HTTPS など）はロックを共有しません。別の push が先にリポジトリへ届いた場合、wikictl は fetch し直して `--base` を再確認し、再試行します。3 回目も失敗すると終了コード 5 で終了します。

### ページの移動と削除

`wikictl mv global/old.md global/new.md` でページを移動すると、そのページへの `[text](path)` 形式のリンクがすべて書き換わります。`wikictl mv projects/app/ projects/app-v2/` でディレクトリごと移動できます。素のパスで書いたリンクは書き換わらないため、移動後に `wikictl --dirs . lint` で wiki 全体を確認してください。

`mv` には `--base` がありません。`mv` がリンク元のページを読み取ってから push するまでの間に、別の場所からそのページへ push された変更は上書きされます。`--no-fetch` を付けると起こりやすくなります。

`wikictl rm <path>` は、リンク元のページを変更せずにページを削除します。壊れたリンクは `wikictl --dirs . lint` で一覧できます。

### wiki の構造を把握する

ページの置き場所を決める前に、`wikictl dirs` で wiki のすべてのディレクトリを、ページ数と `index.md` の `summary` とともに確認できます。`wikictl context` では、カレントディレクトリから `search`、`ls`、`lint` がどのディレクトリを対象にするかを確認できます。

## 設定

wikictl は、次のうち最初に当てはまるものを設定ファイルとして読みます。

1. `--config <path>` で指定したファイル
2. `$WIKICTL_CONFIG` が指すファイル
3. `$XDG_CONFIG_HOME/wikictl/config.yaml`（`$XDG_CONFIG_HOME` が未設定なら `~/.config/wikictl/config.yaml`）

| キー | 必須 | 意味 |
|---|---|---|
| `repo` | 必須 | wiki リポジトリの URL またはパス。プロファイル側で設定してもよい |
| `branch` | 任意 | 使うブランチ。省略時はミラーに保存されたブランチ、それもなければ remote HEAD、決められなければ `main`（[ミラー](#ミラー)を参照） |
| `author.name`, `author.email` | 任意 | コミットの author と committer。それぞれ個別に、カレントディレクトリで見た `git config user.name`、`user.email` にフォールバックする。どちらかが空のままなら、書き込みは終了コード 2 で失敗する |
| `machine` | 任意 | `machines/<name>/` の `<name>`。省略時はホスト名の最初の `.` まで |
| `dirs` | 任意 | 既定の 4 つの代わりに使う、検索対象ディレクトリの固定リスト |
| `projects` | 任意 | `origin` リモートのリポジトリ名から、`projects/` 配下のディレクトリ名への対応表 |
| `profiles` | 任意 | 上記のキーを上書きする名前付きプロファイル。後述 |
| `default_profile` | 任意 | 他の規則でプロファイルが決まらないときに使うプロファイル |

未知のキー（`default_profle` や `match.remote` のような打ち間違いを含む）は設定エラー（終了コード 2）になり、メッセージにキー名が表示されます。打ち間違いによって、気づかないうちに別の wiki が選ばれることはありません。

### プロファイル

プロファイルを使うと、個人用と業務用のように複数の wiki を 1 つの設定ファイルで扱えます。最上位のキーが既定値になり、プロファイルがそれを上書きします。

```yaml
author:
  name: claude-code@laptop
default_profile: personal
profiles:
  personal:
    repo: git@github.com:you/wiki.git
    author:
      email: you@example.invalid
  work:
    repo: git@github.example.com:team/wiki.git
    author:
      email: you@company.example
    match:
      remotes: ["github.example.com/team/*"]
      paths: ["~/work"]
```

プロファイルは、次のうち最初に当てはまるもので決まります。

1. `--profile <name>`
2. `$WIKICTL_PROFILE`
3. `match`。`remotes` と `paths` のいずれかに一致したプロファイルが選ばれる
   - `remotes` は、カレントディレクトリの `origin` リモートに対する glob パターン。リモートは、スキーム、ユーザー、ポート、`.git` を除いた小文字の `host/path` の形式にそろえてから比較するため、同じリポジトリの SSH と HTTPS の URL は同じパターンに一致する
   - 途中の `*` はパス要素 1 つに一致し、末尾の `/*` はそれより下のすべてのパスに一致する。たとえば `gitlab.example.com/team/*` は `team/app` とサブグループのリポジトリ `team/sub/app` に一致するが、`team` 自体には一致しない。`gitlab.example.com/*/app` は `team/app` に一致するが、`team/sub/app` には一致しない
   - `paths` には、絶対パスまたは `~` で始まるパスでディレクトリを指定する。カレントディレクトリがそのディレクトリ自体か、その配下であれば一致する
   - 複数のプロファイルに一致した場合、コマンドは終了コード 2 で失敗する
4. `default_profile`
5. プロファイルなし。最上位のキーだけを使う

存在しないプロファイル名を指定するとエラー（終了コード 2）になります。プロファイル内の `author.name` と `author.email` は個別に上書きされ、`dirs` と `projects` は最上位の値を丸ごと置き換えます（プロファイルの `dirs` は既定の 4 つのディレクトリも置き換えます）。プロファイルが `repo` を設定した場合、`branch` は継承されません。`wikictl context` で、選択されたプロファイル、その選択経緯、リポジトリを確認できます。

## 出力

`--json` を付けない場合、コマンドは標準出力にテキストを出力します。`--json` を付けると、`help` 以外のコマンドは JSON オブジェクトを 1 つ出力します。そのフィールドは `wikictl help <command>` で確認できます。

テキスト出力では、`ls`、`search`、`dirs` が表示する summary と title、`lint` が表示するメッセージに含まれるタブ以外の制御文字（U+0000〜U+001F、U+007F、U+0080〜U+009F）を `\xNN` の形式（例: ESC は `\x1b`）で表示します。ページの内容で端末を操作されないようにするためです。JSON 出力と `get` が表示する本文は変わりません。

警告は、どちらの場合も標準エラーに 1 行ずつ出力されます。

    wikictl: warning: <path>:<line>: <code>: <message>

エラーは標準エラーに `wikictl: <message>` の形式で出力されます。`--json` の場合は標準出力に `{"error": "<kind>", "message": "..."}` の形式で出力され、`<kind>` は `error`、`usage`、`conflict`、`invalid`、`git` のいずれかです。

`put` が衝突を報告するときは、現在のページの内容も出力します。テキストでは、標準エラーに次の 1 行を、標準出力に現在の内容を出力します。

    wikictl: conflict (<reason>): <path> sha=<sha>

`--json` の場合：

    {"error": "conflict", "reason": "<reason>", "path": "...", "sha": "...", "content": "...", "message": "..."}

`<reason>` は、`--base` なしで書き込もうとしたページが既に存在する場合は `exists`、ページの sha が `--base` で指定した値と異なる場合は `changed` です。ページが削除されていた場合、`sha` と `content` は空になります。`message` は、ページが既に存在する、変更された、削除された、のどれに当たるかを説明します。

### 終了コード

| コード | 意味 |
|---|---|
| 0 | 成功 |
| 1 | エラー（ページが存在しない場合など） |
| 2 | 使い方または設定の誤り |
| 3 | 衝突（ページが既に存在する、または読み取った後にページが変更・削除された） |
| 4 | ページが wiki の形式に違反している。`put` は不正なフロントマター、サイズの上限を超えるページ、不正なパスを、`mv` は不正な移動元または移動先のパスを、`rm` は不正なパスを拒否する。`lint` は指摘が 1 件でもあれば 4 で終了する |
| 5 | git コマンドの失敗 |

### lint のコード

`lint` は指摘を 1 行に 1 件ずつ `<path>:<line>: <code>: <message>` の形式で出力します。行番号 0 はファイル全体に対する指摘です。

| コード | 内容 | `put` での扱い |
|---|---|---|
| `bad_path` | パスがファイル名の規則に反している | 拒否 |
| `frontmatter_invalid` | フロントマターが正しい YAML でない、またはサイズかネストの上限を超えている | 拒否 |
| `page_too_large` | ページが 1 MiB を超えている | 拒否 |
| `missing_summary` | `summary` も `description` もない、またはフロントマターがない | 警告 |
| `links_syntax` | Links セクションの行がリンク行の形式になっていない | 警告 |
| `broken_link` | リンク先のファイルが wiki リポジトリに存在しない | 警告 |
| `name_style` | 名前が小文字の ASCII 英字、数字、ハイフンだけでできていない、またはハイフンで始まる | 警告 |
| `case_collision` | 同じディレクトリ内に、大文字と小文字だけが異なる名前がある | 検査しない |

## ミラー

wikictl は、`repo` の値ごとに 1 つの bare ミラーを `$XDG_CACHE_HOME/wikictl/`（`$XDG_CACHE_HOME` が未設定なら `~/.cache/wikictl/`）に置きます。パスは `wikictl context` で確認できます。

ミラーの名前は、`repo` の最後のパス要素から `.git` を除いたものに、`-` と `repo` の値全体の SHA-256 の先頭 12 桁（16 進数）を続けたものです（例: `wiki-0123456789ab`）。wikictl は、ミラーの `remote.origin.url` が `repo` と一致する場合だけそのミラーを使い、一致しなければ終了コード 5 で終了します（`mirror <path> is for another repository (its remote.origin.url is not the configured repo); delete it and run the command again`）。

wikictl 0.2.x 以前が作ったミラーは、`repo` の値全体の `/`、`:`、`@`、`\` を `_` に置き換えた名前です（例: `_srv_wiki.git`）。これらは使われなくなり、初回の実行時に新しいミラーが作られます（fetch が 1 回余分にかかります。wiki の内容はリモートにあるので失われません）。`$XDG_CACHE_HOME/wikictl/` にある旧ミラーのディレクトリは手動で削除してください。隣に `<名前>.lock` ファイルがあれば、それも削除してかまいません。

- 使っているブランチはミラーに保存される。`branch` を設定していない場合は保存されたブランチを使い、何も保存されていないときだけ remote HEAD から決める。そのため、リモートの既定ブランチを変えた場合や、設定から `branch` を削除した場合は、`branch` を設定するかミラーを削除するまで追従しない。同じ `repo` を使うプロファイルのうち `branch` を設定していないものは、他のプロファイルが最後に保存したブランチを使う
- ミラーで動かす git には、`git rev-parse --local-env-vars` が挙げるリポジトリローカルな環境変数（`GIT_DIR`、`GIT_WORK_TREE`、`GIT_INDEX_FILE` など）と `GIT_NAMESPACE` を渡さない。そのため、別のリポジトリの git フックやエイリアスから呼び出しても、wiki のリポジトリを操作する。`git -c` や `GIT_CONFIG_COUNT` で渡した設定もこれらの環境変数に含まれ、ミラーには適用されない。こうした設定は git の設定ファイルに書く。`GIT_SSH_COMMAND` や `GIT_CONFIG_GLOBAL` など、それ以外の環境変数は渡す
- ミラーが壊れた場合は削除する。次のコマンド実行時に作り直される
- ミラーがまだなく、そのパスに git リポジトリでないファイルや空でないディレクトリがある場合、wikictl はそれに触れずに終了コード 5 で終了する（`mirror <path> is not a git repository; delete it and run the command again`）。空のディレクトリであればミラーに置き換える

## 開発

    go test ./...
    go build -o wikictl ./cmd/wikictl

GitHub Actions は、`main` への push とプルリクエストで、Linux での `gofmt -l`、`go vet`、`go test -race`、macOS での `go test -race`、`staticcheck` と `go mod tidy` で `go.mod` と `go.sum` が変わらないことの確認、`govulncheck`、`install.sh` に対する `shellcheck` を実行します。`v` で始まるタグを push すると、GoReleaser がバイナリと `checksums.txt` をビルドし、リリースとして公開します。

ブランチ、プルリクエスト、リリースの運用ルールは [CONTRIBUTING.md](CONTRIBUTING.md) を参照してください。

## ライセンス

[MIT](LICENSE)
