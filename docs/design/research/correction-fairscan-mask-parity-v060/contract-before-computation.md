# Mask値域の固定契約（計算前）

2026-11-11。固定公式source2c5230b12f69f3cee2cc5a970013da20848d1272を読んでから、以下のbranchを固定する。結果を見てthresholdを調整しない。

Android `ImageSegmentationService.outputToArray` はmodel float出力を[0,1]へclampする。**`Segmentation.toMat()` は `threshold=0.5f`、`probmap[i]>=threshold` なら255、それ以外0のCV_8UC1を返す。float Matを期待するという以前の推測は誤り。** `toBinaryMask()` のgrayBitmap表示用pathはこのMask.toMatと別で、混同しない。

一方、公式Pythonはclipped float確率×255→round→uint8をPNG保存し、JVM評価の `MatMask.toMat()` は読み込んだgrayscaleuint8Matをcloneするだけで、0.5 thresholdを適用しない。この両者の入力意味が異なることはsourceから確認できる。

今回の別branchは既存公式PNG6枚をnative OpenCV `threshold(src,dst,127,255,THRESH_BINARY)` で0/255へ二値化する。uint8の値>=128が255になるため、公開0.5閾値をPNG量子化空間へ写した固定契約である。独自検出・正規化・閾値探索は行わない。

これは**0.5 binary契約診断**であり、Androidの未量子化floatからの直接閾値処理と厳密同一とは主張しない。PNG保存前のfloat32丸め境界、AndroidBitmap/TensorImageとOpenCVのRGB/resize差、Android実行そのものは未確認。公式AndroidclassはContext/Bitmap/TFLite support依存で既存JVMにそのまま入っておらず、class移植やstubは作らない。

as-shipped desktop evaluatorの既存の不利な結果も保持する。今回branchのみを有利なモデル結果として取り替えない。モデルを再推論せず、既存6PNGのsource/model/hashから追跡する。
