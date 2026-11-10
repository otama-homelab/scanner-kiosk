# 文書補正モデル・商用製品・OSSの横断調査

調査日: **2026-11-10**。前回の[edge調査](correction-edge-survey-2026-11.md)を拡張した、単独調査者によるnarrative/scoping review。既存の[v0.4.0実測](edge-correction-evaluation-v040.md)とは区別する。今回は新しいモデル推論、独自学習、補正アルゴリズム作成、製品導入を行っていない。

## 判断の更新

文書スキャン全体のデファクトと断定できる単一モデルは確認できなかった。公開採用例からは、**小型の文書専用検出器＋既存画像処理、湾曲補正、照明補正を分担する構成**が実用上の参考になる。MODNet、MobileNet、U²-Netは異なるモデルである。

- **文書検出の新しい比較候補:** FairScanが実際に配布・使用するDeepLabV3+／MobileNetV2。量子化TFLiteは約4.9MB。アプリのモデル利用箇所とreleaseを確認した。
- **影・地色の比較候補:** DocShadow／FSENet。原著者の重み配布があり、原著者READMEからリンクされたコミュニティの配布済みONNXもある。SyntheticDocだけに候補を絞る必要はない。
- **湾曲補正の優先再評価:** UVDoc公式Transformers経路。モデルが返す2次元meshと公式samplingを確認した。前回の「mapなし」は評価したONNX artifactの制約であり、UVDoc方式全体の制約ではない。品質合格を意味しない。
- **2026研究の監視:** ReWarp、SyntheticDoc、BookNet、AADD、ArbDR、MMDIR、DR-Mamba等。論文の新しさ、配布済み実装、公開重み、利用条件、端末実測を別々に判定する。

これらは次回比較への推奨であり、ロードマップへの新phase追加・採用承認ではない。v0.4.0の非導入という評価結果は保持する。

## 何を補正するモデルか

| 責務 | 出力・目的 | 代表例 | 限界 |
| --- | --- | --- | --- |
| 検出・切り抜き | 文書mask／四隅 | MobileNet系文書検出、FairScan、U²-Net、DocAligner | maskだけでは湾曲も影も補正できない |
| 遠近補正 | 四隅から平面へ既存の射影変換 | OpenCVを使うscanner library | 本の喉・折れ・波打ちは残る |
| 非線形幾何補正 | mesh／flowに沿った画素の再配置 | UVDoc、DewarpNet、DocScanner、ReWarp | OCR改善だけでは数字・細線・幾何の保持を証明できない |
| 照明・外観補正 | 背景照明／shadow mapまたは補正画像 | FSENet、SyntheticDoc、DocRes | 薄字・色地・写真・ハンコへの影響を別評価する |
| 二値化・読取り | 白黒画像、文字・構造 | DR-Mamba、Tesseract、OCR／VLM | 色の忠実性や補正済み保存画像の代替にはならない |

## 商用アプリ・SDKの公開採用技術

「製品で使用すると開発元が公表」「ソースで呼出しを確認」「機能のみ公表」を区別した。公開論文や特許が同じ会社にあるだけでは、そのモデルのアプリ採用とは数えない。各社の速度・精度は独自環境の自己報告であり、当scannerでの横並び測定ではない。

| 製品 | 一次資料で確認できた技術 | 公開・再利用の境界 | scannerへの示唆 |
| --- | --- | --- | --- |
| **Genius Scan** | 2024技術記事で**MobileNetV2、96×96入力、四隅回帰**を明示。2021世代は古典的処理で輪郭を精密化、2024世代は追加NNで精密化 | 専用学習済み重みの一般公開は確認できず。2026 SDK6で同じアプリのエンジンを提供 | 小さい検出器で撮影を支援し、確定画像で精密化する設計の実例。公表されたbackboneだけで同品質は再現できない |
| **Scanner Pro / Readdle** | 文書検出は**MobileNet系keypoint検出＋撮影後segmentation**。2026年3月v8.34で色処理をNN方式へ更新。global／local／patchの3段階、Apple Neural Engine利用を公表 | 色・影モデルの名称／重みは非公開。旧影処理の記事はMetal上の背景照明推定を説明 | 2026年でも役割別の小型処理を組み合わせる。古い古典処理の説明と最新NNの説明を混同しない |
| **Google Drive Android / Pixel Camera** | Googleの2024発表が**ML Kit Document Scanner API採用**を明示。検出、影・汚れ除去等を端末内で処理 | モデル・UIはGoogle Play services経由。公開checkpointではなくAndroid用SDK | 完成SDKを使う経路の実例。現Web／Linuxの共通処理へそのまま移植する対象ではない |
| **Apple VisionKit Document Camera** | WWDC21で、Neural Engine搭載機では旧rectangle requestに代えて**document segmentation request**を使うと説明 | Apple OS APIであり任意サーバーへ持ち出せる重みではない。2026内部構造までは不明 | iPhone専用clientの参考。単に四角を検出するAPIと文書専用segmentationを区別する |
| **Adobe Scan** | 公式helpは本の左右ページ撮影・曲率補正のStraighten Imageを説明。公開初期資料ではSenseiを使用と説明 | これだけではUVDoc、MODNet、Transformer等の具体的採用を特定できない | 機能・操作の比較対象。非公開実装を推測してモデル候補へ昇格させない |
| **CamScanner / INTSIG** | 公式製品資料でAI edge detection、Magic Pro等を公表 | 今回確認した資料では補正モデル名／checkpointは不明 | 製品品質の参考。OCRモデルと画像補正モデルを同一視しない |
| **Scanbot SDK** | WebとLinuxにも文書補正経路があり、Linux v10資料にCPU対応、湾曲・折れ補正、画質評価、offline処理を明記 | 商用ライセンスSDK。具体的NN／重みは非公開。cleanupには文字・ハンコ等の削除機能もある | **サーバーで写真とscanを共通処理する既製品**の比較対象。費用・再配布条件の確認が必要。cleanupを忠実保存の既定にしない |

出典:

- Genius Scan: [検出技術](https://blog.thegrizzlylabs.com/2024/10/document-detection.html)、[TFLite・INT8による回転判定](https://blog.thegrizzlylabs.com/2024/01/genius-scan-auto-rotate-technology.html)、[SDK6発表](https://blog.thegrizzlylabs.com/2026/05/announcing-genius-scan-sdk-6.html)。回転判定の676KBを四隅検出モデルのサイズと取り違えない。
- Readdle: [文書検出](https://readdle.com/blog/scanner-pro-border-detection)、[旧影処理の技術解説](https://readdle.com/blog/scanner-pro-shadow)、[2026色・影補正更新](https://readdle.com/blog/why-scanner-pro-scans-look-better-than-ever)。
- Google: [採用アプリを明記した公式発表](https://android-developers.googleblog.com/2024/02/ml-kit-document-scanner-api.html)、[ML Kit機能・提供形態](https://developers.google.com/ml-kit/vision/doc-scanner)。
- Apple: [WWDC21文書検出の説明](https://developer.apple.com/videos/play/wwdc2021/10041/)。2021時点の採用事実として扱う。
- Adobe: [iOS公式help](https://www.adobe.com/devnet-docs/adobescan/ios/en/scan.html)、[2017公式発表](https://blog.adobe.com/en/publish/2017/05/31/new-adobe-scan-app-for-document-cloud)。
- INTSIG: [CamScanner公式](https://www.intsig.com/camscanner/)。
- Scanbot: [Linux機能・platform・offline](https://docs.scanbot.io/linux/document-scanner-sdk/introduction/)、[Linux straightening](https://docs.scanbot.io/linux/document-scanner-sdk/document-enhancer/document-straightening/)、[Web straightening](https://docs.scanbot.io/web/document-scanner-sdk/document-enhancer/document-straightening/)。

## OSSが実際に使うもの

| OSS | 確認した実装 | 評価・再利用上の注意 |
| --- | --- | --- |
| **FairScan** | DeepLabV3+＋MobileNetV2の文書専用segmentation。アプリGradleが**v1.2.0**のTFLiteを取得し、`ImageSegmentation.kt`がInterpreterでロード | 2026-02-25公開、**4,921,040bytes**。releaseにGPLv3のLICENSE.txtを同梱。商用禁止ではないが、取り込み・配布方法と既存製品ライセンスの整合を確認する。mask後の四隅抽出も既存処理を評価する |
| **OSS Document Scanner** | `cpp/src/DocumentDetector.cpp`でOpenCVのGaussianBlur、Canny、findContours、approxPolyDP、HoughLinesP等を確認。READMEはOpenCV＋Tesseract依存を説明 | 調べた検出経路は古典CV。アプリ全体にAIが一切ないとまでは断定しない。既存部品を再利用する候補で、自作の検出手法を作る必要はない |
| **OpenScan / alexeygrigorev** | READMEはML Kitによる撮影・crop、ML Kit OCRを明示 | 同名の別scanner／3D scannerとは区別する。FOSSとPlay版の差は版ごとに確認が必要 |
| **jscanify** | OpenCV.jsを使う既成JavaScriptライブラリ | browserの平面文書検出・遠近補正の基準候補。湾曲や色むらまで解決するものとして扱わない |
| **ScanTailor Advanced** | READMEと`src/dewarping/`の既存実装。円筒面モデル・文字列追跡等による幾何補正がある | 書籍scan後処理の参考。NNでなくても候補に含める。GUI／batch操作、入力条件、ライセンスと共通経路への統合費用は別評価 |
| **DocScanner / fh2019ustc** | 研究実装の`inference.py`で**U2NETPによるmask＋DocScannerのbackward map＋torch.grid_sample**を確認 | 製品名を持つ商用scannerとの混同を避ける。公式scriptはCUDA依存、READMEは商用利用について連絡を求める。CPU版を自作して採用する提案にはしない |

出典: [FairScanモデル](https://github.com/pynicolas/fairscan-segmentation-model)、[release](https://github.com/pynicolas/fairscan-segmentation-model/releases/tag/v1.2.0)、[利用側Gradle](https://github.com/pynicolas/FairScan/blob/main/app/build.gradle.kts)、[利用側推論](https://github.com/pynicolas/FairScan/blob/main/app/src/main/java/org/fairscan/app/domain/ImageSegmentation.kt)、[OSS Scanner検出](https://github.com/ossappscollective/OSS-DocumentScanner/blob/main/cpp/src/DocumentDetector.cpp)、[OpenScan](https://github.com/alexeygrigorev/openscan)、[jscanify](https://github.com/puffinsoft/jscanify)、[ScanTailor](https://github.com/4lex4/scantailor-advanced)、[DocScanner推論](https://github.com/fh2019ustc/DocScanner/blob/main/inference.py)。

FairScan release APIが示すTFLiteのSHA-256は`96e14d7e610dd0c27b768b228fbc553b4ec119ebe68f3a3594029a25400691d2`。これは配布元metadataであり、今回ダウンロードした重み自体を検証した値ではない。原著者READMEのvalidation Dice>0.94も、当corpusの検出精度ではない。

## 従来モデルを含む候補表

| 候補 | 補正範囲・特徴 | 配布・edge・今回の判断 |
| --- | --- | --- |
| **MODNet**（AAAI2022） | RGBから人像のalpha matteを推定 | [公式](https://github.com/ZHKKKe/MODNet)はcode/modelsをApache2と明記。人物用途では参考になるが、**文書補正の定番とは確認できない**。文書検出や湾曲補正を無学習で担えるとは期待しない |
| **MobileNetV2** | backbone。Genius Scanは四隅回帰、FairScanはsegmentationに使用 | backboneだけのImageNet重みは文書検出器ではない。本人の自作・独自学習禁止に従い、利用するなら既存の文書用学習済みartifactを選ぶ |
| **U²-Net / U2NETP**（2020） | 汎用salient-object segmentation | [原著者](https://github.com/xuebinqin/U-2-Net)がU2NETP約4.7MBを案内。DocScannerには文書用maskとしての使用実装があるが、汎用checkpointと同じ学習済み重みとは限らない。湾曲補正のモデルではない |
| **BiRefNet**（2024） | 高精細foreground segmentation、ONNX配布あり | [原著者](https://github.com/ZhengPeng7/BiRefNet)の配布を確認。文書専用検出・曲率・照明の比較証拠は今回なし。人物用MODNetより汎用でも、文書処理の第一候補には自動昇格させない |
| **DocAligner / DocsaidLab** | 小型の文書四隅検出 | 既存実測の基準候補。最新9画像では4変形・5fallback。別組織の同名DocAligner論文と区別する。湾曲・影は範囲外 |
| **UVDoc公式Transformers** | 2次元meshを推定し公式後処理でsampling | [公式文書](https://huggingface.co/docs/transformers/en/model_doc/uvdoc)と[後処理source](https://github.com/huggingface/transformers/blob/main/src/transformers/models/uvdoc/image_processing_uvdoc.py)でmeshを確認。map保存の障害を解ける有力経路。ただしPyTorchのRAM／配布量、ONNXとの出力差と前回OCR悪化を再評価 |
| **DewarpNet / LiteRT版** | 既存CNNの非線形map | [配布](https://huggingface.co/litert-community/DewarpNet-LiteRT)は約189MB、コミュニティ変換。upstreamの端末値と当host実測を分離。既存変換がlocal patchを要するため今回も採用しない |
| **DocScanner、DocTr／DocTr++、DocGeoNet** | 歴史的な湾曲補正の比較系統 | [DocScanner](https://github.com/fh2019ustc/DocScanner)、[DocTr](https://github.com/fh2019ustc/DocTr)。今回全系統の重み条件・CPU性能は精査していない。論文比較に登場することを商用アプリ採用事実とは扱わない |
| **DocShadow / FSENet**（ICCV2023） | 高解像度文書の影除去 | [原著者](https://github.com/CXH-Research/DocShadow-SD7K)が重みを配布。SD7K.pth **117,980,483bytes**、[紹介されたONNX実装](https://github.com/fabio-sim/DocShadow-ONNX-TensorRT)の配布済みONNX **120,041,674bytes**。sourceはMIT、checkpoint条件・変換差は実行前確認。2026年に原repoはarchive済み。小型edgeモデルとは言わない |
| **DocRes**（CVPR2024） | Transformer系の幾何・影・外観・blur・二値化 | [公式](https://github.com/ZZZHANG-jx/DocRes)はDocResに加えてMBD重みを必要とする。万能比較候補として残すが、総RAM・CPU・配布条件が未検証。特定商用appが採用する証拠は今回なし |
| **ShaDocNet／BGShadowNet／BEDSR-Net** | 従来の文書影除去比較モデル | [ShaDocNet公式](https://github.com/CXH-Research/ShadocNet)、[FSENet原著論文](https://arxiv.org/abs/2308.14221)。比較対象として把握したが、今回は全checkpointの適格性を確認していない。非公式再実装を原著者配布と混同しない |

## 2025–2026の研究更新

以下は一次資料のabstract／READMEによる候補の分類。全論文のfull text再現や性能順位付けは行っていない。特に「軽量なmodule」を備えることは、モデル全体や画像全経路が軽いことを意味しない。

| 研究・時期 | 内容 | 今回確認できた到達点 |
| --- | --- | --- |
| **ReWarp / Pacific Graphics2026** | Transformerのattentionにmap点間の幾何関係を入れる非線形補正 | [出版社のabstract](https://diglib.cgv.tugraz.at/items/8cf8b915-871b-4020-821b-b870b4e2e870)、[2026年10月学会program](https://pacificgraphics2026.github.io/full-program.html)。公式checkpoint／端末性能は今回未確認。別用途の画像stitching REwarpとは別物 |
| **SyntheticDoc / ECCV2026** | geometry＋illuminationの共同学習 | [公式](https://github.com/tanguymagne/SyntheticDoc)。v0.4でsource MIT／外部重み条件不明を記録。未実行を維持 |
| **BookNet / TMM2026** | 見開き左右ページのcross-page attention | [公式](https://github.com/Liu-Shaokai/BookNet)はmodel source/evalを掲載、code MIT、datasetは非商用研究用と明記。今回READMEで事前学習済みweightの明確な配布手順は確認できず。片面レシートへの一般化は不明 |
| **AADD / AAAI2026** | 軸方向整列を学習・前処理・評価に利用 | [公式](https://github.com/chaoyunwang/AADD)は結果を配布するがfull code/modelsはComing Soon。再現可能な配布済みengineとは数えない |
| **ArbDR / TMM2026** | affine→curl/fold→細部の多段補正 | [公式](https://github.com/chaoyunwang/ArbDR)は結果配布とcode整理中を明記。製品候補への即時導入はできない |
| **D2Dewarp / CVPR2026** | 水平・垂直線を使う幾何表現 | [公式](https://github.com/xiaomore/D2Dewarp)。非商用研究向けCC BY-NC-ND表記を既存調査で確認。現製品の自由な配布候補から除外 |
| **MMDIR / CVPR2026** | 指示付きの混合劣化修復、blur／shadow／watermark／seal | [公式](https://github.com/xiaomore/MMDIR)でbenchmark・予測結果と非商用CC BY-NC-ND表記を確認。文書の印影等を除去するタスクは忠実保存の既定に適さない |
| **TextFlow / 2026掲載資料** | textlineを補助とするflow学習、欠けた境界に対応 | [作者repo](https://github.com/Liu-Shaokai/TextFlow)にCPU上のremote demoと商用利用は要連絡の記述。remote demoがあることはローカル重み配布の証拠ではない。私文書は送らない |
| **TADoc / 2025 preprint・2026出版** | 時間段階を使うprogressive rectification、軽量を主張 | [原著者abstract](https://arxiv.org/abs/2508.06988)。公式の使用可能checkpoint、対象端末・総経路性能は未確認 |
| **DvD / SIGGRAPH Asia2025** | diffusionで**画素ではなく座標map**を生成 | [公式](https://github.com/hanquansanren/DvD)にcodeと4種類のweight取得手順、AGPL3表記。diffusionだから文字を描き直すと一括除外するのは不適切。ただし複数モデル・sampling負荷・licenseと配布条件を評価する必要がありedge-firstの優先度は低い |
| **MatteViT / 2025-12 preprint** | shadow matteと高周波保持を組み合わせるViT | [作者abstract](https://arxiv.org/abs/2512.08789)。OCR／細線を考慮する新比較候補だが、今回公式weight/runtime/edge測定は未確認 |
| **DocShaDif / Information Sciences2026** | 文書shadow除去のconditional diffusion | [出版社abstract](https://www.sciencedirect.com/science/article/abs/pii/S0020025526001970)。配布weight・CPU性能は未確認。DocShaDiffusionという別題の2025 preprintと同一研究とは断定しない |
| **DR-Mamba / ICDAR workshop2026** | 劣化文書の二値化、単一forwardで入力依存の背景抑制 | [作者abstract](https://arxiv.org/abs/2606.22625)。code will be released soon。カラー保存や湾曲補正とは別の責務 |
| **DocIntent / 2026-08 preprint** | VQAに必要な修復toolを選び、比較してrollbackするagent | [作者abstract](https://arxiv.org/abs/2608.29037)。単一edge補正モデルではなく、文書QAの研究。自動判断・原本の忠実保存を同一視しない |

[DRNetの2026年10月arXiv登録](https://arxiv.org/abs/2610.15445)には「Accepted by TCSVT2023」とあるため2026新方式には数えない。同様に、2026に検索indexへ現れた2025学会論文を年だけで最新へ繰り上げない。

## scannerでの比較順序

1. **検出:** 現DocAlignerとFairScanの既存artifact／既成処理を比較する。GPLv3の利用・配布整合とLiteRT導入費用を先に調べる。人物用MODNetを文書用に再学習する案は採らない。
2. **湾曲:** UVDocの公式mesh出力経路を最優先で確認する。既存libraryのsamplingを使い、前回と同一corpusに追加truthを用意し、文字端・数字・細線・欠損とCPU全経路コストを判定する。ONNXで得た性能値を別runtimeへ流用しない。
3. **均一化:** FSENetの原著者配布と紹介済みONNXを照明専用候補として調べ、SyntheticDocは重み条件が明らかになれば追加する。地色・写真・薄字・印影の保持を独立に評価する。
4. **完成品の基準:** 商用SDKを許容する判断があれば、ScanbotのLinux／Web既成経路を品質と統合工数の基準にする。購入・trial登録・外部uploadは今回行わない。

全経路で、写真／scanの共通入口、きれいなscanのno-op、原本保持、preview→明示apply→undo、旧OCR保持を継続する。商用製品が毎回自動補正していても、その既定動作までコピーする理由にはならない。自作warp、独自shadow estimator、独自学習は提案しない。

## 検索・証拠と限界

- 期間: 2026-11-10までに公開された情報。新規研究は2025–2026、従来の採用実例は2010年代以降。画像取得・文書検出・幾何補正・照明補正を含み、OCRのみのモデルは補正モデルから分離した。
- 一般web検索36query、GitHubの原著者／製品repo・release API、メーカー技術blog、arXiv API、CVF／AAAI／Eurographics／出版社の一次資料を組み合わせた。論文collectionは発見用であり、採用や性能の裏付けは原資料へ戻った。
- arXiv API初回は2026提出＋dewarping/shadowのtitle条件で3件、いずれも別分野。unwarping／rectificationと2025提出も含めた再検索は18件。検索式の取りこぼしと2025 preprint→2026出版を補うためpublisher／著者repoを参照した。18件を関連研究18件とは数えない。
- DBLP APIは202応答のchallengeで利用できず、網羅的なdatabase検索として計上しない。ReWarpの直接HTTP取得はTLS hostname mismatch、Adobe helpは403となったため、web取得結果で確認した。TLS検証は無効化していない。
- `research/correction-broad-survey-2026-11/search-log.json`にquery、API検索式、一次資料の取得status／bytes／SHAを保存。生の検索結果／原資料snapshotは`/tmp/scanner-broad-survey-20261110/`に保存した一時資料で、恒久保存や重みの同一性証明ではない。
- source／README／abstractを中心とした単独調査。全候補のfull text、学習data・checkpoint条件・配布互換性・脆弱性を監査したものではなく、systematic review／世界全体のデファクト調査とは呼ばない。非公開製品のmodel名は未確認のまま扱う。
- 今回の調査は新推論、端末／browser試験、checkpoint download、購入・登録、本人文書uploadを含まない。資源・品質に関する当hostの実測はリンク先v0.4.0記録だけであり、新候補には未測定と明記する。

調査手順には[literature-review skill](https://github.com/K-Dense-AI/scientific-agent-skills)を利用した。方法参照: Kassis, T., Agarwal, V., He, Y., Patel, D., & Brueckner, A. M. (2026), [Scientific Agent Skills: A Library of Procedural Knowledge for Research Agents](https://arxiv.org/abs/2609.00065)。著者・題名は今回arXiv metadataで照合した。補正モデル採用や性能の根拠には使用していない。
