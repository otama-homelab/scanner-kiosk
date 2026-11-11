# 次回評価の事前プロトコル

2026-11-11。既存幾何結果を読んだ後に作成した探索的計画であり、既存結果の事前登録とは主張しない。以下は今後の独立holdout評価用。今回追加する合成OCR診断には、実行前に同じ比較手順を固定する。

- 既存2% corner cutoffはpilot診断の履歴として保持し、実用性が実証された境界とは扱わない。今回これを緩和した新しい合格閾値を作らない。
- 無補正と同じsource・同じ独立GT・同じOCR engine/referenceをpaired比較する。幾何は既存の平均と最大corner誤差の両方、符号付きpaired差、改善/悪化/混在/同等/未検出を報告する。no-op誤差0の場合に比率改善は計算しない。
- synthetic6と追加grid-endpoint3は別集計。未検出や実行失敗は分母から除外せず、nullの誤差0補完やfullpage代入をしない。検出した症例だけの平均を全体性能としない。
- 内容はまず元GTの数字・細線・文字領域を確保する。全体CERと正確な数字列、位置を含む内容保持、写真/印影/紙色を分ける。今回の合成文字OCRは予備診断で、位置mapが不整合ならregion scoreを作らない。
- 新しい公式crop rasterは公式のinputJPEG/outputJPEGのpairedOCRで比較し、codec差を揃える。Tesseract5.5.4 jpn+eng psm3/threads2と既存AnalyzeOCRPageのstrictNFC距離を無変更利用。空OCR・nullfallbackも全件記録し、whitespace除去等の結果調整をしない。
- 最終の内容損失許容度・実行失敗許容度・端末性能の実用gateは、元実写真の独立正解と本人確認で先に決め、未見holdoutで評価する。現在の9例へ閾値を合わせない。
- preview-only適格性も、内容/権利/元画像保持/undo/本人操作/候補bound回帰が揃うまでfalse。相対改善した2/3例だけでpreview適格と判定しない。
