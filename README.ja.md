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
| `context` | 解決済みの設定と検索対象ディレクトリを表示する |

`wikictl help <command>` で各コマンドの説明とフラグを表示します。`wikictl version` は版を表示します。

共通フラグはコマンド名の前後どちらにも置けます: `--json`、`--dirs a,b`、`--config <path>`、`--no-fetch`（実行前の fetch を省く）。

### 他人の変更を上書きせずにページを更新する

`get` はページの blob `sha` を表示します。それを `put --base` に渡します。その間にページが変わっていれば `put` は終了コード 3 で終わり、現在の内容と `sha` を出力します。読み直して変更を再適用し、もう一度 `put` してください。既存ページに `--base` 無しで書くのも衝突（終了コード 3）です。マージ状態は一切作られません。

### コマンドが見る場所

既定では最大 3 つのディレクトリを検索します。`global/`、カレントディレクトリの `origin` リモート名から決まる `projects/<name>/`（git 管理外では省かれます）、ホスト名から決まる `machines/<name>/` です。`--dirs a,b` で上書きでき、`wikictl context` で確認できます。

## 設定

`~/.config/wikictl/config.yaml`（`$XDG_CONFIG_HOME` も尊重）、または `$WIKICTL_CONFIG` か `--config <path>` で指定したファイル:

| キー | 必須 | 意味 |
|---|---|---|
| `repo` | 必須 | wiki リポジトリの URL |
| `branch` | 任意 | 使うブランチ。省略時はリモートの HEAD から決める |
| `author.name`, `author.email` | 任意 | コミットの author。無ければ `git config user.name` と `user.email` |
| `machine` | 任意 | `machines/<name>/` の name。省略時はホスト名の最初の `.` まで |
| `dirs` | 任意 | 既定の 3 つの代わりに使う検索対象ディレクトリの固定リスト |
| `projects` | 任意 | リモート名から `projects/` 配下のディレクトリ名への写像 |

ミラーは `~/.cache/wikictl/`（または `$XDG_CACHE_HOME/wikictl/`）にあります。壊れたら削除してください。次のコマンド実行時に作り直されます。

## 終了コード

| コード | 意味 |
|---|---|
| 0 | 成功 |
| 1 | エラー（ページが無い、など） |
| 2 | 使い方または設定の誤り |
| 3 | 衝突。読んだ後にページが変わった |
| 4 | ページが wiki の形式に合わない（フロントマターの欠落・不正、summary 無し、パス不正） |
| 5 | git コマンドの失敗 |

`--json` ではエラーは `{"error": "<kind>", "message": "..."}` になります。`<kind>` は `error`、`usage`、`conflict`、`invalid`、`git` のいずれかです。

## ページ形式

ページは、サブディレクトリ（ルート直下は不可）にある Markdown ファイルで、フロントマターに 1 行の `summary` を持ちます:

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
- 任意のフロントマターキー: `type`、`status`（`deprecated` にすると `search` と `ls` から隠れる）、`tags`、`aliases`、`review_after`。
- `## Links` 節がある場合は最後の見出しであること。各行は `- <type>: <target> | <note>`。`<target>` は相対パスまたは URL。
- コードフェンスの中は解釈しない。フェンス内の `## Links` 見出しは節を始めない。

## 開発

    go test ./...
    go build -o wikictl ./cmd/wikictl

## ライセンス

[MIT](LICENSE)
