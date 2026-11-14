# Scanner Pro：公式公開の処理方式

確認日2026-11-10。Readdle公式説明を参照。実機比較、内部実装の検査、checkpoint取得はしていない。

- [境界検出の技術記事](https://readdle.com/blog/scanner-pro-border-detection)：MobileNet-based keypoints → capture-time segmentation refinement; IMU/Kalman tracking. Article describes experimental rollout, not independent performance verification.
- [従来の影・色補正](https://readdle.com/blog/scanner-pro-shadow)：illumination-background estimation, Metal, coarse-to-fine processing; historical implementation, not proof of the2026color-mode architecture.
- [2026年v8.34の更新](https://readdle.com/blog/why-scanner-pro-scans-look-better-than-ever)：neural color enhancement with global/local/patch stages, on-device ANE. Specific architecture, model weights and OSS equivalence are not identified in this article.

Scanner-kioskへの示唆：検出・幾何・照明を分けて既成実装を選定する。この記事を理由に自作補正／学習を追加しない。MobileNetという分類だけでDocAlignerやFairScanと同一モデルとは扱わず、紹介された市販appの品質と今回のOSSモデル測定を混同しない。Scanner Pro自体の導入／登録／購入／原稿送信は行っていない。


## 本人指定の参照先としての設計方針 — 2026-11-14

本人指示「scanner proを参考に」を受け、公式技術記事と操作ガイドを再確認した。以下は次の補正機能の設計方針であり、本番v0.2.27で実装・採用済みという意味ではない。既存のPaperless系作業画面は維持し、取り込み後の補正体験を参考にする。

### 公式の操作資料から確認したこと

| 参照 | 確認した機能 | この製品への適用方針 |
| --- | --- | --- |
| [Create a scan](https://support.readdle.com/scannerpro/view-manage-the-scans/create-a-scan) | 撮影時の自動文書検出・透視補正・切り取り。提案された四隅の手修正。既存写真の取り込みにも対応 | 文書として取り込む写真は、検出・傾き／台形補正の案を自動で作り、その結果から確認を始める。撮影と既存写真の取り込みを同じ補正経路へつなぐ |
| [Crop and rotate](https://support.readdle.com/scannerpro/enhance-and-edit-your-scans/how-to-crop-scans) | 自動処理後も四隅と回転を手で直せる | 必要なときだけ画像上の四隅を調整する。既存の矩形cropを台形補正済みと表示しない |
| [Existing-photo enhancement](https://readdle.com/scannerpro/how-to-improve-quality) | ギャラリーの文書写真を取り込み、境界・フィルター・明るさ等を調整する操作 | 写真入力からスキャン風の文書を作る流れを優先する。原本へ戻す経路を残す |
| [Color modes](https://readdle.com/scannerpro/how-to-change-color-mode) | Auto Color／Color／Black & White／Photo／Grayscaleとプレビュー | 仕上がりの候補は自動・カラー文書・白黒・グレー・写真の意味で整理する。実際に対応した既成処理だけを選択可能にする。自動色判定のPlus条件をこの製品の機能・利用条件へ転記しない |
| [2026 v8.34 enhancement](https://readdle.com/blog/why-scanner-pro-scans-look-better-than-ever) | 色・影補正のニューラル化。global/local/patchの段階処理と端末内実行を説明 | スキャン風変換は単なる白黒化ではなく、照明・影・背景と文字／色の保持を含む。記事を独自モデル再実装の仕様にはしない |

### 目標とする通常の操作

文書用途の選択 → 撮影または写真取り込み → 自動の文書切り出し・傾き／台形補正案 → スキャン風の仕上がり確認 → 必要なら四隅／回転／仕上がりを手修正 → OCR → 保存。

- 自動補正は、適格な既成処理を接続できた段階で文書写真の通常経路にする。自動案の作成と、確認した画素をOCR・保存へ使う決定は分ける。無条件で原本を上書きしない。
- 入力手段の「写真」と原稿用途の「写真・作品」を混同しない。文書／写真用途は既存の取り込み前選択とimmutable intentを維持し、保存済み文書の用途を無断変更しない。写真・作品は紙の白色化を標準にしない。
- スキャナー由来の画像も同じ補正確認の経路を利用できる設計を維持する。検査だけで十分なきれいな原稿まで重いモデルを必ず実行する構成にはしない。ただしスキップ判定自体も独自アルゴリズムとして新設しない。
- 本の湾曲補正と平面文書の傾き／透視補正は別機能として扱う。まず平面文書の経路を完成させる。
- 通常画面では処理モデルや多数のスイッチを選ばせず、補正結果と必要な手直しを中心にする。現行のOCRインライン設定・一つの文字スクロール・安定した保存位置は保つ。
- 未検出や不適格な結果は、未補正であることを明示して既存の手動編集／原本保存へ戻す。失敗を補正成功として数えない。

### 実装境界と現在地

Scanner Proの記事には一部の方式が公開されているが、色・影モデルの具体的な構造や再利用できる重みは確認できていない。境界検出記事も実験的配布段階の説明を含むため、記事の方式を全端末の現行実装と断定しない。Scanner Proと同じ実装・性能・端末内実行を保証しない。

既存の完成したSDK／ライブラリ／公式実行経路を利用する。独自の検出、mask→四隅、傾き推定、影除去、学習、画像や文字の生成補完は作らない。モデル名がMobileNet等で似ているだけでは同等性を主張しない。

既存評価の正本は [correction-validation-v060/summary.json](research/correction-validation-v060/summary.json)。FairScanやFSENet等の既存不合格を今回の参照指示で合格へ変えず、同じ条件の評価を再実行しない。選定には、未検出率、対原本の有用性、数字・細線・印影・色の保持、実行環境／利用条件を確認する。原本と同じ内容の写真入力・スキャン入力で比較し、自動案・手修正・原本へ戻す・OCR・保存を通して受け入れる。2%だけを有用性の唯一の根拠にしない。

今回の作業は公式資料の再確認と設計方針の記録のみ。アプリ本体やモデルの変更、推論、私文書の取得・送信、スキャン、購入・登録・配備は行っていない。公式記述とこの製品の適用方針は表で分離した。ローカルUXデータ検索は再検索でも本件に適した一致を確認できなかったため、その結果を根拠として採用せず、公式操作資料と本人指定の作業順序から方針を整理した。
