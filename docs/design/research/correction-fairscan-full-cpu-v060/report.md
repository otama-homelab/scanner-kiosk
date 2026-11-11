# FairScan公式CPU segmentation→quad経路の実行

2026-11-11。公式固定revision `2c5230b12f69f3cee2cc5a970013da20848d1272` のPython前処理・TensorFlow Lite推論から公式JVM四隅stageまで、公開合成control6枚で実行が成立した。先行66-03のPillow診断mask依存を今回の実行では解消した。数値corner精度と元実写真の独立truthは別の未完条件。

## 実測

| 工程 | 実際の結果 |
|---|---|
| binary wheel取得 | 38件、379,377,173 bytes、9.23秒、全件PyPI公開SHA256一致 |
| 隔離venv install | offline/no-deps、34.77秒、exit0 |
| 公式 `generate_masks.py` | 6枚のmask、3.05秒、exit0、peak process-group RSS640,225,280 bytes |
| 公式 `QuadDetectionEvaluatorKt` | 同じ6枚処理、11.08秒、exit0、peak process-group RSS106,598,400 bytes |
| 生成物 | mask PNG6枚、input/overlay JPEG12枚、HTML1件 |
| 最終combined setup-tree | 3,142,012,479 bytes <3GiB |
| 評価run temp/masks/reports | 366,509 bytes <256MiB |

CPU affinity2、TensorFlow intra/inter-op threads2、Gradle workers2。segmentation process全体に30秒上限を適用し、各forward30秒より強い制限として成立した。診断RSSは2GiB監視。公式scriptにtiming wrapperやimport shimを挿入していない。rootからheavy semaphore GOを受領して実行した。

## 公式実装とruntime

既存runtimeを一度確認したがTensorFlowは見つからなかったため、公式PyPI binaryの `tensorflow-cpu==2.19.1`、`opencv-python-headless==4.12.0.88`、`numpy==2.1.3` と解決された依存を専用venvへ導入した。全38wheelのversion/hash/URL/metadata licenseを [wheels](wheels.json) に固定。nvidia/cuda/cudnn packageなし、source buildなし、global installなし。1wheel256MiB・total512MiB以内。TensorFlow wheelは251,917,833 bytes。

実行したのは **公式 `tensorflow.lite.Interpreter`**。既存の `ai_edge_litert` に置き換えず、officialscriptを一切変更していない。`tensorflow.lite.Interpreter` の将来削除警告はlogへ保持した。2.19.1で今回のAPIは実際に動作した。

前処理も公式sourceそのままの `cv2.imread` → BGR/RGB変換 → `cv2.resize(..., INTER_LINEAR)` → float32 `/127.5-1`。公式出力のclip/round/uint8/PNG保存まで含む。modelは既存公式TFLite bytesを固定pathへcopyし、SHA256 `96e14d7e610dd0c27b768b228fbc553b4ec119ebe68f3a3594029a25400691d2` を確認した。

control6枚の元PNG bytesを `.jpg` 名で配置した。公式scriptがjpgだけを列挙するためのbyte-preserving aliasで、画像再圧縮はなくOpenCVのcontent検出で読み込まれた。公開renderer controlのみで、本人の私文書は使っていない。

今回の全chainには別source workingdirを用い、先行stage-only実験の画像・mask・reportを上書きしなかった。元272source filesのhashは全件一致。外部init scriptは、既にcompile済み公式classes/runtimeClasspathと今回workingdirを結ぶ起動設定だけ。四隅algorithm、metric、移植、exporterを作成していない。

## 何が解消し、何が残るか

「公式FairScan全経路をdesktopで実行できない」というruntime不足は、今回の **公式segmentation＋公式quad stageの実行** によって解消した。出力overlayは6件とも生成されたが、公式mainはquad検出数・corner座標・精度値を出力しない。従ってquad検出6/6成功や精度合格とは判定しない。製品アプリ全体のpixel parity、Android実機、独立元実写真GT比較も証明していない。

公式 `DatasetEvaluator` には既存quadから `extractDocument` へ進む別のHTML評価sourceもある。ただし今回の契約はsegmentation→quadであり、その別mainや補正出力を実行したとは記録しない。確認した既存評価sourceはHTML中心で、数値corner precisionを自動的に提供する経路は確認できなかった。

[入力source/model identity](inputs.json)、[segmentation command/result](segmentation.json)、[segmentation log](segmentation.log)、[JVM command/result](evaluation.json)、[JVM log](evaluation.log)、[出力hash](outputs.json)、[selfchecks](selfchecks.json)、[installed inventory](installed-packages.json)。raw評価JSONのtemp_tree_bytesは旧JVM treeだけの値であり、combined quotaの最終確認値はselfchecksの3,142,012,479 bytesを参照する。broadtree unchangedfalseも生成Gradle cacheの変化であり、元272source一致は別途selfcheckで確認した。

未実施: 数値corner精度、元実写真独立GT比較、Android pixel parity、製品組込。リスク: 公式pipelineの実行成立は検出品質・適格性・VERIFY-01/02全要件の合格ではない。
