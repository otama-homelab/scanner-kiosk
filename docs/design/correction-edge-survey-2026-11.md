# 文書AI補正・エッジモデル調査

調査日: 2026-11-10。公開一次資料の調査。今回、新しいモデルの実行・導入・デプロイはしていない。
従来の候補表と評価結果は履歴として保持する。

## 結論

次の実測候補は **公式UVDoc ONNX版**。四隅だけでなく湾曲を扱い、公式の前後処理を利用できる。
最小サイズの比較対象は引き続きDocAligner。照明まで均一化したい場合はSyntheticDocの公開済みモデルが新しい調査対象になる。
「新しい論文」「端末実行できるモデル」「製品で利用できるモデル」は別々に判断する。
自作の補正アルゴリズムや独自学習は行わず、公式実装・既存ライブラリで比較する。

## 候補比較

| 候補・時期 | 補正範囲 | 配布・利用条件 | エッジ判断 |
| --- | --- | --- | --- |
| [DocAligner 1.1.1](https://github.com/DocsaidLab/DocAligner/releases/tag/1.1.1)、2026-01-13 | 四隅検出＋既存ライブラリの射影変換。紙の湾曲・影は対象外 | LCNet100 ONNX 4,767,987 bytes、Apache-2.0 | 小さいモデルの基準。ただしPython依存全体と画像処理の負荷は別 |
| [UVDoc公式ONNX](https://huggingface.co/PaddlePaddle/UVDoc_onnx) | 非線形グリッドによる湾曲・幾何補正 | 31,684,150 bytes、モデルカードApache-2.0。公式PaddleOCRにORT推論経路あり | 次の第一候補。CPU実行可能性の資料があるが、スマホ・WebGPU互換性は未検証 |
| [UVDoc Transformers対応](https://huggingface.co/docs/transformers/en/model_doc/uvdoc)、2026-03-21追加 | 同系統の幾何補正 | [公式safetensors](https://huggingface.co/PaddlePaddle/UVDoc_safetensors)、7.91M params、31,667,380 bytes、Apache-2.0 | 新しい実行経路であり、新しいTransformer構造の補正モデルという意味ではない。PyTorch一式の負荷に注意 |
| [DewarpNet-LiteRT](https://huggingface.co/litert-community/DewarpNet-LiteRT)、配布カード2026-09更新 | CNNによる非線形グリッド。画像の変形適用はホスト側 | コミュニティ変換、188,852,928 bytes、MIT表記。変換時のネットワーク置換・開発版ツールあり | 実端末記録のある比較候補。ただし小容量ではなく、公式原著者の新モデルでもない |
| [SyntheticDoc](https://github.com/tanguymagne/SyntheticDoc)、ECCV 2026／[arXiv 2026-09](https://arxiv.org/abs/2609.15503) | UVDoc系共有encoder＋幾何map／shadow mapの二出力 | ソースMIT。[公式training README](https://github.com/tanguymagne/SyntheticDoc/blob/main/training/README.md)に2種類の学習済みモデルとCPU推論指定あり。重みの個別条件・hashは未確認 | 湾曲と照明の両方を改善する研究候補。公式エッジ速度・メモリ・ONNX配布は今回確認できず |
| [D2Dewarp](https://github.com/xiaomore/D2Dewarp)、CVPR 2026 | 水平・垂直線の幾何を使う歪み補正 | コード・重みリンクあり。READMEは非商用研究用CC BY-NC-ND 4.0 | 製品候補から除外。新しい論文の比較参照に留める |
| [AADD](https://github.com/chaoyunwang/AADD)、AAAI 2026 | 軸方向の整列制約 | READMEはコード・モデルをComing Soonと記載 | 入手可能な完成モデルとして数えない |
| [ForCenNet](https://github.com/caipeng328/ForCenNet)、ICCV 2025 | 前景を使う幾何補正 | READMEの評価コード・デモ公開項目が未完了 | 今回は再現可能なエッジ候補に進めない |
| [TADoc](https://arxiv.org/abs/2508.06988)、2025 preprint／2026出版 | 時間モデル・疎グリッドによる軽量化 | 論文は軽量と主張。公式の実行可能なモデル配布・端末測定を今回確認できず | lightweightという説明だけで採用しない |

DocResは多タスクTransformerの比較対象だが、[公式推論手順](https://github.com/ZZZHANG-jx/DocRes)はDocResに加えてMBD重みを要求する。
単一モデルのサイズだけで全経路の軽さを判定できず、CPU・端末向けの再現可能な測定を今回確認できなかったため優先しない。

検索上の新着日にも注意する。[DRNetのarXiv登録](https://arxiv.org/abs/2610.15445)は2026-10だが、同ページのCommentsはTCSVT 2023採択と記載している。
登録日だけで2026年の新方式とは扱わない。

## 速度・メモリの根拠と比較限界

- [PaddleOCR公式測定](https://www.paddleocr.ai/latest/en/version3.x/module_usage/text_image_unwarping.html): UVDoc CPU 869.82 ms、GPU 19.05 ms。CPUはXeon Gold 6271C・FP32・8 threads、GPUはTesla T4。**前後処理を除く推論単体**。スマホの測定ではなく、ONNX版にそのまま引き継げる数字でもない。公式表の30.3 MB表記と上記artifactのbytesは区別する。
- [LiteRT変換者の測定記録](https://github.com/john-rocky/edge-compat/blob/main/cards/dewarpnet/CARD.md): Raspberry Pi 5 CPU/XNNPACK p50 593.957 ms、peak memory 490.2 MB表記。M4 Max上Chromium 151／LiteRT.js 2.5.3 WebGPUはp50 22.56 ms、WASMは633.472 ms。**モデル単位の第三者測定**で、画像読込・高解像度変形・保存・OCRの総時間ではない。スマホCPUの性能を示さない。
- LiteRT配布カードにはPixel 8a GPU約24 msの主張もあるが、ここでは詳細環境の残る上記記録を主な根拠とする。配布モデルのhashと測定artifactのhashは一致。変換ツールは開発checkout＋local patchesなので、安定版だけで再現できるとは判断しない。
- 既存の当repoのDocAligner評価は合成10画像すべてがunsafe-or-missing-cornersで原本fallback。実補正は0件。処理全体は別CLI起動ごとに約0.95–1.21秒、peak約384 MiB。graph単体のwarm約33 msを製品の応答時間とは扱わない。合成画像の結果から実写真でも失敗するとは結論しない。
- モデルサイズはRAMではない。入力解像度、変形用map、出力画像、runtime、前後処理を含むpeakと、初回／常駐後の時間を別々に測る必要がある。

## スマホOSの既存経路

[Android ML Kit Document Scanner](https://developers.google.com/ml-kit/vision/doc-scanner)は端末内処理を提供する。
[Android導入資料](https://developers.google.com/ml-kit/vision/doc-scanner/android)によれば、Google Play servicesがモデル・処理・UIを初回downloadし、Android API 21以上・端末RAM 1.7GB以上を要求する。
アプリ増分約300KBはモデル総サイズではない。ネイティブActivityと撮影／gallery importのUIを提供するSDKであり、ブラウザーやサーバーで任意画像に適用できる公開重みではない。
現在のWebアプリとCanon画像の共通経路には直接は適合しない。将来ネイティブ撮影を選ぶ場合の比較基準になる。

## scannerへの推奨

1. UVDoc公式ONNXを公式前後処理ごと固定し、公開の実写benchmarkでno-op／DocAlignerと比較する。まずCPUで足りるか判断し、GPUサービスは先に作らない。
2. 「均一化」は幾何と照明を分けて採点する。UVDocだけで影・色むらまで直るとは期待しない。SyntheticDocは次順位として、重み条件と配布物を確認してから比較する。公式READMEはページ付近への事前cropを要求しており、背景の広いスマホ写真をそのまま万能補正できるモデルではない。
3. 非線形モデルには現在の四隅contractを流用しない。既存ライブラリの変形mapを保持できるcontractと、対応する検証oracleを先に定める。自作のmap推定や補正手法は追加しない。
4. 写真とCanonの入口は共通化しても、きれいなscanはno-opを維持する。適用時は原本保存・preview・明示apply・undoと、入力元に応じたSave／reOCRの既存規則を守る。
5. Transformer OCR／VLMは読取りの別候補。歪んだ写真を読めても補正画像が生成されるわけではない。LLMによる文字や画素の描き直しはこの補正経路に入れない。

## 配布物の同一性（API metadata確認、今回ダウンロード未実施）

| 配布物 | revision | SHA-256（配布元LFS metadata） |
| --- | --- | --- |
| PaddlePaddle/UVDoc_onnx `inference.onnx` | `3bcf535371727d11e783101f79a504c68848aae3` | `54cab30dc2cf347b4f3d6971c833b1c3d84f5ef17280f1f6d15796bff20e63d6` |
| PaddlePaddle/UVDoc_safetensors `model.safetensors` | `bfa6aad03b549c7e2cf89cefd2fb02ba3bef2ae4` | `65f980e3083b4919d962d7b4c8ddb42a926dc8b36f4909944746cf2d7b7325dc` |
| litert-community/DewarpNet-LiteRT `dewarp.tflite` | `dfc68e3e462a6eb7eb8c4dbbff4049e3bd3cc9e3` | `0f72fc016f50bd16cc2055e0b499a4317702ed81b3ff6672fd9affe0bad6d064` |

製品採用前には取得ファイル自身のhashと利用条件を再確認する。旧UVDoc checkpointとPaddle版の同等性は未検証。
今回の調査は網羅的なsystematic reviewではなく、端末利用・公式配布・製品適合性に絞った一次資料の比較である。
