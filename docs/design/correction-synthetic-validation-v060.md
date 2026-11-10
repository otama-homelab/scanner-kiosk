# 生成画像による補正検証 — v0.6.0追補

2026-11-10、本人指示「お任せ、生成画像でもよい」により実施。既存生成器・既知の文字／座標／罫線を使う自動検証経路を完了した。実写真の未確認事項と重み条件は残す。新モデルの製品採用・配備は行わない。

## 入力と正解

既存`scripts/eval/ocr/prepare_catalog.py`を無変更で使用し、Pillow12.2.0と固定IPA Gothicフォントで横書き・混在英数字・表・縦書き・段組・一文字改行の6枚を再生成した。すべて既存oracleと画像hashが一致。rendererの文字列・文字box・900×700page境界・表の2px罫線が正解であり、OCR/LLM/補正モデルから正解を作っていない。生成モデルの任意の文字・画素を正解とする方式は使わない。写真・影の自前生成や独自補正／検出アルゴリズムも追加しない。

画像とoracleとrendererhashを新推論前に固定。明示的なpage四隅／罫線metadataは実行後に固定済みrendererの定義から記録したもので、出力を見て調整したラベルではない。full-page canvasの既知境界は、背景に置いた紙の実撮影輪郭とは別の正解である。DocUNet既存9枚・photo2_2とUVDoc独立GT3枚は維持し、今回の結果で置換しない。

## 実行と結果

既存scanner-eval/candidateを再使用。6枚×4経路×cold1/warm2、計72process完了。各UVDocprocessはさらに同sessionで3forwardを行う。CPU2threads／30s／RSS1GiB／temp256MiB、追加導入256MiBの製品基準は維持。資源計測中にbuild/test/OCRを重ねず、OCRと再読込は後で実行した。UVDoc新mesh18組のsource/output/map/model/prepost束縛と公式samplingによる厳密再読込も全件成功した。

| 経路 | process完了 | peak RSS | clean画素MAE基準合格 | 入出力寸法一致 | fallback |
|---|---:|---:|---:|---:|---:|
| 補正なし | 18/18 | 22,777,856B | 6/6 | 6/6 | 0/6 |
| DocAligner既存完成経路 | 18/18 | 254,062,592B | 5/6 | 5/6 | 5/6 |
| UVDoc旧ONNX | 18/18 | 232,693,760B | 0/6 | 6/6 | 0/6 |
| UVDoc新公式mesh | 18/18 | 514,940,928B | 0/6 | 6/6 | 0/6 |

clean基準はchannel MAE<=1/255。UVDoc新旧とも全6件で超過。DocAlignerは5枚を無変更fallbackにし、表だけを切り抜いて寸法を変えた。表の元座標での暗い罫線画素一致は補正なし100%、UVDoc新旧12.9614%。**これは固定座標での一致率であり、幾何移動後の罫線総残存率や87%消失を意味しない。** DocAlignerは寸法変更により同座標比較対象外とした。

## OCRの扱い

既存Tesseract5.5.4、jpn+eng、PSM3、2threadsを全24caseで固定。既存`evaluation.AnalyzeOCRPage`のNFC Unicode-code-point distanceを再使用した。空のpositioned spansを渡しており、文字誤り数だけを使用し、領域／cell／読順の合格を主張しない。改行・空白も保持する厳格比較で、挿入が多いとCERは1を超える。完全一致と1文字置換のself-checkに成功した。

数字を持つのは混在・表の2枚。補正なしを含む全4経路で数字列完全一致は0/2だった。したがって補正だけが数字欠落を起こしたと断定できない。空の数字列4枚を数字合格に加算しない。平均CERは補正なし1.3808、DocAligner1.3808、旧ONNX1.2194、新mesh1.2861だが、空出力でもCERが下がる例を含むため、この平均だけで改善・採用を判定しない。現OCR設定の誤りを隠すためのPSM変更や出力整形はしていない。

既存onefile runtimeは`bundled runtime unavailable or changed`で起動しなかったため、wrapperを修理・緩和せず、同じ既存cacheの展開済みnative Tesseractをそのmusl loaderで実行した。binary SHA256は`5cc985582023a51d1ecd15bba88bf5b531607f60a4a68c4bf2b43344fcea3b66`。過去の別binaryによるOCR測定とは別の証拠として扱う。新しいbuild・依存導入・製品runtime変更はない。

## 採否と残件

今回の許可によりTRUTH-01の画像独立な正解固定は、renderer-known6画像の範囲で成立した。既存6実写真に人手ラベルを付けたことにはしない。元の実写真条件と今回の代替経路を併記する。

clean入力へAI補正を常時適用する案は今回の測定でも不採用。写真／Canon共通の処理経路と、clean scanでは無推論・無再samplingという既定を維持する。新meshの資源実行自体は成功するが、先行検証で展開依存容量約1.05GiBが256MiB基準に不合格であることも変わらない。

photo2_2の独立した数字・細線／回帰原因、6組の整列済み影正解と3clean controlの全属性coverage、照明checkpointの明示的条件、実iPhone／PCの本人操作は未完。生成画像の許可を、これらの証拠や権利者回答・本人受入へ読み替えない。milestone全体は未完のまま、今回の生成入力による自動検証を完了として記録する。公開・配備・外部問い合わせ送信は行っていない。

[機械可読結果とhash一覧](research/correction-synthetic-v060/summary.json)。raw実行結果は`/tmp/scanner-v060-synthetic/`、生成画像は既存生成器で再現可能。本人Canon画像は今回の処理へ投入せず、外部送信もしていない。

追加の取得確認では、SyntheticDoc datasetの公式DOIに対応するDataCite metadataがCC BY 4.0を明記していた。これはdatasetの条件確認であり、モデルcheckpointの条件へは転用しない。公式landing／APIは双方HTTP429だったため迂回や連続再試行をせず、影ペア未取得として記録した。
