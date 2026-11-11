# 写真取り込みの用途・保存形式選択

本人のiPhone/PC確認中にホームの写真取り込みが失敗し、同一ブラウザのcapabilities取得がprovider_unavailableと確認された。写真/PDFの上限取得を既存のOCR非依存メタデータへ切り替えた。OCR設定/実行は引き続き完全なカタログを要求する。Tesseractへの認証付きメタデータ取得は成功したが、liveカタログ未発行の根本原因は未確定。

本人の追加指示により、スキャン同様に書類(PDF)/写真(JPEG・PNG)を取り込み前に選べるよう実装。既存UIのpurpose選択、JA/ENラベル、保存形式を再利用し、カメラ・画像取り込みカードへ見出しを追加。ui-ux-pro-maxのラベル/選択状態/モバイル指針とimpeccableの既存画面方向に従う。

用途と形式は送信・不明応答・再読み込み時の同一性に固定し、途中変更しない。省略/書類は旧request digestを維持。写真のJPEG/PNGは別意図として衝突検査し、写真用途の単一ページ/原本・owner・hash束縛を保持。crop→undo後の出力形式も保持する。

常用v0.2.5から今回の修正のみを取り出した候補branchはfix/photo-import-purpose-v025。未公開評価model/sourceはこの候補に含めない。main実装0e04b36、OCR依存修正8defb6f、実API試験7c3baf6、過大画像の試験transport修正b121c07。schema27はpage_photo_source triggerだけを更新し、既存文書データを書き換えない。

**反映時の境界:** liveはまだv0.2.5/schema26。27更新後に26binaryだけへ戻すことはできない。既存writerの停止と整合したstate/documents backupを確認し、必要なら対応DB復元または27対応binaryで戻す。公開/反映はテスト済み候補の承認後に実施する。本人操作の合格・AI採用は主張しない。

画像・ブラウザ/Goログ・ソースレビューはresearch/photo-import-purpose-v027/に保存。最終の実ブラウザ通し検証と反映状態はsummaryで区別する。
