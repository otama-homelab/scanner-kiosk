# FSENet SD7K checkpointと作者紹介ONNXの直接parameter同一性

2026-11-11。公開時期やURLの違いだけからモデルが異なると判断せず、保持済み公式SD7K.pthとdocshadow_sd7k.onnxの実parameterを直接照合した。新しいモデル推論・export・重み変更は行っていない。

## 実測結論

**現在の公式FSENet SD7K checkpointの全991 state parametersが、作者紹介ONNXの全992 initializersへ値を変えず対応することを確認した。** 990件は同じ名前・shape・dtype・全raw bytes SHA256が完全一致。残1件は公式forwardで明示されるrow選択/broadcastが2initializerへfoldされていることをsourceとgraphで確認し、各row bytesも一致した。両artifactのparameter総bytesは117,592,520で一致。不一致・未対応は0。

| 検査 | 実測 |
|---|---:|
| 公式checkpoint state entries | 991 |
| ONNX initializers | 992 |
| 直接name/shape/dtype/rawbytes一致 | 990 |
| source-defined folded parameter一致 | 1（ONNX定数2本） |
| 同名不一致 / 未対応state / 未対応initializer | 0 / 0 / 0 |

対象checkpoint SHA256は`6a514e34cf2d3aff12eebd0c4f7b04466f47c1c0c41019d81091cc4d2defdc56`、ONNX SHA256は`8c09b9320a0fb3c53806cdf9cb8410b3706c77caed81f4ec1cb0bf2cbd14b049`。現在の公式sourceはb9395ed333c916051d7ae58be212ac62119b61f5、作者紹介exporterはec926bf36b4ac0778f836a2d34e27021447df27c。Jung/Kliglerには一般化しない。

## 最後の24bytesの証拠

公式models/model.pyのModel backboneはcoefficient_1_0をshape[2,3]で保持し、forwardでrow0[None,:,None,None]をlatent branchへ、row1[None,:,None,None]をtrans_low branchへ乗算してAddする。ONNXのonnx::Mul_12850とonnx::Mul_12854は各shape[1,3,1,1] float32・12bytesで、row0/row1のraw bytesへその順に完全一致する。graphも/backbone/latent/latent.0/Add_1_output_0へのMul、/backbone/trans_low/Slice_2_output_0へのMul、その2出力を足す/backbone/Addになっている。

これはsourceに書かれたrow選択/broadcastの分析であり、値を合わせるpermutation探索・係数fit・runtime weight transformationではない。parameter-mapping.jsonに991件の完全対応、folded-coefficient-identity.jsonに2定数のconsumerとAdd、coefficient-rows.jsonに元rowとhashを保存した。公式forward source自体もGit baselineと一致確認して保存した。

## 同一性と未証明の区別

以前の「現FSENetと旧ONNXの重み対応が不明」は、このSD7K二artifactのparameter値に限って解消した。年代・GoogleDrive URLが違うだけで「別の重み」とし続ける根拠はない。一方、ONNXとPTHという異なるcontainerのfile bytes同一性、実際のexport時に使ったGoogleDrive checkpoint fileのbyte hash、export command/environmentの完全build lineage、公式CUDA出力parityはこのparameter照合だけでは証明していない。

作者紹介exporterはDocShadowを作り、既存loaderでGoogleDrive ID 1a1dGcSFYB1ocR5KohSTjXIQcpQ0w05V7をロードし、opset12のtorch.onnx.exportを呼ぶ。今回その公式URLは200だがvirus-scan確認HTMLを返した。parameter identityを閉じた後に同じ117MB級modelを重複取得する必要はないため、HTML応答のURL/サイズ/hashだけ保存し、追加download/deserializeは行わなかった。

先行する同一native input tensor10件のCPU output比較は固定atol1e-5/rtol1e-4で4/10合格・6/10不合格のまま。parameter一致を理由に閾値を変えたり、数値parityを合格へ書き換えたりしない。parameter値同一性、graph/runtimeの数値差、実写真/製品の品質は別の証拠である。

## 条件と方法

current checkpointはtorch2.12.1+cpuのweights_only=True/map_location=cpuだけで安全ロードし、既存公式loaderが行うmodule prefix除去と同じ名前を記録した。ONNXは既存standard onnx.numpy_helper.to_arrayでinitializerをdecodeし、external dataがないことを確認した。標準のC-order bytes SHA256・shape・dtype比較を使い、新しい補正/変換/metricを作っていない。2CPU・各process2GiB sampledRSS/180秒以内、既存依存だけで測定した。

公式MIT repository/releaseと作者紹介MIT exporterの配布文脈は保持する。確認資料に重み除外はなく、専用modelcardや個別許諾文がないという理由だけで一律の権利者回答待ちには戻さない。ただし権利者からの独立保証を新たに取得したとは主張しない。元ILL-01の完了はrootが元の数値parity等の条件と照合して判断する。

未実施: 新規推論/export、GoogleDrive checkpoint本体の重複取得、公式CUDA parity、export build再現、実写真・本人受入・製品統合。リスク: 全parameter一致は品質合格や既存parity不合格の免除を意味しない。
