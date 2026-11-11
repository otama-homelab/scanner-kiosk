# 写真取り込みの用途・保存形式選択

本人のiPhone/PC確認中にホームの写真取り込みが失敗し、同一ブラウザのcapabilities取得がprovider_unavailableと確認された。写真/PDFの上限取得を既存のOCR非依存メタデータへ切り替えた。OCR設定/実行は引き続き完全なカタログを要求する。当初未確定だったカタログ未発行の原因は、LLMプロファイルの認証キー参照名が許可prefixを満たさなかったこと。同一の既存Secretへworker限定の参照名を追加して修正した。

本人の追加指示により、スキャン同様に書類(PDF)/写真(JPEG・PNG)を取り込み前に選べるよう実装。既存UIのpurpose選択、JA/ENラベル、保存形式を再利用し、カメラ・画像取り込みカードへ見出しを追加。ui-ux-pro-maxのラベル/選択状態/モバイル指針とimpeccableの既存画面方向に従う。

用途と形式は送信・不明応答・再読み込み時の同一性に固定し、途中変更しない。省略/書類は旧request digestを維持。写真のJPEG/PNGは別意図として衝突検査し、写真用途の単一ページ/原本・owner・hash束縛を保持。crop→undo後の出力形式も保持する。

常用v0.2.5から今回の修正のみを取り出した候補branchはfix/photo-import-purpose-v025。未公開評価model/sourceはこの候補に含めない。main実装0e04b36、OCR依存修正8defb6f、実API試験7c3baf6、過大画像の試験transport修正b121c07。schema27はpage_photo_source triggerだけを更新し、既存文書データを書き換えない。

**反映状態:** v0.2.10/schema27を本番へ反映済み。正常停止を確認した旧writerの両保存領域を整合バックアップしてから更新した。26binaryだけへの切戻しはできず、復元または27対応binaryが必要。本人端末での操作確認と新しい補正モデルの採用は、この反映の合格に含めない。

画像・ブラウザ/Goログ・ソースレビューはresearch/photo-import-purpose-v027/に保存。最終の実ブラウザ通し検証と反映状態はsummaryで区別する。


## 本番反映の検証

2026-11-12 JSTにv0.2.10（source6abe5c5745df39bd3e7e9399a12615bae6c62d21）を公開・反映した。公式Go1.27.4へ更新してGO-2026-6859を解消し、native assembly・実コンテナOCR・race/PDF/embed・govulncheckを含む全必須CIが成功。48配布物のサイズとserver SHA256、匿名manifest取得、両OCIのversion/latest・config同一性を確認した。

旧writer停止後の同一PVC内バックアップを経て、schema26→27へ更新。API・worker・TesseractはReady、再起動0回。新しい用途選択用コードを配信し、Tesseract/LLMカタログの発行も復旧した。認証キー参照の修正は同一OpenBao源のworker限定aliasで、新しい送信先・credential grantは追加していない。

本人のiPhone/PC操作とLLM実推論は未確認。補正検証milestoneは7/14を維持し、新しい補正モデルの適格性を認定しない。公開可能な反映証拠は[production-v0210.json](research/photo-import-purpose-v027/production-v0210.json)に保存している。
