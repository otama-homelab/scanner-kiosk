# 現FSENetのCPU portability fork実測

2026-11-11。旧DocShadowだけの結果に留めず、公式FSENet SD7K.pthを実際に取得・安全ロード・推論した。これは公式source無変更runtimeではなく、CPU checkpoint loadingだけを1行変更したprivate local forkの測定である。

## source・重み・CPU環境

公式baseline `b9395ed333c916051d7ae58be212ac62119b61f5`を/tmpにGit cloneし、private branch scanner-cpu-checkpoint-portabilityのcommit `efe9400c72e09ab8fa0d67bf2b248f4edad9adde`を作成。唯一の変更はutils/utils.pyのtorch.load map_locationをCUDA固定からcpuへ変更しweights_only=Trueを明示した1行。全tracked fileをbaselineと照合し、model/backbone/blocks/data/inferのsourceは同一。独自補正・architecture変更・weight key加工・exporter・importshim・monkeypatchは追加していない。GitHub fork/pushはない。

公式MIT repositoryのWeights releaseからSD7K.pth117,980,483bytes、SHA256 `6a514e34cf2d3aff12eebd0c4f7b04466f47c1c0c41019d81091cc4d2defdc56`を取得。safe weights_only CPU loadで991state entriesを読み、元のload_checkpoint処理（既存module prefix除去を含む）でstrict model load成功、29,398,130parameters。許可されていないpickle allowlistやweights_only=Falseは使わなかった。

新規/tmp venvだけにCPUtorch既存site-packagesへのread-only.pth参照と公式PyPI binary dependenciesを配置。公式指定accelerate0.17.1/torchmetrics0.11.4/einops0.6.1/yacs0.1.8/albumentations1.3.0を使用。最初のAccelerate importはPython3.12のdistutils廃止で失敗し、通常の公式setuptools80.9.0によるvendor distutilsで解消した。失敗ログを保持し、sourceを追加patchしていない。全wheelをPyPI SHA256と照合しoffline install。setupは3GiB未満、追加dependency downloadは512MiB未満。既存CPU環境・製品dependencyは変更していない。

## 公式infer実行と資源

公式infer.infer()をそのまま呼び、公式DataLoader num_workers8、公式Albumentations data path、forward、torchmetrics、save_imageを保持。標準PyTorch計測hookは最上位Modelの時間/rawtensor保存/30秒alarmだけを行い、input/returnを変更しない。CPU2core/intra2/inter1、全process180秒。親RSSとは別に、親＋全workerのaggregate PSSを約0.1秒sampleして2GiBを超えたら所有する検証process groupをkillする。これはRSSのhard kill保証ではない。

nativeは10/12forwardを完了し、最後のblue_clean予測/GT保存まで存在する。parent peak RSS1,643,376,640bytes、aggregate peak PSS2,180,001,792bytesとなり2GiB診断上限で停止。peach2件の成功出力はない。10件目の公式metric処理か次case前後かは計測していないので停止行を推測しない。36秒程度までの有効な10出力を保持し、native corpus完走とは扱わない。

次にsource/weights/algorithmを変えず、許可済み公式config TRAINING.ORI=Falseだけで512経路を別directoryへ実行し12/12完了。parent RSS814,211,072bytes、aggregate PSS1,147,172,864bytes。総forward22回。512標準出力はPSNR21.5759dB/torchmetrics SSIM0.929655/RMSE23.9364(255scale)。512は入力/target両方が公式DataLoaderのA.Resizeで512になる条件であり、原寸へ復元した旧ONNX512診断と同一条件ではない。

## 合成品質

| 構成 | 影PSNR+1dB | clean MAE≤1/255 | clean標準SSIM≥0.99 |
|---|---:|---:|---:|
| native | 0/5 | 0/5 | 0/5 |
| 512 | 4/6 | 0/6 | 3/6 |

既存OpenCV PSNR/normと隔離scikit-image0.25.2標準SSIMを使用。nativeのreferenceは原寸、512のreferenceは公式save_image(tar)から得た512target。raw predictionをclipせず比較し、SSIM data_range1/channel_axis2など既存固定parameterを保持。reference自己一致SSIM=1も確認。全clean MAEが不合格なので、SSIM一部合格を安全な自動適用に読み替えない。digits/細線/印章のmissing=0等はこのglobal metricで検証しておらず、元ILL-02全要件完了には数えない。

## 旧ONNXとの限定比較

nativeで重なる10ケースについて、旧ONNX公式preprocessと現FSENet公式DataLoaderのRGB tensor byte hash/shapeを完全一致確認した。同じ入力に対する既存CPU出力を事前固定rtol1e-4/atol1e-5でNumPy assert_allclose比較した結果は4/10合格・6/10不合格、最大絶対差5.85318e-5。小さい差でも閾値を調整せず不合格を保持する。これは限定されたCPU output比較であり、checkpoint配布lineageや公式CUDA parityの証明ではない。512はPillowとAlbumentationsでpreprocessが異なるため比較対象から除いた。

official-infer-512-wrapper-original.jsonのcwd記述は旧fieldのままrootを示すが、実際のPopenはrun512を使っている。保存したwrapper sourceとconfigを根拠にofficial-infer-512.jsonへ訂正し、原receiptも残した。失敗や条件差を隠す再実行ではない。

未実施: native残2件の完走、current FSENet OCR追加、実写真・内容欠落/印章・本人端末受入、公式CUDA parity、製品統合・配備。リスク: CPU portability forkの実測と512部分改善は、公式無変更runtime・全品質合格・採用承認を意味しない。
