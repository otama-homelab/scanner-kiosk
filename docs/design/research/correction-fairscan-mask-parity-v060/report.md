# Android／desktop mask契約の違いと固定binary診断

2026-11-11。公式sourceを確認し、**以前の「Androidはfloat Matを検出器へ渡す」という仮説を訂正した。Androidの `Segmentation.toMat()` は確率0.5を閾値としてCV_8UC1の0/255 binary Matを返す。** desktop evaluatorは公式Pythonが保存した確率grayscalePNGをそのままcloneして渡す。両者のmask契約は一致していない。

この違いを結果改善用に調整せず、公式0.5定数に基づく別branchを計算前に固定した。[契約記録](contract-before-computation.md)。元のas-shipped desktopの不利な結果は保存し、今回branchへ置き換えていない。

## sourceから確認した契約

| 段階 | Android公式source | desktop公式source |
|---|---|---|
| 入力 | Bitmap→TensorImage(FLOAT32)、ResizeOp(BILINEAR)、NormalizeOp(127.5,127.5) | OpenCV BGR→RGB、INTER_LINEAR256×256、float32/127.5−1 |
| model出力 | FloatArrayを[0,1]へclamp | float32を[0,1]へclip |
| detector向けMask | `probmap[i]>=0.5f` で255、他0、CV_8UC1 | ×255→round→uint8PNGをIMREAD_UNCHANGEDで読み、MatMaskはcloneのみ |
| 表示用別method | `toBinaryMask()` はgrayBitmap用で、toMatとは別 | PNGはgrayscale確率quantization |

fixedrevisionは `2c5230b12f69f3cee2cc5a970013da20848d1272`。具体的sourceのSHA256とmask値域は [mask bindings](mask-bindings.json)。既存公式PNG6枚は5枚がmin0/max255の多値gray（181〜255種類）、wrappedだけ全0だった。

Android classにはContext/Bitmap/TFLite support依存があり、既存JVMで実classを起動していない。sourceの移植・Android stubは作らなかった。代わりにnative OpenCV `threshold(gray,127,255,THRESH_BINARY)` を使った **公開0.5 binary契約の診断**。uint8の128以上が255になる。これは独自検出処理や閾値探索ではなく、公式定数に基づくnativeAPI invocationである。

**厳密なAndroid parityではない。** Androidは未量子化floatに直接0.5判定するが、今回は保存済みPNGから二値化する。元raw floatが保存されておらず、量子化境界の完全一致は未検証。Bitmap/TensorImageとOpenCVのRGB変換・bilinear resize実装差、Android実行自体も未確認。以前のPillow診断rawを公式前処理rawに見立てて補完していない。

## 実際のquad結果

modelは再推論せず、同じ公式mask6枚のbinary版を既存公式detectorへ渡した。座標serializerは前回と同じ薄い外部adapter。公式quad API、CAPTURE、scaledTo、TL/TR/BR/BL順を維持した。

| control | as-shipped gray quad | 0.5 binary契約quad | binary最大corner誤差/対角長 |
|---|---|---|---:|
| columns | null | あり | 38.685% |
| horizontal | null | あり | 50.928% |
| mixed | あり | あり | 47.155% |
| table | あり | あり | 33.293% |
| vertical | あり | あり | 57.031% |
| wrapped | null | null | — |

quad戻り値は3/6から5/6へ増えたが、検出精度5/6を意味しない。既存2%pilot gateは両branchとも0/6、無補正6/6。新しい合格閾値は追加していない。追加UVDoc3枚は今回再計算しておらず、その既存数値をbinary branchへ移していない。[branch比較](branch-comparison.json)、[既存metric無変更再利用](metrics.json)、[raw座標](synthetic-coordinates.json)。

## 変更quadに対する公式cropとOCR

quadが変わったので、公式DatasetEvaluatorのcrop/warp出力を同じ6枚で生成した。既存の同codec inputJPEG hashは前回baselineと全件一致。候補outputのhashが以前のOCR済み画像と一致した1件は再利用し、新しい5画像だけTesseractを実行した。engine・traineddata・oracle・strictNFC scorerは前回と同一。

| control | 無補正文字誤り数 | binary契約出力の文字誤り数 | paired結果 |
|---|---:|---:|---|
| horizontal | 13 | 13 | 誤り数同じ、文字列は大きく異なる |
| mixed | 14 | 25 | 悪化 |
| table | 20 | 18 | 改善 |
| vertical | 21 | 19 | 改善 |
| columns | 26 | 26 | 同等 |
| wrapped | 8 | 8 | null→公式fallback、同等 |

全6件を分母に改善2・悪化1・誤り数同等3。horizontalは同じCERでもOCRが`b 2`／`A0z`等へ変わり、参照にない数字も現れた。誤り数同等を内容保持とは扱わない。数字を含む2controlはbaseline・candidateとも数字列完全一致0/2。baseline OCR自体も低品質で、CER低下だけから内容安全とは結論しない。

位置を含む文字・細線保持は、既存bilinear map評価器と公式projective warpの不整合が残るため未測定。独自の領域metricやwarpを追加していない。preview-only適格性は引き続きfalse。

## 実行証拠と結論

quad13.50秒／RSS106,360,832 bytes、公式raster16.50秒／RSS107,024,384 bytes、いずれもexit0。CPU2・RSS2GiB監視、既存toolchainを再利用。新OCRは各30秒以内。setup5tree合計3,168,531,613 bytes <3GiB。source272files不変、モデルforward追加0。

[quad run](synthetic-run.json)、[raster run](raster-run.json)、[OCRとhash再利用](ocr.json)、[selfchecks](selfchecks.json)。mask契約の違いは実在し、binary契約でquad数・OCR結果が変わることを実測した。一方でgeometryの大きい誤差、OCR悪化例、量子化とAndroid前処理の未確認が残り、元の不利な結果を無効化したりアプリ本体の品質へ直結させたりできない。

未実施: 未量子化raw→実Androidclassの厳密parity、元実写真内容正解、位置内容保持、本人端末・製品組込。リスク: 契約を揃えると一部の結果は改善するが、未検出と内容の不安定さは残る。
