# FairScan公式JVM四隅stageの実行成立

2026-11-11。前回の全setup-tree1GiB停止を保持し、今回だけsetup/toolchain/cache3GiBと評価出力256MiBを分離した計画66-03で再開した。disk空き692,236,685,312 bytesを事前確認。公式source・algorithmは変更していない。

## 実測

| 項目 | 結果 |
|---|---|
| 公式module compile | 成功、31.26秒、exit0、peak process-group RSS162,590,720 bytes |
| 公式 `QuadDetectionEvaluatorKt` | 成功、15.81秒、exit0、peak process-group RSS159,088,640 bytes |
| 処理した入力 | 既存の公開合成control6枚＋identity一致の既存診断mask6枚 |
| 出力 | JPEG12枚（input/overlay各6）＋HTML1件、処理log6件 |
| 最終setup-tree | 1,195,190,133 bytes、3GiB未満 |
| 固定公式source | revision2c5230b12f69f3cee2cc5a970013da20848d1272、元272filesのSHA256全件不変 |

公式Temurin21.0.13+9／Gradle9.8.2と既存dependency cacheを再利用した。`:evaluation:classes` と `:imageprocessing:compileKotlin` が実際に完了し、Android SDKの新規導入なしで成立した。CPU affinity2、workers2、daemon heap768MiB、RSS2GiB監視、各attempt180秒の範囲。rootからheavy実行GOを受けて直列実行した。

公式buildにrun taskがないため、外部 [init script](evaluate.init.gradle) で `evaluation` の公式runtimeClasspathとmain classを結びつけた。sourcepatch・移植・独自四隅algorithmではない。公式 `OpenCV.loadLocally()` が読み込まれ、公式 `detectDocumentQuad(..., Mode.CAPTURE)` の呼出しを含む既存mainを実行した。

[compile command/result](build.json)、[compile log](build.log)、[評価command/result](evaluation.json)、[評価log](evaluation.log)、[入力identity](inputs.json)、[出力identity](outputs.json)、[selfchecks](selfchecks.json) を保持。数値corner座標やquad検出数は公式mainから出力されないため、12overlayが存在するだけで検出成功6件とは記録しない。

## 入力と適用範囲

元controlはrenderer-knownの公開合成PNG。公式mainは拡張子jpg/jpegだけを選ぶため、**元PNG bytesを無変更で.jpg名へcopy**し、OpenCVのmagic decodingで実際に読めた。JPEG再圧縮や原本pixelの変形はしていない。catalogのprivacy分類がowner-privateでもdocument_typeはpublic-synthetic-known-rendererであり、本人の私文書は使っていない。

maskは前回LiteRT診断のraw256×256 float32を再利用した。元raw hashは既存metrics記録と6/6一致。公式mask scriptと同じclip→×255→round→uint8のquantizationでPNG化し、decode後のuint8完全一致をassertした。前回の推論前resizeはPillowで、公式OpenCV scriptの前処理ではない。従って今回の成立範囲は **既存診断maskを入力とした公式四隅stageのdesktop実行**。公式segmentation全経路のparity、新しいモデル推論、元実写真精度、完全pipeline品質を主張しない。

公式Python mask scriptを無変更で実行するためのTensorFlow/cv2はこのCPU環境に揃っておらず、新たな重いruntime導入は行っていない。既存maskを無根拠に再推論することも避けた。

## 証拠の補足

raw build/evaluation JSONの`original_source_bytes_unchanged:false`は、再開scriptがGradleの生成mutable filesまでsnapshotした結果。元sourceの変更を示す値ではない。過去の元272filesに対する独立の [source identity](source-identity.json) はmismatch0。false値を改変せず双方を保存した。

前回の1GiB監視停止は失敗として残る。今回の成功はsourceやmodelを変更したretryではなく、具体的なsetup容量原因への計画修正である。公式sourceの初回compileが成立したので「互換公式binaryが出るまで評価不能」という制約は解消した。数値検証用のcorner出力と元実写真の独立truthは別の残件で、VERIFY-01/02の全要件をcheckedにはしない。

未実施: 全公式segmentation前処理の再推論、corner精度値、元実写真GT評価、製品組込。リスク: 四隅stage実行成功はquad検出数・精度・製品適格性の合格を意味しない。
