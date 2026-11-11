# 旧DocShadow ONNX native解像度のCPU評価

2026-11-11。512診断で残ったresize影響を分けるため、同じ公開合成6shadow＋6cleanを元寸法のまま全件評価した。現FSENetではなく作者紹介の旧DocShadow SD7K ONNX v1.0.0。model SHA256 `8c09b9320a0fb3c53806cdf9cb8410b3706c77caed81f4ec1cb0bf2cbd14b049`、公式runner revision `ec926bf36b4ac0778f836a2d34e27021447df27c`を再確認。公式preprocess/runを変更せず、既存CPU ORT1.23.2・2core/intra2/inter1を再使用した。

## 結果

12/12ケースで各3forward成功、計36forward。先行mixed_shadowの有効な900x700実測3回をsource/input/model/output hashes・config一致で再利用し、新規は11プロセス・33forward。900x700、1055x1491、1024x1536の全寸法で独自paddingやresizeなしに実行できた。出力shapeは全件で原寸法に一致し、finite検査を通過した。失敗・再試行・timeout/RSS killはない。

session load1.107–1.330秒、forward1.560–6.171秒、peak RSS706,609,152–1,356,152,832 bytes。全件2GiB診断枠内だが大きな入力は製品1GiB枠を超える。RSSは0.1秒程度の/proc samplingによる上限監視であり、hard cgroup上限保証とは扱わない。CPUExecutionProviderだけを使用し、ORTのGPU discovery警告はstderrに保持した。

| 合成case | 影PSNR改善(native対no-op,dB) | clean channel MAE(native) | 影+1dB / clean≤1/255 |
|---|---:|---:|---|
| mixed | +0.317 | 0.03135 | 不合格 / 不合格 |
| table | +0.774 | 0.03196 | 不合格 / 不合格 |
| faint_mixed | -0.210 | 0.02884 | 不合格 / 不合格 |
| faint_table | -0.216 | 0.02939 | 不合格 / 不合格 |
| blue | -2.006 | 0.06728 | 不合格 / 不合格 |
| peach | +0.361 | 0.04488 | 不合格 / 不合格 |

影改善基準0/6、clean保持基準0/6。nativeでは512よりclean MAEが全6件改善するが、閾値を満たすものはない。blueと薄文字2件はnativeでも影付きno-opより悪化した。512時の影改善4/6はnativeで再現せず、縮小設定を変えれば自動的に合格するとは言えない。このnative構成も不合格・apply不可と判断する。

## 測定範囲

OpenCV既存cv2.PSNR(R=1)とcv2.norm(NORM_L1)を使用し、native出力はresize/clippingせず同じclean rasterへ比較。既存4評価環境のmetadataにskimage/torchmetrics/piq/sewar/SSIM対応contribがなく、標準SSIMは未測定。新規依存や自作SSIMは追加していない。この既知のPSNR/MAE不合格はSSIM未測定で覆さないが、元ILL-01/02全条件の検証完了とは主張しない。

prior-evidence-bindings.jsonに先行native/512/paired-manifestのhash、identity.jsonにmodelと公式source、runs.jsonに正確なコマンド・出力hash・資源、quality.jsonにnative/512比較を保存した。11新規ケースのstdout/stderrを保持。raw NPYは/tmp/scanner-docshadow-native-v060/outputs、再利用mixed_shadowは従来/tmp/scanner-docshadow-v060/outputsにある。model/output pixelsはsourceへ追加しない。

未実施: SSIM、実写真/OCR、checkpointとの数値parity、現FSENet、本人端末受入、製品統合・配備。リスク: 合成データでの構成不合格をモデル一般の評価や実写真の合格に読み替えないこと。
