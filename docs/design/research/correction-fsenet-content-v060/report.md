# 現FSENet固定6組の内容保持診断

2026-11-11。保持済み公式FSENet CPUfork512出力を使い、モデル再推論なしで固定6shadow/6cleanの内容を追加評価した。既存のglobal PSNR/MAE/SSIM/資源判定に、既存OCR/foreground-retentionと標準ROI画像metricを追加した。

## 実際に測定した内容

| 内容 | 実測 | 判定の範囲 |
|---|---|---|
| 数字・薄文字 | Tesseract24/24成功、独立renderer4caseを16採点 | Model対no-opは文字誤り改善2/不変2/悪化4。物理的な字形欠落数ではない |
| 独立転記の数字literal | no-op5/16、FSENet4/16のliteralを観測 | 2026/１２３/123/1200の完全UTF8文字列。OCR誤りと画素欠落を同一視しない |
| table/faint_tableの既知2px線 | Model24regionを既存scorerで計測、全regionにforeground有 | 最小pixelretention0.880985。semantic線保持/missing0の証明には不十分 |
| 既知文字/線ROI | 96件のOpenCV PSNR/MAE | 既存MapSourcePointでboxを512へ写しPillow crop、独自metricなし |
| 写真・色紙を含むblue/peach | 公式512 global PSNR/MAE/SSIMをsource/hashへ再束縛 | 両影PSNR悪化、clean MAE/SSIM不合格。局所写真/色/印影maskはない |
| 印影のmissing0 | 未確定 | imagegen固定rasterには独立したseal ROI/maskがない |

## 数字・文字OCR比較

| input | no-op文字誤り | FSENet文字誤り |
|---|---:|---:|
| mixed_shadow | 12 | 12 |
| mixed_clean | 16 | 12 |
| table_shadow | 5 | 8 |
| table_clean | 7 | 8 |
| faint_mixed_shadow | 12 | 16 |
| faint_mixed_clean | 16 | 16 |
| faint_table_shadow | 11 | 8 |
| faint_table_clean | 7 | 8 |

元rendererのoracleと公式font/text/座標を使う。faintは既存の字形/幾何不変の派生。期待boxは既存correction.MapSourcePointのfullframe/rotation0で512へ写し、既存evaluation.AnalyzeOCRPageへ渡した。Tesseract5.5.4、固定jpn+eng辞書、PSM1、2CPU/2threads、各30秒・sampled process PSS2GiB上限。blue/peachの生成文字は独立転記にせずrawOCRだけを保存した。

## foreground scorerをsemantic missing0へ昇格できない実証

既存AnalyzeCorrectionCatalogsはwhiteとの差分合計>24をforegroundとし、region内に1pixel以上あればregion retainedとする。照明モデルのcolorcast/元の影が残るだけでもforegroundになり得る。実際に検証用の空のsolidgray225画像（字形も線もない）を入れると、tableの既知6線と4文字regionがすべてpassed、retention1になった。このnegative controlはmetric妥当性の試験であり、補正アルゴリズムを作ったものではない。

したがって、Modelの24line regionsがpresence合格しても「細線欠落0」とは書かない。pixelretention低下を「線が丸ごと消失」と言い換えることもしない。色紙全体をwhite背景foregroundへ入れて印影保持を判定することも避けた。

## pixel表現と残る独立正解

今回のOCR/ROI/retentionは公式infer.pyのsave_imageによるclip/8bit量子化済みPNGを測る。以前のglobal rawfloat PSNR/SSIMとは表現が異なる。no-op512は同じ公式DataLoaderVal/Albumentationsをモデル無しで通したtensorを同じsave_imageでPNG化し、resizeだけの効果を揃えた。原寸900x700のregionから512への幾何写像は評価用既知変換であり、モデルがwarpしたという意味ではない。

固定blue/peachは写真・紙色・印影textureを保持すべき既知rasterとして有効だが、生成READMEは要素を記述するだけで印影や写真の独立座標/maskを供給しない。LLMで見て囲ったboxを独立正解として追加していない。seal-specific missing数はunknownのまま。

追加データだけの最小案は、固定6組を入れ替えず、既存の承認済みfont/renderer textとPillow標準ellipse/text primitivesで赤いring＋「印」を描く別controlを加え、描画定義からROI/maskを固定すること。影生成は既存recipeのみを使う。今回は作成/推論していない。ROI画像metricやliteral認識は標準実装で測れるが、それをsemantic missing0と呼べるかは元基準に照らした独立監査が必要で、既存foreground scorerだけでは不足する。

元ILL-02はglobal/資源・今回の内容診断で実測不合格が増えたが、印影とsemantic欠落のGT/測定契約が未確定なので、自動で比較完了へ昇格しない。根拠と不明点を独立criteria auditへ渡す。

未実施: 新モデル推論、新seal fixture、独自欠落detector/metric、実写真・本人受入・配備。リスク: OCRの未認識やforeground残存をphysical glyph/seal missing数と取り違えると誤ったmissing0になる。
