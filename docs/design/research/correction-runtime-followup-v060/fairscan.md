# FairScan公式実行経路の追補確認

2026-11-11。公式mainのrevisionは `2c5230b12f69f3cee2cc5a970013da20848d1272`。今回のHTTP再取得でも前回と同じrevisionである。10個の公式sourceのサイズ・SHA256、最新releaseのasset identityは [取得記録](fairscan-sources.json) に保存した。

## 判定

**FairScanにはAndroid以外で使う公式評価ソースがある。** 「公式のLinux完成バイナリがない」だけで、評価経路自体がないと判断するのは狭すぎる。ただし、現在の「新規ビルドをしない」境界内で、完成した四隅検出経路をそのまま実行できることまでは確認できていない。元実写真の独立注釈不足も別に残る。

| 公式経路 | sourceで確認した内容 | 今回の扱い |
|---|---|---|
| Python segmentation | `evaluation/python/generate_masks.py` はTensorFlow Lite Interpreterで画像からmaskを生成。`requirements.txt` はtensorflow/numpy/opencv-python。READMEにvenv・pip install手順あり。 | 解釈実行できる公式scriptで、事前コンパイル済みdesktop binaryは不要。ただし出力はmaskまで。既存mask測定を完成四隅検出の証明へ昇格できない。 |
| JVM quad evaluator | `QuadDetectionEvaluator.kt` に `main()`、`OpenCV.loadLocally()`、公式 `detectDocumentQuad(..., Mode.CAPTURE)` がある。`evaluation` と `imageprocessing` はAndroid pluginではなく `java-library` / Kotlin JVM、target JVM11。 | Android端末やemulatorが必須のコードではない。Kotlin compile、runtime classpath、OpenCV native loadが必要。公式READMEはPython環境までで、完成済みJARや起動commandを配布していない。 |
| Android app | READMEはAndroid8.0以上・camera、GitHub/Google Play/F-Droid配布を案内。実験的intent `org.fairscan.app.action.SCAN_TO_PDF` はscan開始後PDF URIを返す。 | 公式APKを互換Android実機で使えば移植は不要。Android実機の存在は確認していない。intentは既存JPEGのheadless corpus評価APIではない。 |

## 配布・依存の実際

最新GitHub releaseは `v2.3.0`。assetsは `FairScan-2.3.0-arm64-v8a.apk` と `FairScan-2.3.0-armeabi-v7a.apk` の2件。最新releaseにdesktop evaluator/JARはない。この観測は全配布元・全履歴にbinaryが絶対ないという証明ではない。

JVM評価moduleは `org.openpnp:opencv:4.9.0-0` を参照する。root settingsはapp/imageprocessing/evaluation全3moduleを含むので、Android SDKなしで任意のGradle taskを設定できるかは未検証。JVM11はsource/bytecode targetであり、現行Android Gradle Plugin9.5.0やGradle実行用JDKの必要版を11と断定しない。

公式 `evaluation/README.md` の準備手順はPythonと `dataset/images` のJPEG配置まで。四隅評価sourceは `evaluation/dataset/images` と同名PNGの `evaluation/dataset/masks` を読み、HTMLとoverlayを書き出す。元の四隅座標や精度値を機械可読出力する完全な評価CLIとは記述されていない。入力画像なし／対応maskなしなら空集合でも完了表示しうるため、起動成功のみを検証成功にできない。

READMEがモデル取得元としてリンクする `app/download-tflite.gradle.kts` は、この固定revisionのtreeにはなく、raw取得も404。公式Python scriptの固定相対model path `../../app/build/downloads/fairscan-segmentation-model.tflite` を満たす配布model identity・配置を既存証拠と照合する必要がある。README記載だけで当該download手順が稼働するとは扱わない。

## 次の具体的経路

最小のdesktop候補は **公式Python mask生成＋変更なしの公式Kotlin quad evaluator**。独自の四隅アルゴリズムや移植を作る必要はない。一方でJVM evaluatorの初回ビルド・依存解決が必要で、現在の新規ビルド禁止境界は残る。将来この境界が変更された場合も、固定sourceをforkで管理し、公式処理を変更せず、起動経路・native load・入力件数・出力件数を確認してから独立注釈と比較する。現時点のVERIFY-01/02完了を意味しない。

一次資料: [公式README](https://github.com/pynicolas/FairScan/blob/2c5230b12f69f3cee2cc5a970013da20848d1272/README.md)、[評価README](https://github.com/pynicolas/FairScan/blob/2c5230b12f69f3cee2cc5a970013da20848d1272/evaluation/README.md)、[公式quad evaluator](https://github.com/pynicolas/FairScan/blob/2c5230b12f69f3cee2cc5a970013da20848d1272/evaluation/src/main/java/org/fairscan/evaluation/QuadDetectionEvaluator.kt)、[最新release](https://github.com/pynicolas/FairScan/releases/tag/v2.3.0)。

未実施: build・依存install・APK取得/実行・model推論・実機探索・外部連絡。リスク: source存在とmask推論の実測だけでは完成した検出経路や元実写真精度を保証できない。
