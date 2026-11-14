# 開発とソース構成

[README](../README.md) · [ドキュメント一覧](README.md)

GoのbackendとAstroのfrontendを使います。frontendは静的アセットとしてGo実行ファイルへ埋め込みます。Goのバージョンは`go.mod`、Nodeとfrontend依存はbuild設定と`web/package-lock.json`に固定しています。

```text
cmd/                 実行ファイルの入口とroleの構成
internal/            API・worker・文書管理・OCR・PDFなどの実装
pkg/ocrplugin/       OCR providerの公開契約
web/                 Astro画面・TypeScript・スタイル
api/                 HTTP API契約
deploy/              利用者向けのCompose・Kubernetes配備例
docs/guide/          導入と操作手順
docs/screenshots/    架空データで撮影した実画面
docs/design/         設計・実装・評価記録
docs/adr/            設計判断
native/              ネイティブ依存の固定入力・再build資料
scripts/             build・検証・配布素材作成
tests/               横断的なテスト資産
```

テストは原則として対象コードのそばに置きます。個人用設定・認証情報・ローカル進捗は配布ソースへ含めません。

## 責任の境界

| 場所 | 主な責任 |
| --- | --- |
| `cmd/scanner` | CLI・シグナル・roleごとの設定 |
| `internal/apiruntime` | HTTP・認証session・API起動 |
| `internal/workerruntime` | 起動整合・ジョブ実行・終了処理 |
| `internal/documents` | 所有者・revision・保持期間・ジョブの永続状態 |
| `internal/scanner` / `scannerdriver` | 機器の検証・読み取り・所有する試行の回収 |
| `internal/ocr` / `pkg/ocrplugin` | OCR実装とprovider契約 |
| `internal/pdftextlayer` / `pdfassemble` | 文字座標・PDF合成 |
| `internal/staterecovery` | identityのbackup・restore。文書とsessionは対象外 |

## buildと検証

```sh
go test ./pkg/ocrplugin ./internal/ocr/llm ./internal/pdftextlayer
python3 -m unittest discover -s scripts -p 'test_prepare_candidate.py'
python3 scripts/build-web.py --output /tmp/scanner
```

全体検証・race・native PDF runtime・browser検証には、[CIで固定した依存と手順](https://github.com/otama-homelab/scanner-kiosk/blob/main/.github/workflows/checks.yml)を使用してください。対象packageだけの成功を全機能・実機の受入に読み替えないでください。

mainのimageは開発用previewです。正式な安定版は、検証・source/license/SBOM・承認を束縛した候補から配布します。[配布物の構成](distribution.md)を参照してください。
