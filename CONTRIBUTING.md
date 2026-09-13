# 開発の進め方

wikictl のブランチ、プルリクエスト、リリースの運用ルールです。ビルドとテストの方法は README の「開発」節を参照してください。

## 全体の流れ

```
Issue ──> ブランチ（main から） ──> PR（base: main） ──> squash マージ ──> タグでリリース
```

- main は常にリリースできる状態に保ちます。
- 長期間のブランチや統合用のブランチ（`release/*` など）は作りません。

## Issue

- 変更は Issue から始めます。不具合は再現手順を、提案は目的を書きます。
- 外部仕様（コマンドの出力、JSON のキー、終了コード、受け付ける入力）を変える Issue には `spec-change` ラベルを付けます。本文には、変更する仕様、変更する理由、互換性への影響、代替案を書きます。
- `spec-change` の Issue は、方針に合意してから実装します。

## ブランチ

- 名前は `<type>/<Issue 番号>-<短い名前>` にします（例: `fix/24-push-retry`）。`<type>` はコミットメッセージの type と同じです。
- main から作り、1 つの PR ごとに 1 つのブランチを使います。

## プルリクエスト

- base は常に main です。
- 1 つの PR には 1 つの目的だけを入れます。本文に `Closes #<Issue 番号>` を書きます。
- 仕様変更を伴う PR には、README とヘルプ文の更新を含めます。
- main と衝突したら、PR のブランチを main に rebase して解消します。解消した内容も PR でレビューします。

## コミットメッセージと PR タイトル

PR は squash でマージするので、PR のタイトルがそのまま main のコミットメッセージになり、リリースノートの分類にも使われます。

- [Conventional Commits](https://www.conventionalcommits.org/ja/v1.0.0/) の形式にし、説明は日本語で書きます（例: `fix: push の ref ロック失敗を再試行する`）。
- type は `feat`、`fix`、`docs`、`refactor`、`test`、`ci`、`chore` のいずれかです。
- 仕様変更を伴う場合は、type の後に `!` を付けます（例: `fix!: rm でページ以外のパスを拒否する`）。

## レビューとマージ

main はルールセットで保護されており、次を満たさないとマージできません。

- PR を経由すること
- CI の `test`（`gofmt -l`、`go vet`、`go test`）が成功していること
- レビューのスレッドがすべて解決していること
- マージ方法が squash であること（main の履歴は直線に保たれます）

マージ後、ブランチは自動で削除されます。

## バージョン

[Semantic Versioning](https://semver.org/lang/ja/) に従います。0.x の間は次のように上げます。

| PR の内容 | 上げる番号 |
|---|---|
| 仕様変更（`!`）または機能追加（`feat`）を含む | minor（0.2.0 → 0.3.0） |
| 修正（`fix`）などだけ | patch（0.2.0 → 0.2.1） |

## リリース

1. main の最新コミットで CI が成功していることを確認します。
2. そのコミットに annotated tag を付けて push します。

   ```sh
   git tag -a v0.3.0 -m "wikictl v0.3.0"
   git push origin v0.3.0
   ```

3. タグの push で GitHub Actions の release ワークフローが動き、GoReleaser がバイナリと `checksums.txt` を GitHub Release に公開します。
4. リリースノートに、互換性に影響する変更と移行方法を追記します。

`v` で始まるタグはルールセットで保護されており、削除や付け替えはできません。誤ったリリースは新しいバージョンで修正します。

## セキュリティ

未公開の脆弱性は、公開 Issue ではなく、GitHub の [Private vulnerability reporting](https://github.com/roamer7038/wikictl/security/advisories/new) で報告してください。修正したら patch リリースを出します。
