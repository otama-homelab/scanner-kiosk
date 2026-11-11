# ILL-02 renderer-known truth feasibility（read-only）

2026-11-12 JST。対象repo `/home/yamato/otama-homelab/scanner-kiosk`。今回はsource/metadataの読取だけで、画像閲覧・推論・新download・Git・台帳変更・数値評価はしていない。

## 結論

**現在の固定6pairを既存出力・標準libraryだけでILL-02全比較完了へ昇格する経路はない。** 数字の独立転記とtableの細線座標はあるが、blue/peachの印影はflattenされたimagegen rasterだけで、独立した印影ROI/mask/構成要素sourceがない。標準libraryによる色分割・円検出・template推定を使っても、それ自体が独立の正解annotationになるわけではない。

一方、追加の**data-only renderer-known control2例**を用意する経路は具体的にある。既存6pairは保存し、新学習・新モデル・自作補正・新しい影生成・LLM手動囲みは不要。ただし標準metricをsemantic missing=0へ結びつける評価契約を先に固定し、元基準に対する独立監査を通す必要がある。現時点で「既存libraryを呼べば自動でsemantic欠落0を評価できる」とは言えない。

## 独立GTの所在

| 内容 | 独立source / 既存GT | 利用可能な比較 | 残る制約 |
|---|---|---|---|
| 数字 | `scripts/eval/ocr/prepare_catalog.py:48,51-52,73-78`、`scripts/fixtures/evaluation/ocr-oracles.json`。mixedの2026/１２３/123、tableの1200円。font003.03 SHA固定、Pillow12.2.0記録 | 既存`AnalyzeOCRPage`と独立転記への文字誤り、literal数字列の完全一致 | OCR誤りはphysical glyph missing数そのものではない。必要なら固定fontと同じrendererのglyph maskをdataとして生成し、再描画SHAの一致確認が先 |
| 細線 | `prepare_catalog.py:67-71`。縦x70/420/800、y70…340、width2。横y70/200/340、x70…800、width2。`correction-synthetic-v060/truth-definition.json`にも記録 | 既存`correction-synthetic-v060/analyze.py:14-20`に同寸法・source dark<128のline pixel survivalがある | この旧blockはnon-faint table向け。faintへ同じ128を流用不可。512は幾何/alias条件が違い、nativeと別契約 |
| 印影2例 | `scripts/fixtures/generated-correction/README.md:3,6-7`はblueの赤star seals、peachの緑leaf sealsを記述するだけ | 参照rasterに対する既存global PSNR/MAE/SSIM。写真/紙色を含む入力比較は済み | 印影専用ROI、mask、個別layer、SVG、正解転記なし。semantic stamp missing数は未確定 |

### PNG/fixture実体の確認

`reference-blue.png`はRGB1055×1491、`reference-peach.png`はRGB1024×1536。両方metadata keys空、EXIF0、frame1。`scripts/fixtures`にはJSON4/MD2/PDF1/PNG12があり、SVG・mask・layer sourceは見つからない。blue/peachのcopy元は`correction-paired-v060/prepare.py:7`で、そのまま既存shadow recipeへ渡す。promptの「2個のseal」は座標付きannotationではない。

### faintの重要な注意

`internal/evaluation/catalog.go:848-850`の既存faint recipeは各channelを `(v+3*255)/4` にする。元の黒線0は191になる。したがって旧dark<128のsource maskはfaintでは空となり、旧scriptの`assert mask.sum()>0`を満たさない。native FSENetのtable/faint_table4出力は全て900×700だが、旧blockを変更なしで適用できるのは**non-faint table clean/shadow2出力だけ**。faint側2出力も同一128で評価できるという説明は撤回する。

faintの字形/座標が独立に既知である点は変わらない。ただし事前にsourceの宣言濃度と背景に基づくcontrast/occupancy契約が必要で、FSENet出力を見て128を上げ下げしてはいけない。

## 現scorerをmissing=0へ使えない理由

`internal/evaluation/correction.go:463,474`はRetainedForeground>0でregionをretainedにする。foregroundは同848-855の固定backgroundRGBとの差合計>24。67-10のsolidgray225 negative controlが、字形/線のない画像でも全6線/全4文字region合格・retention1になった。

`MapSourcePoint`（`internal/correction/transform.go:137-174`）はfullframe/rotation0の幾何座標変換を正しく提供するが、画素contrast・stroke/文字/印影identityを認識するものではない。ROI SSIM/PSNRやforeground残存をsemantic missing0へ名称だけ変えてはいけない。

標準OpenCV/skimage/sklearnのbinary confusion/IoU/SSIM等は使えるが、**予測maskの定義と独立reference maskが必要**。library名が標準であることは、不在のsemantic GTを補完しない。固定rasterから色や円を推定するだけのmaskは、赤い写真領域等と印影を区別する保証がなく、独立annotationとして自動採用不可。

## 具体的な進め方（実行していない）

1. 直ちに既存証拠だけで進めるのは、native non-faint table2出力へ既存dark<128 line-survival blockをそのまま適用する補足測定。数字は既存renderer転記と既存OCR scorer/既存digit-sequence比較を再利用する。これは意味のある追加測定だが、印影を含む全ILL-02完了にはならない。
2. 既存6pairと全hashを固定したまま、別の512×512 renderer-known controlを2例追加する。既存の承認font・Pillow `ImageDraw.text`/`textbbox`・native `ellipse`/`line`等の描画APIだけを使い、例Aを赤ring＋「印」＋数字＋細線、例Bを緑ring＋異なる印文字/数字/細線とする。新しい補正/検出/影アルゴリズムではなくテストデータ定義。seal/数字/strokeごとの独立layer mask・ROI・render params・source SHAを描画時に保存する。
3. 影は既存`evaluation.ResolveFixture pattern/shadow`を無変更で使用する。source layerはshadowやモデル出力から逆推定しない。font/Pillow/source recipeの版を固定し、元rendererを再描画する場合は元raster SHAの再現性も検査する。
4. 新controlの初回モデル評価前に、既存標準binary-mask/contrast/ROI metricの組合せとmaskの取扱いをhash固定する。semantic missing0のoperational definitionとして十分か独立監査する。empty white/solidgray/solidinkなどの既知negative controlsを不合格にでき、元reference/no-opを適切に扱える契約が必要。pixel-level presenceだけの偽陽性を繰り返さない。
5. 契約が認められた場合だけ、既存の**同じ**適格FSENet CPUforkで新data2例を評価する（新モデルの採用/学習は不要）。既存固定6のglobal/資源結果は保持する。全項目を実測できれば、候補が品質不合格でもILL-02の「評価完了」とする余地がある。契約未確定やGT不足をcompleteへ変えない。

「既存のseal正解を抽出して今の出力だけで全比較完了」ではなく、「既存数字/線GTを最大限再利用し、印影GTだけ独立な新dataで補完する」経路である。元photo2_2の独立GT/TRUTH-02/VERIFY-04はこの合成追加で代用できない。
