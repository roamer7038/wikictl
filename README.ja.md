# wikictl

[English](README.md)

wikictl は、Git ホスト（GitHub、GitLab、Gitea など）上のリポジトリに置いた Markdown wiki を扱うコマンドラインツールです。AI エージェントと人が共有する知識基盤を想定しています。エージェントはシェルからページを検索・記録し、人は同じページを Git ホストの Web UI や Obsidian で読み書きします。

wikictl はサーバを持たず、索引も作業ツリーも持ちません。bare ミラーに fetch し、`git grep`、`git cat-file`、`git log` で読み、git の plumbing コマンドでコミットを組み立てて `--force-with-lease` で push します。wiki は wikictl に依存しません。どのエディタでも扱える素の Markdown です。

## 導入

`git` が `PATH` 上にあること。対応 OS は Linux と macOS です。

最新リリースを `~/.local/bin` に入れる:

    curl -fsSL https://raw.githubusercontent.com/roamer7038/wikictl/main/install.sh | sh

スクリプトは OS とアーキテクチャに合うバイナリを選び、チェックサムを検証し、`~/.local/bin` が `PATH` に無ければ注意を出します。特定の版を入れるには `WIKICTL_VERSION`、配置先を変えるには `WIKICTL_INSTALL_DIR` を設定します。

Go でソースからビルドする場合:

    go install github.com/roamer7038/wikictl/cmd/wikictl@latest

Linux と macOS（x86_64 と arm64）のバイナリとチェックサムは [releases ページ](https://github.com/roamer7038/wikictl/releases)にあります。

## 最短の使い方

1. Git ホストに空のリポジトリを作り、そこへの `git push` が対話なしで通るようにします（credential helper または SSH agent）。
2. `~/.config/wikictl/config.yaml` を書きます:

       repo: git@github.com:you/wiki.git
       author:
         name: claude-code@laptop
         email: claude-code@laptop.invalid

3. 初期ページを作り、書いて、読みます:

       wikictl init
       printf -- '---\nsummary: --force-with-lease 付きの push は、リモートの ref が期待する sha のままでなければ拒否される\n---\n# --force-with-lease は何を保証するか\n\n本文。\n\n## Links\n- part_of: [index](index.md)\n' \
         | wikictl put global/git-force-with-lease.md
       wikictl search lease
       wikictl get global/git-force-with-lease.md
       wikictl lint

どのコマンドも `--json` を付けると機械可読な出力になります。

## コマンド

| コマンド | 目的 |
|---|---|
| `init` | 空のリポジトリに `README.md` と `global/index.md` を作る |
| `search <word>...` | 全ての語を含むページを探す（`--any` でいずれかの語） |
| `get <path>` | 1 ページの sha、フロントマター、本文、リンク、逆リンクを表示する |
| `ls` | ページを一覧する（`--type`、`--tag`、`--all`） |
| `put <path> < content` | ページを作成または置換する。更新時は `--base <sha>` |
| `mv <path> <newpath>` | ページを移動・改名し、参照元のリンクも書き換える |
| `mv <dir>/ <newdir>/` | ディレクトリ配下の全ページを移動する |
| `rm <path>` | ページを削除する |
| `lint [<path>...]` | 形式違反を報告する |
| `dirs [<dir>...]` | wiki 全体のディレクトリを、ページ数と `index.md` の summary とともに一覧する |
| `context` | 解決済みの設定と検索対象ディレクトリを、ページ数とともに表示する |

`wikictl help <command>` で各コマンドの説明とフラグを表示します。`wikictl version` は版を表示します。

共通フラグはコマンド名の前後どちらにも置けます: `--json`、`--dirs a,b`、`--config <path>`、`--profile <name>`、`--no-fetch`（実行前の fetch を省く）。

### 他人の変更を上書きせずにページを更新する

`get` はページの blob `sha` を表示します。それを `put --base` に渡します。その間にページが変わっていれば `put` は終了コード 3 で終わり、現在の内容と `sha` を出力します。読み直して変更を再適用し、もう一度 `put` してください。既存ページに `--base` 無しで書くのも衝突（終了コード 3）です。マージ状態は一切作られません。

### コマンドが見る場所

既定では最大 4 つのディレクトリを検索します。`global/`、`personal/`、カレントディレクトリの `origin` リモート名から決まる `projects/<name>/`（git 管理外では省かれます）、ホスト名から決まる `machines/<name>/` です。`--dirs a,b` で上書きでき、`wikictl context` で各ディレクトリ配下のページ数（サブディレクトリ内も含む。0 はそのディレクトリにまだページが無いこと。`wikictl dirs` の直下のページ数とは異なります）とともに確認できます。`--dirs .` とすると wiki 全体が対象になります。

各ディレクトリは「その知識がどこで有効か」を表すスコープです。

| ディレクトリ | 有効な範囲 |
|---|---|
| `global/` | 誰にとっても |
| `personal/` | このユーザだけ。マシンやプロジェクトを問わない（コミット規約、用途ごとに使うアカウント、ツールの選択など） |
| `projects/<name>/` | 1 つのプロジェクト |
| `machines/<name>/` | 1 つの実行環境 |

ページは当てはまる中で最も狭いスコープに置きます。このプロジェクトだけ → `projects/<name>/`、この実行環境だけ → `machines/<name>/`、このユーザだけ → `personal/`、それ以外 → `global/`。`personal/` はエージェントが必要になった時に検索して参照する事実を置く場所で、すべての会話に適用すべきルールはエージェントの常設の指示（Claude Code なら `CLAUDE.md`）に置きます。`init` は `personal/` を作りません。`projects/` や `machines/` と同様、最初の `put` で作られます。

`personal/` は 1 人で wiki を使うことを前提にしています。wiki を共有する全員が同じ `personal/` を検索するため、複数人で共有する wiki では `personal/` を使わないか、設定の `dirs` で検索対象のディレクトリを指定してください。

ページをどこに置くか決める前に wiki 全体の構造を見るには `wikictl dirs` を使います。ページを直接含む全ディレクトリを、そのページ数と `index.md` の `summary`（無ければ `(no index)`）とともに一覧します。検索対象ディレクトリの設定は無視され、`wikictl dirs projects` のように引数で `projects/` 配下に絞れます。

## 設定

`~/.config/wikictl/config.yaml`（`$XDG_CONFIG_HOME` も尊重）、または `$WIKICTL_CONFIG` か `--config <path>` で指定したファイル:

| キー | 必須 | 意味 |
|---|---|---|
| `repo` | 必須 | wiki リポジトリの URL。プロファイル側で設定してもよい |
| `branch` | 任意 | 使うブランチ。省略時はリモートの HEAD から決める |
| `author.name`, `author.email` | 任意 | コミットの author。無ければ `git config user.name` と `user.email` |
| `machine` | 任意 | `machines/<name>/` の name。省略時はホスト名の最初の `.` まで |
| `dirs` | 任意 | 既定の 4 つの代わりに使う検索対象ディレクトリの固定リスト |
| `projects` | 任意 | リモート名から `projects/` 配下のディレクトリ名への写像 |
| `profiles` | 任意 | 上記のキーを上書きする名前付きプロファイル。後述 |
| `default_profile` | 任意 | 他の規則でプロファイルが決まらないときに使うプロファイル |

`default_profle` や `match.remote` のような打ち間違いを含む未知のキーは、そのキー名を示す設定エラー（終了コード 2）です。打ち間違いで別の wiki が黙って選ばれることはありません。

### プロファイル

プロファイルを使うと、個人用と業務用のような複数の wiki を 1 つのファイルで扱えます。最上位のキーが既定値で、プロファイルがそれを上書きします:

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

プロファイルは次のうち最初に当てはまるもので決まります:

1. `--profile <name>`
2. `$WIKICTL_PROFILE`
3. `match`。`remotes` はカレントディレクトリの `origin` リモートに対する glob です。リモートはスキーム、ユーザ、ポート、`.git` を除いた小文字の `host/path` の形で比べるので、同じリポジトリの SSH と HTTPS の URL は同じパターンに一致します。途中の `*` はパスの 1 要素に、末尾の `/*` はそれより下のすべてのパスに一致します。たとえば `gitlab.example.com/team/*` は `team/app` とサブグループのリポジトリ `team/sub/app` に一致し、`team` 自体には一致しません。`gitlab.example.com/*/app` は `team/app` に一致し、`team/sub/app` には一致しません。`paths` は絶対パスか `~` で始まるパスで書くディレクトリで、カレントディレクトリがそのディレクトリか配下なら一致します。複数のプロファイルに一致した場合、コマンドは終了コード 2 で失敗します。
4. `default_profile`
5. プロファイル無し。最上位のキーだけを使います。

存在しないプロファイル名はエラー（終了コード 2）です。プロファイル内の `author.name` と `author.email` は個別に上書きし、`dirs` と `projects` は最上位の値を置き換えます。プロファイルが `repo` を設定した場合、`branch` は継承しません。`wikictl context` で選ばれたプロファイル、その選ばれ方、リポジトリを確認できます。

ミラーは `~/.cache/wikictl/`（または `$XDG_CACHE_HOME/wikictl/`）にあります。壊れたら削除してください。次のコマンド実行時に作り直されます。

## 終了コード

| コード | 意味 |
|---|---|
| 0 | 成功 |
| 1 | エラー（ページが無い、など） |
| 2 | 使い方または設定の誤り |
| 3 | 衝突。読んだ後にページが変わった |
| 4 | ページが wiki の形式に合わない（フロントマター不正、パス不正） |
| 5 | git コマンドの失敗 |

`--json` ではエラーは `{"error": "<kind>", "message": "..."}` になります。`<kind>` は `error`、`usage`、`conflict`、`invalid`、`git` のいずれかです。

## ページ形式

ページは、サブディレクトリ（ルート直下は不可）にある Markdown ファイルです。フロントマターには 1 行の `summary` を持たせることを推奨します:

```markdown
---
summary: このページが答えることを 1 文で
type: concept
---
# 題

本文。他のページへは相対パスでリンクします: [index](index.md)。

## Links
- part_of: [index](index.md)
- cites: https://example.com/spec | この原典が裏付けること
```

- ファイル名とディレクトリ名は、空のもの、`.` か `<` で始まるもの、空白、制御文字、``" \ # ? : ( ) ` `` のいずれかを含むものは不可。ページは `.md` で終わる。これに反するパスは `put` と `mv` が拒否する（`bad_path`、終了コード 4）。
- 名前には小文字の ASCII 英字、数字、ハイフンを推奨する。それ以外の名前は `lint` が `name_style` として報告し、同じディレクトリ内で大文字小文字だけが異なる名前（大文字小文字を区別しないファイルシステムで衝突する）は `case_collision` として報告する。
- `summary` は推奨であり必須ではない。無い場合、`put` はページを書き込んだうえで `missing_summary` の警告を出し、`lint` は `missing_summary` を報告し、`search` と `ls` は代わりに題（最初の見出し、無ければファイル名）を表示する。`description` は `summary` の同義として読む。両方ある場合は `summary` が優先。
- 任意のフロントマターキー: `type`、`status`（`deprecated` にすると `search` と `ls` から隠れる）、`tags`、`aliases`、`review_after`。
- `## Links` 節がある場合は最後の見出しであること。各行は `- <type>: <target> | <note>`。`<target>` は相対パスまたは URL。`- <target>` のように type を省いた行は `see_also` として扱う。type を省いた行で URL を書く場合は `<scheme>://...` の形にする。箇条書き記号は `-`、`*`、`+` のいずれでもよく、字下げも許す。
- ページへのリンクは `[text](path)` の形で書く。`mv` が書き換えるのはこの形のリンクだけで、`- part_of: index.md` や `- index.md` のような素のパスは書き換わらず、リンク先を移動すると壊れたリンクになる。
- コードフェンスの中は解釈しない。フェンス内の `## Links` 見出しは節を始めない。

各ディレクトリには、そのディレクトリに何を置くかを `summary` に書いた `index.md` を置き、同じディレクトリの他のページから `- part_of: [index](index.md)` でリンクすることを推奨します。こうすると `wikictl get <dir>/index.md` がそれらのページを `backlinks` として一覧し、`wikictl dirs` がディレクトリの横にその summary を表示するので、生成物を作らなくても wiki の構造がページ自身から読み取れます。

## 開発

    go test ./...
    go build -o wikictl ./cmd/wikictl

## ライセンス

[MIT](LICENSE)
