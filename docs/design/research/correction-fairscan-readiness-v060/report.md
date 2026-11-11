# FairScan公式JVM評価の実行準備判定

2026-11-11。HTTP再取得なし。前回の固定公式source記録と、ローカルcommand/path/model metadataだけをread-onlyで確認した。[環境記録](environment.json)。

## 境界の訂正

本人の明示要件は「自力の補正アルゴリズム作成は避けてください」。親AGENTSは「自己ビルドは最小限とし、公式配布物・公式設定を優先」「upstreamコードの修正とbuildが必要ならfork」である。**公式sourceを無変更で一度コンパイルすることの全面禁止は、本人の要件ではなく前回designと66-01 PLANが導入した実装上の選択**。この禁止だけを理由に人間の許可待ちへ固定する必要はない。rootは自律評価指示に基づき計画を改訂できる。独自algorithm・Kotlin/Python移植・emulator禁止は維持可能。

## 現在の技術的判定

準備済み環境だけでの即時実行は **NO-GO**。PATHにはJava8 JREのみ (`1.8.0_512`)。`javac`、`gradle`、`kotlinc`、`adb` はない。`/usr/lib/jvm` の観測はJava8のみで、`~/.gradle`、`~/.sdkman/candidates`、`~/Android/Sdk` もない。host全filesystemや別hostを探索したとは主張しない。

これは外部権利者や本人端末待ちとは異なる通常のtoolchain準備不足であり、公式JDK/Gradleの隔離導入で解消する余地がある。JVM11はevaluatorのbytecode targetで、root Android Gradle Plugin9.5.0を動かすJDKの必要版を11と断定しない。

## 最小の具体的実行経路

1. 固定revision `2c5230b12f69f3cee2cc5a970013da20848d1272` の公式sourceを隔離checkoutし、source無変更をhashで確認。公式wrapper設定の対応JDKを隔離して用意し、root/global環境を変更しない。source修正が必要になった場合は親AGENTSどおりforkへ移す。
2. root projectの公式Gradle wrapperを使い、`--configure-on-demand :evaluation:classes` によるJVM評価moduleだけのcompileを最初の候補にする。root settingsはAndroid appもincludeするため、SDKなしのconfigurationが成功することは未実証。失敗時は原因を記録し、sourceやalgorithmを勝手に書き換えない。
3. 公式evaluatorのmain classは `org.fairscan.evaluation.QuadDetectionEvaluatorKt`。`evaluation` の `sourceSets.main.runtimeClasspath` でJavaExec起動するのが責任境界に合う。公式build.gradleにapplication/run taskがないので、外部Gradle init scriptでJavaExec taskを一つ登録する方法が候補。これは起動設定であり四隅algorithmの実装ではない。command/classpath/native loadはcompile後に実証する必要がある。
4. cwdをcheckout rootにし、`evaluation/dataset/images/*.jpg` / `*.jpeg` と同名の `evaluation/dataset/masks/*.png` を配置。公式Python `generate_masks.py` は前者からmaskを生成するがglobは `*.jpg` のみ。既存raw maskを同一前処理の公式8bit PNGへ対応付ける証拠を固定してから使う。
5. 既存公式TFLite modelは `/tmp/scanner-v050/upstream/fairscan-segmentation-model.tflite` に4,921,040 bytesで存在。SHA256は `96e14d7e610dd0c27b768b228fbc553b4ec119ebe68f3a3594029a25400691d2`。公式Python固定pathは `app/build/downloads/fairscan-segmentation-model.tflite` なので、同じbytesを隔離checkoutへ配置すればモデル再downloadは不要。TensorFlow準備不足を理由に既存LiteRT測定を捨てず、既存mask identityとの対応を検証する。
6. 四隅evaluatorの公式出力は `evaluation/reports/results/*_input.jpg`、`*_output.jpg` と `evaluation/reports/index.html`。maskを公式 `detectDocumentQuad(..., Mode.CAPTURE)` へ渡すが、corner座標JSONや独立GTとの精度数値は出力しない。入力/対応mask数、処理件数、出力件数、検出なしを明示し、空集合でのDone表示を成功にしない。品質の数値検証は既存許可metric経路との接続が別途必要で、新metric algorithmを作らない。

公式sourceの実行経路を一度成立させることは前進になるが、元実写真の独立注釈不足を解消せずVERIFY-01/02を完了にはしない。新規実行はCPU・時間・temp・RSSの既存上限内で直列化し、過去の失敗と現baselineを保持する。

未実施: JDK/Gradle導入、compile、JavaExec、model推論、source変更、Android実機探索。リスク: 現行root Gradle構成のSDK要求、OpenCV native対応、公式出力の精度計測不足は実行して初めて確定する。
