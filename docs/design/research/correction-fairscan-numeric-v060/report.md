# FairScan公式quad数値比較

2026-11-11。固定公式source `2c5230b12f69f3cee2cc5a970013da20848d1272` の `detectDocumentQuad` 戻り値を外部の薄いJava起動adapterでJSON化し、既存corner metricを無変更で再利用した。独自検出・独自metric・upstream sourcepatchはない。

## 結果

### 公開合成control6枚

既存の公式TensorFlow/OpenCV前処理で生成したmaskを再利用。四隅は検出3枚・null3枚。既定の最大corner誤差/対角長2% gateは **0/6合格、無補正は6/6合格**。

| control | 公式quad | 最大corner誤差/対角長 | 既存2% gate |
|---|---|---:|---|
| columns | null | — | 未検出 |
| horizontal | null | — | 未検出 |
| mixed | あり | 50.965% | 不合格 |
| table | あり | 33.222% | 不合格 |
| vertical | あり | 57.150% | 不合格 |
| wrapped | null | — | 未検出 |

無補正は同じ既知full-canvas四隅に対して最大誤差0。nullにfull-page四隅を代入して成功扱いにしていない。公式API内部のbounding-rectangle fallback使用有無は戻り値に含まれず、unknownとして保持した。

### 追加の独立UVDoc3文書

従来から採用していた独立grid2d正解の4端点を使うcorner診断。既存 `docaligner-metrics.py` の端点抽出と距離式をそのまま再利用した。人手corner注釈でも元DocUNet9枚の代替でもない。source hashで元9枚との重複は0件。

| ID | 公式quad | 平均corner誤差/対角長 | 最大corner誤差/対角長 | 同じ正解に対する既存無補正平均 |
|---|---|---:|---:|---:|
| 00000 | null | — | — | 11.794% |
| 00001 | あり | 0.752% | 1.466% | 10.378% |
| 00002 | あり | 1.649% | 2.147% | 8.444% |

検出した2件は無補正より平均誤差が小さい。一方で1件は未検出。追加3文書に対して新しい合否閾値を後付けしていない。nullを除いて平均を作り、全体品質と誤認させる集計もしない。

## 実行と再利用

- 合成6枚の外部adapter初回compile＋実行: 87.57秒、exit0、peak process-group RSS192,286,720 bytes。
- 追加3文書の公式無変更 `generate_masks.py`: 3.43秒、exit0、peak RSS548,569,088 bytes。全process30秒制限が各forward30秒より強い制限として成立。
- 追加3文書の同じcompile済みadapter: 11.15秒、exit0、peak RSS205,455,360 bytes。
- CPU affinity2、JVMheap768MiB、RSS2GiB監視。既存toolchain/runtimeを再利用。setup3tree合計3,158,456,947 bytes <3GiB。新しいモデルforwardは公開追加3枚の3回だけ。

[adapter](QuadCoordinates.java) は既存公式evaluatorと同じ `MatMask`、`Mode.CAPTURE`、`detectDocumentQuad`、`scaledTo`、TL/TR/BR/BL順を呼び出す。座標の再順序化・強制補正・confidence補完はしていない。Javaの有限値・入力件数・対応mask検査はserializationの境界検証のみ。外部Gradle init設定で追加source directoryを渡し、公式source272filesは両checkoutでhash一致した。

metricは [既存実装](../correction-detection-v060/measure.py) のfull-canvasGT、norm/diagonal、最大誤差、既存2%gateと、[既存3文書実装](../correction-validation-v060/docaligner-metrics.py) のgrid端点抽出・同じ距離・平均をAST単位で無変更実行した。[metric invocation](metric-invocation.py)、[metrics](metrics.json) に元source hash・行・実行した式を保持する。GT一致で誤差0、既存合成gateが大きいoffsetを拒否するselfcheckも成功。新しい距離式やgrid-to-quad heuristicは作成していない。

追加3文書のsource/metadata/grid2d/seg archive memberを取得時SHA256と再照合し、派生grid NPY hash、既存無補正fallback catalog、source hashを [truth binding](real-truth-bindings.json) とmetricsへ固定した。使用許可は従来の公開benchmarkローカル評価scope。画像・texture・私文書pixelsをsourceへ追加していない。

## 解釈上の制限

今回のAPI呼出しは公式desktop evaluatorに忠実で、PNGを `IMREAD_UNCHANGED` で読み込んだuint8の `MatMask` をcloneして渡す。公式検出sourceは途中で255倍のuint8変換を行うため、**この評価経路のmask値域とAndroid本体とのparityは未証明**。adapterで勝手に正規化を直してはいない。合成の失敗をモデル単体の能力だけに帰属させない。

元6実写真の独立注釈、必須photo2_2数字・細線truth、Android実機parityは残る。したがってVERIFY-01/02全体を完了にはしない。runtime不足は解消し、限定scopeの実際のcorner誤差・未検出を測れる状態になった。

[raw synthetic coordinates](synthetic-coordinates.json)、[raw real coordinates](real-coordinates.json)、[合成run](synthetic-run.json)、[追加mask run](real-segmentation.json)、[追加quad run](real-quad-run.json)、[data bindings](data-bindings.json)、[selfchecks](selfchecks.json)。

未実施: 元6実写真corner注釈評価、photo2_2内容truth、Android値域parity、製品統合。リスク: 合成0/6合格・追加3枚中1枚未検出のため、今回の結果から自動適用を採用できない。
