# FairScan相対改善・内容・信頼性の評価

2026-11-11。既存2%corner閾値はpilot診断の履歴として残し、実用性が確立した境界とは扱わない。今回は同じ正解に対する無補正との相対変化、未検出、実際の公式crop出力のOCR回帰を評価した。後付けの緩い合格閾値は設けていない。

## 幾何の相対変化

既存9件のraw数値を再利用し、モデル推論は再実行していない。平均corner誤差と最大corner誤差の両方を同じ無補正と比較した。

| 固定scope | 全件数 | 平均・最大とも改善 | 平均・最大とも悪化 | 未検出 |
|---|---:|---:|---:|---:|
| 公開合成full-canvas control | 6 | 0 | 3 | 3 |
| 追加UVDoc supplied-grid端点診断 | 3 | 2 | 0 | 1 |

全体は9件中5件にquadがあり、4件はnull。scopeを混ぜた「精度5/9」や、検出例だけを分母にした合格率は作らない。改善と悪化が混在するtradeoff、両指標同等は今回0件。[relative.json](relative.json) に全9件の平均・最大・paired差を保存した。no-op誤差0の合成例で相対改善率の除算は行わない。

追加UVDocの検出2件は平均誤差0.752%／1.649%で、同じGTに対する無補正10.378%／8.444%から改善している。しかし1/3未検出で、元DocUNet9枚との重複0、元6実写真注釈の代わりではない。既存2%pilot判定は合成0/6、無補正6/6のまま保持した。

## 実際の公式cropとOCR

既存source/maskを使用し、無変更の公式 `DatasetEvaluatorKt` を実行。公式のquad検出、`autoColorMode`、`extractDocument` / OpenCV `warpPerspective` でcandidate画像を生成した。null3件は公式mainの `inputMat.clone()` fallbackであり、adapterがfullpageを補ったものではない。

公式raster処理は26.90秒、exit0、peak process-group RSS108,580,864 bytes。6枚のinputJPEGとoutputJPEGを生成。両側を同じJPEG codecに揃えてTesseract5.5.4 jpn+eng psm3、threads2と既存 `AnalyzeOCRPage` のstrictNFC文字距離を無変更で使用した。OCR12件の合計6.22秒、peak RSS204,599,296 bytes、全件exit0。各process30秒・CPU affinity2・RSS2GiB監視。文字参照は以前に固定したrenderer oracleをそのまま使用し、whitespace削除や採点調整はしていない。

| control | 公式quad | 無補正JPEGのCER | 公式出力のCER | 文字誤り差 |
|---|---|---:|---:|---:|
| horizontal | null→公式fallback | 1.000 | 1.000 | 0 |
| mixed | あり | 0.467 | 0.800 | +10 |
| table | あり | 1.818 | 1.909 | +1 |
| vertical | あり | 2.100 | 2.100 | 0 |
| columns | null→公式fallback | 1.300 | 1.300 | 0 |
| wrapped | null→公式fallback | 1.600 | 1.600 | 0 |

全6件を分母に、文字誤り改善0・悪化2・誤り数同等4。検出3件に限ると悪化2・同等1で、null fallback3件の同等を補正成功に数えない。verticalは誤り数が同じでもOCR文字列自体は異なり、内容保持の証明にはならない。

数字を含む2件はbaselineもcandidateも数字列完全一致0/2。既にbaseline OCRが読めていないため、どちらも不一致であることから数字保持を保証できない。strictNFCではwhitespace/挿入も数えるのでCERは1を超えうる。このpilotの低いOCR baselineも保持し、製品の実用性合格値には使わない。

[OCR rawとengine/oracle pins](ocr.json)、[paired結果](ocr-paired.json)、[公式raster command/result](raster-run.json)、[log](raster.log)、[source/output hash selfchecks](selfchecks.json) を保存した。

## 内容保持と適格性

既存 `internal/evaluation/correction.go` の `AnalyzeCorrectionCatalogs` はforeground/region保持を測れるが、`internal/correction/transform.go` の `MapSourcePoint` はinverse-bilinear crop写像である。今回の公式processorはprojective `warpPerspective`。両者を同一の変換だと偽ってregion scoreへ渡すことはできない。**文字領域・細線・写真・印影の位置を含む内容保持は未測定**とし、新しいcoverage metricやhomographyを実装していない。

既存grid端点GTもページ内部の文字・数字・細線内容を保証しない。幾何が改善した追加2例だけで内容安全性を推定しない。元画像保持、undo、候補bound共通経路回帰、本人端末操作の受入も今回の実験では成立しないため、`preview_only_qualified:false`。自動applyへの適格性も主張しない。

今回の解釈は「2%を超えたから即不採用」ではなく、**追加幾何で相対改善する例はあるが、未検出と合成の内容/OCR回帰があり、安全な候補としての証拠が不足**というもの。既存desktop evaluatorのuint8 mask値域とAndroid本体のparity未確認も引き継ぐ。

次回の未見データ評価に向けて、paired改善・悪化・missingness・内容保持・本人確認を先に固定する [prospective protocol](prospective-protocol.md) を保存した。既存結果を読んだ後に作成したため、今回幾何結果の事前登録とは呼ばない。今回追加OCRについては実行前に手順を固定した。

未実施: perspective対応の位置内容保持、元6実写真/photo2_2内容正解、Android parity、本人端末・製品組込。リスク: 旧pilot閾値を緩めても、今回観測した内容回帰と未検出は解消しない。
