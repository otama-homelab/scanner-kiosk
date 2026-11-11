# 旧DocShadow保持出力の標準SSIM評価

2026-11-11。新しいモデル推論は行わず、保持済みOLD DocShadow native/512出力と公開合成clean/shadowの6組について標準SSIMを測定した。以前欠けていた標準metricだけを補完するため、新規の隔離/tmp venvへ公式PyPI binary wheelsを導入。製品・既存評価環境は変更していない。

## 標準実装と実行条件

scikit-image0.25.2のskimage.metrics.structural_similarityを使用。data_range=1.0、channel_axis=2、win_size=7、gaussian_weights=False、use_sample_covariance=True、K1=0.01、K2=0.03を明示し、独自SSIM式は実装していない。9依存wheel77,107,709bytesを公式PyPI release SHA256と全件照合し、offlineで新規venvへinstallした。download/staging peak154,523,224bytesで256MiB以内、sourcebuildなし。詳細version/SHAはwheel-identities.jsonとinstall-report.json。

2CPU affinityとOMP/OpenBLAS/MKL2threads、全process30秒・sampledRSS2GiB上限で逐次実行。36/36成功、SSIM本体0.085–0.427秒、最大RSS205,062,144bytes。compile/モデル推論と重ねず実行した。clean参照対自身6件は全てSSIM=1.0、全入力hash・finite・同寸法をself-checkした。

nativeはraw float出力をresize/clippingせずそのまま評価。512出力は公式表示と同じ既存OpenCV cv2.resizeで原寸へ戻す。OCR時の8bit clipping/PNG量子化はこのmetricには入れていない。data_range=1はモデルのRGB単位範囲を固定するためで、実出力の単位区間外値をclipしたことを意味しない。

## 結果

| 合成case | clean native | clean512 | shadow no-op | shadow native | shadow512 |
|---|---:|---:|---:|---:|---:|
| mixed | 0.986908 | 0.989343 | 0.986005 | 0.979124 | 0.983768 |
| table | 0.974092 | 0.963657 | 0.985915 | 0.969893 | 0.958308 |
| faint_mixed | 0.988803 | 0.994078 | 0.986012 | 0.979394 | 0.987834 |
| faint_table | 0.985479 | 0.983262 | 0.985958 | 0.977509 | 0.976540 |
| blue | 0.879936 | 0.761230 | 0.981057 | 0.869250 | 0.759660 |
| peach | 0.884106 | 0.713557 | 0.980886 | 0.871802 | 0.698782 |

既存paired manifestのclean SSIM≥0.99基準に対し、native0/6合格、512はfaint_mixedの1/6だけ合格。nativeのshadow SSIMは全6件でshadow no-opより低下。shadowには事前定義のSSIM閾値がないため、差分を新しい合否基準へ変えない。以前のPSNR/clean MAE/OCR/資源の不合格と整合し、今回標準SSIMを追加しても適用不可の判断は変わらない。

summary.jsonは全証拠hash、runs.jsonは実コマンド・時間・RSS・metric値、comparison.jsonは既存0.99閾値と差分を保持する。新しく補完したのは旧DocShadow合成診断のSSIMであり、現FSENet・独立実写真正解・変換元parity・本人受入を代用しない。

未実施: 新規推論、実写真/OCR再実行、現FSENet、変換元checkpoint parity、統合・配備。リスク: 合成画像・rawfloatと512復元の診断に限定され、製品の自動補正合格ではない。
