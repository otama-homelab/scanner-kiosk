# 配布物の構成

[README](../README.md) · [導入ガイド](guide/getting-started.md)

[GitHub Releases](https://github.com/otama-homelab/scanner-kiosk/releases/latest)の同じversionに属するファイル・imageを組み合わせてください。

| 配布物 | 用途 |
| --- | --- |
| `scanner-deployment.tar` | Compose・Kubernetes例、環境変数例、README、利用ガイド、画面例、ライセンスをまとめた導入用アーカイブ。 |
| Core image | frontend、Go API／worker、SANE／airscan。`serve`と`worker`で同じimageを使用します。 |
| Tesseract image | 任意のOCR API。Tesseractと日本語／英語辞書を別コンテナで動かします。 |
| `SHA256SUMS`・`manifest.json` | 配布ファイルの同一性、version・source・image設定などの対応を確認する資料。 |
| 対応するsource・notices・SBOM・security reports | ライセンス確認、再build、依存・脆弱性指摘の確認に使用します。 |
| 単体実行ファイル | Docker以外の個別構成向け。[対応範囲](standalone.md)を先に確認してください。 |

## image

- Core: `ghcr.io/otama-homelab/scanner-kiosk`
- Tesseract API: `ghcr.io/otama-homelab/scanner-kiosk-tesseract`

正式なversionタグは`vMAJOR.MINOR.PATCH`です。`latest`は安定版の別名で、固定した運用ではReleaseのrepository digestを使います。`preview`と`sha-<commit>`は開発用です。

## アーカイブ内の導線

```text
README.md                 機能と導入への入口
LICENSE                   プロジェクトのライセンス
THIRD-PARTY-NOTICES.md     第三者のライセンス情報
.env.example              設定項目一覧
deploy/                   配備用設定
docs/guide/               導入・使い方
docs/screenshots/         READMEとguideの画面例
docs/                     配布と開発の案内
```

利用者向けアーカイブは配布元sourceから作り、そのファイル一覧とhashを既存のdeployment inventoryへ束縛します。設計研究の大量の評価記録は、導入ガイドの代わりに同梱するものではありません。対応するsource配布は別途保持します。

新しいドキュメントや同梱範囲の変更は、次にそのsourceから作成するReleaseへ反映されます。既に公開したimmutableな配布物を後から書き換える運用ではありません。
