# 元DocUNet6写真と平面scanの独立参照追補

2026-11-11。元のDocUNet9入力（写真6枚＋平面scan3枚）を維持したまま、公式の対応付けと不足する正解の種類を再確認した。追加UVDoc3文書で置き換えていない。

**必須photo2_2に対応する独立したflatbed scanは既に取得されている。今回、公式UVDocのDocUNet評価実装から2_2→scan2の対応を明示的に確認した。** 一方、scan画像があることは元写真上の数字・細線の転記／座標正解があることとは異なる。

## 新しく確認した一次資料

- [DocUNet publisher page](https://www3.cs.stonybrook.edu/~cvl/docunet.html) は、benchmarkをoriginal photos／centered crops／flatbed scansの3部分として公開し、公式evaluation codeを案内している。
- 公式 `eval.zip` を実取得。1,347,652 bytes、SHA256 `5511239ebfdefcbf2066c796759d206cd64d2ceae21add416fcf688e5920c25c`。公式リンクはHTTP。HTTPS版は証明書検証で停止し、TLS検証は無効化していない。HTTP配布に認証済みpublisher checksumはないという制限を記録した。
- 公式UVDoc repositoryの固定revision `4c9b82b537057aff2526e6dd118a847cdd072e82` にある `docUnet_pred.py` は `2_2 copy.png` のcopy suffixを除去して `2_2.png` として出力する。
- 同revisionの `eval/eval_code/evalScript.m` はdocument `k=1..65` の `scan/k.png` とcapture `m=1..2` の `k_m.png` を比較する。公式 `ocr_eval.py` も入力`k_m copy.png`とscan`k.png`の同じ対応を使用する。

従って今回のpairingは単なるファイル名の推測ではなく、**公式UVDocが公開するDocUNet benchmark consumerの実装に基づく対応**。DocUNet作者の独立pair manifestそのものを新発見したとは主張しない。source URLs・固定revision・SHA256・対応するsource行は [official evidence](official-evidence.json)。新規取得量は約1.36MBで、archive全体の再downloadはしていない。

## 厳密な原本identityの訂正

最初の「archive内PNG bytesがcanonical入力hashと同じ」というassertは実際に失敗した。[初回失敗](initial-failed-byte-assumption.json)。調べると、現在のcanonical9入力は元archiveのPNGと**encoded bytesが全9件で異なる**。

全9件について確認したことは以下。

- canonical SHA256は以前のmanifestと全件一致し、今回一切変更していない。
- 元archive SHA256も以前の取得記録と一致。cropは294,741,912 bytes、scanは436,458,210 bytesだが、これは以前からあるローカル取得物を読んだもので、新しいdownload上限を超えて取得していない。
- 元archiveとcanonicalのdimensions・decoded RGB pixelsは全9件で完全一致。canonicalはRGB projection＋PNG再encodingに対応する派生物で、元archiveそのもののbyte identityではない。
- 写真6枚はarchive／canonicalともRGB、元orientation1・canonicalではmetadataなし。scan3枚は元RGBA→canonical RGBで、alphaが落ちている。

| scan | 元alpha範囲 | 完全不透明pixelの割合 |
|---|---|---:|
| 1 | 0〜255 | 98.554% |
| 2 | 0〜255 | 99.508% |
| 3 | 0〜255 | 99.270% |

alphaは全3件で非不透明部分がある。従って **RGB pixel一致だけでRGBAの描画結果が視覚的に同じとは言わない**。表示背景／alpha compositingを記録せず、RGB-dropalpha派生物を元scanの完全な描画同一物と扱わない。元RGBA scan1/2/3をexact archive bytesとして別のprivate/tmpへ展開し、それぞれのhashを保持した。imageの再encodingや既存入力の書換えは今回行っていない。

以前の「archive memberを取り出せばcanonical hashそのものになる」という復旧説明には訂正が必要。復旧時には元archive hash・member hashとcanonical派生hashを別々に検証し、decoded RGB／alpha／metadataの範囲を区別する。[exact bindings](exact-original-pair-bindings.json) に全9件の両方のidentity、alpha、orientation、metadata keysを保存した。

## 必須photo2_2の対応

- canonical原写真: `/tmp/scanner-v040/corpus/crop_2_2_copy.png`、1365×2421、SHA256 `b59f2a03e16a9c5105f72b32998b4e17b83456bfe6eb06b83576768005388d96`。
- 元archive member: `crop/2_2 copy.png`、SHA256 `63081e3dc3d76ac7425e3f8923b8a782dab28315d8ca26f304b4c4cd4d6bdab1`。decoded RGBはcanonicalと一致。
- 対応するflatbed scan: `scan/2.png`、944×2702、RGBA、SHA256 `a0c79a3c801cb55ccf5ecab8d355f4064960830d0188ba4b8889d902acacbb13`。
- raw参照の保存先: `/tmp/scanner-original-truth.vhhyGR/archive_scan_2.png`。以前のcanonical `/tmp/scanner-v040/corpus/scan_2.png` は別hashのRGB派生物として維持。

1_1／1_2→scan1、2_1／2_2→scan2、3_1／3_2→scan3の全6pairを同じように検証した。新しい撮影・物理scanや別画像の調達は不要。

## 供給されていない正解と既存評価経路

crop archiveはPNG130件、scan archiveはPNG65件で、どちらにもcoordinate grid・数字transcription・細線region annotation等の非画像memberはなかった。flatbed scanは独立した内容参照になるが、写真と同じ座標へ整列されたpixel GTではない。

公式DocUNet `evalUnwarp(A,ref)` はMatlab Image Processing ToolboxのMS-SSIMとSIFTflow推定correspondenceを使うLDを評価する。公式UVDocのDocUNet評価もscanと予測をresizeしてSIFTflowを**計算**する。これは配布済みの独立dense deformation truthではなく、標準的な既存registrationを使っても推定対応が人手／dataset提供の数字・細線GTへ変わるわけではない。

現在PATHにMatlab／Octaveはない。新取得した公式eval.zipのnative MEXはMac／Windows向けで、Linux用MEXは含まれていない。source portや独自alignmentを作って迂回していない。公式OCR評価はOCR(scan)をGT_textと呼ぶが、ここでは**OCR出力を独立文字truthとして採用しない**。そのsourceはpair mappingの確認だけに使い、実行していない。[既存標準経路](standard-evaluation-path.json)。

残る具体的な作業は、原写真と対応RGBA scanだけを見た独立確認者による、必須photo2_2の数字転記・原写真上のpixel領域・細線region／対応点・判読不能箇所・確認者／日付／source hashの固定。他5写真の独立corner/content annotationも残る。[正確な注釈対象](required-annotation.md)。LLM・OCR・補正出力から注釈を補わない。

paired flat referenceの取得／対応付け不足は解消し、sourceのencoded identityに関する誤認も訂正した。しかしTRUTH-02／VERIFY-04の内容／座標正解はまだ未完で、要件をcheckedにはしない。

未実施: 独立の数字・細線注釈、Matlab/SIFTflow実行、alpha描画同一性の判定、モデル再実行。リスク: flatbed scanや推定registrationだけで、必須photo2_2の内容保持を合格にできない。
