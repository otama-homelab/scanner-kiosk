# 旧DocShadow出力のOCR保持評価

2026-11-11。新たなモデル推論は行わず、保持済みnative/512出力と元の公開合成clean/shadowへ既存Tesseract5.5.4 jpn+eng PSM1を適用した。6case×clean/shadow×no-op/native/512＝36OCRが全件成功。各0.650–0.965秒、各入力30秒上限、2thread/2core。Tesseract executableと日本語/英語辞書、既存parse/scorer sourceをhash固定した。

mixed/tableと同じrenderer文字・座標を保持するfaint variantsの4caseだけ既存oracleを使用した。blue/peachは画像生成pixelから文字正解を推測せずrawOCRのみ12件。既存parse_tsvとevaluation.AnalyzeOCRPageで24件を採点し、独自CER・編集距離・位置metricを追加していない。

## 結果

| 入力 | no-op文字誤り | native | 512 |
|---|---:|---:|---:|
| mixed_clean | 12 | 12 | 11 |
| mixed_shadow | 12 | 12 | 13 |
| table_clean | 10 | 10 | 2 |
| table_shadow | 10 | 10 | 6 |
| faint_mixed_clean | 12 | 13 | 12 |
| faint_mixed_shadow | 12 | 13 | 12 |
| faint_table_clean | 10 | 10 | 11 |
| faint_table_shadow | 2 | 8 | 11 |

nativeはno-op比で改善0・同じ5・悪化3。薄文字mixedのclean/shadowは12→13誤り、薄文字table影は2→8誤りに増えた。512は改善3・同じ2・悪化3で一貫した保持を示さない。baseline自体も厳密page合格ではなく、24採点全件がstrict quality failed。Tesseractの固定PSM・合成入力に限定した結果であり、実写真や全OCR engineの一般性能ではない。

## pixel変換・検証の限界

公式DocShadow infer.pyはfloatのcv2.resize/cvtColor表示だけでPNG保存手順を持たない。OCR入力には、その表示値を255倍しOpenCVの標準PNG encodeによるsaturating8bit変換を使用した。これは公式のPNG exporterを実行したという主張ではない。raw floatの単位区間外channel数を全出力についてruns.jsonへ保存し、clip/量子化の影響を隠さない。nativeにはresizeなし、512には公式表示同様の原寸へのcv2.resizeを適用した。画素補正や新アルゴリズムは追加していない。

初回scratch setupはoracle JSON key想定違いでOCR開始前に失敗し、実際のentriesへ修正。最初のGo scorer実行も既存GOROOT1.25.7混入でcompile失敗し、既存Go1.27.3のGOROOTを明示して解消した。どちらも履歴を保存し、OCR結果の再試行はない。既存scorerへ正解text/位置をそのまま入力するself-checkはpassed。

SSIMは前回4評価venvの不在確認を再利用し、追加のsystem Pythonにもskimage/torchmetrics/cv2なしを確認。新規dependencyや自作SSIMは導入していない。OCRの診断は既知の画質/資源不合格を解除せず、旧DocShadow構成はapply不可のまま。

## 残る評価経路

保持出力だけで進めるSSIMには既存にない標準実装が必要。旧DocShadow Jung/Kliglerは別checkpointの追加評価計画が必要で、現FSENetとは区別する。現FSENetは公式CUDA固定loaderのCPU対応、FairScanは既存計画外のJVM build/依存解決、SyntheticDocはcheckpoint identity/terms/安全なCPU load、元photo2_2は独立注釈、本人受入は実端末操作がそれぞれ必要。単に同じコマンドを繰り返してもこれらは解消しない。

未実施: SSIM・実写真・現FSENet・checkpoint parity・本人端末確認・統合/配備。リスク: 公開合成文字の部分的な改善を安全な自動補正や元写真の合格へ読み替えないこと。
