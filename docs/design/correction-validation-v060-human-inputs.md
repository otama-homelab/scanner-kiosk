# v0.6.0 人による確認票

生成入力の追加検証後、現時点5/14要件を実証、残る9要件は未完。生成画像の既知正解は別途固定済みで、この票の実写真注釈を実施したことにはならない。この票を作っただけでは受入にならない。対象者・確認日・入力SHA256を記録する。「全部OK」だけでは不足する独立正解の代わりにならない。

## 1. 公開DocUNet6写真の独立注釈（Phase65）

下表の原画像を既存の画像ビューアで等倍表示し、**補正結果を見る前に**注釈を固定する。LLM/OCR/補正モデルの推測は正解として採用しない。原画像は既存ローカル取得物で、このrepoへ転載していない。

| 対象 | 原画像 | 対応する平面参照 | SHA256 |
|---|---|---|---|
| fixture_docunet_crop_1_1_copy | [crop_1_1_copy.png](/tmp/scanner-v040/corpus/crop_1_1_copy.png) | [scan_1.png](/tmp/scanner-v040/corpus/scan_1.png) | `af2a09921d541d146b0eba19e118dd2aae171e785b43a7cb7419386d1315ba3b` |
| fixture_docunet_crop_1_2_copy | [crop_1_2_copy.png](/tmp/scanner-v040/corpus/crop_1_2_copy.png) | [scan_1.png](/tmp/scanner-v040/corpus/scan_1.png) | `9143c7af398dc0f81aaa7554f2df14fd60909722b34f0405279c7c82adb2b796` |
| fixture_docunet_crop_2_1_copy | [crop_2_1_copy.png](/tmp/scanner-v040/corpus/crop_2_1_copy.png) | [scan_2.png](/tmp/scanner-v040/corpus/scan_2.png) | `23e97fd18175337d272af23bb6f03a01f5d7e556f5c59a49655fee3385531eb8` |
| fixture_docunet_crop_2_2_copy | [crop_2_2_copy.png](/tmp/scanner-v040/corpus/crop_2_2_copy.png) | [scan_2.png](/tmp/scanner-v040/corpus/scan_2.png) | `b59f2a03e16a9c5105f72b32998b4e17b83456bfe6eb06b83576768005388d96` |
| fixture_docunet_crop_3_1_copy | [crop_3_1_copy.png](/tmp/scanner-v040/corpus/crop_3_1_copy.png) | [scan_3.png](/tmp/scanner-v040/corpus/scan_3.png) | `91e6ed2ad65c9b053598a0c1b57495397ed9a9ea29396985275729d4d2f84efe` |
| fixture_docunet_crop_3_2_copy | [crop_3_2_copy.png](/tmp/scanner-v040/corpus/crop_3_2_copy.png) | [scan_3.png](/tmp/scanner-v040/corpus/scan_3.png) | `e72175f0a98b6c1039440c86097fcd02d119f2b3fa9a66543ce6c4aa8049500a` |

各画像について以下を記録する。既存correction-oraclesの領域は画素座標{x,y,width,height}、OCR oracleは独立転記を使うが、未確認の値は既存runnerへ登録しない。

- 確認者、確認日、原画像SHA256。
- 原画像左上を(0,0)として、文書四隅を左上→右上→右下→左下の画素座標で記入。曲面の輪郭は追加の境界点を記録する。判断不能な点は未確認にする。
- 文字領域の画素範囲と、その領域の全文転記。数字は順序・桁・小数点・記号を含めて照合する。
- 細線・薄字・表の罫線の領域と、残存確認に使う線・文字。未確認の要素を「欠落0」と判定しない。
- 平面参照との独立対応点。対応不能な領域と理由を明記する。

特に`crop_2_2_copy.png`（photo2_2）は数字と細線の正解が必須。同画像を評価から外さない。参照画像を位置合わせ済みの画素GTとは自動的に扱わない。

注釈固定後に、同じ領域を原画像／旧ONNX／新公式meshで比較する。対応出力は以下。これらは注釈生成元ではない。

- [旧ONNX](/tmp/scanner-v040/uvdoc-fixture_docunet_crop_2_2_copy/repetitions/001-cold/artifacts/artifact_candidate_correction_001)。原画像hashは`b59f2a03e16a9c5105f72b32998b4e17b83456bfe6eb06b83576768005388d96`。
- [新公式mesh・製品資源枠](/tmp/scanner-v060/product-fixture_docunet_crop_2_2_copy/repetitions/001-cold/artifacts/artifact_candidate_correction_001)。原画像hashは`b59f2a03e16a9c5105f72b32998b4e17b83456bfe6eb06b83576768005388d96`。

## 2. 照明用の独立正解（Phase65/67）

必要量は位置合わせ済み影あり／影なし6組以上と、clean control3枚以上。薄字・数字・写真・色紙・印影をそれぞれ2ケース以上含める。各ファイルの出典・利用条件・SHA256・整列確認者・確認日と領域／転記を記録する。単なる同名、似た画像、モデルで影を消した画像は独立正解ではない。自前の影生成や新しい位置合わせアルゴリズムは作らない。公開配布物の小subsetを使う場合も条件とcoverageを確認する。

## 3. 重み条件（Phase67）

[未送信の質問下書き](research/correction-validation-v060/illumination-clarification-unsent.md)あり。FSENet元checkpointと旧DocShadow ONNXの同一性・変換出所、重みの利用／取得／実行／再配布条件が必要。SyntheticDocは公式の通常31×45、高解像度121×177checkpointリンクを特定したが、MITコード表記のみから重み条件を確定しない。外部問い合わせ送信は未実施。

## 4. 本人原稿・端末操作（Phase68）

Canonの原稿は許可済み1回の取り込みを完了。基準画像は[ローカルPNG](/home/yamato/.local/share/scanner-validation/v060/canon-baseline.png)、[ローカルreceipt](/home/yamato/.local/share/scanner-validation/v060/capture.json)。外部送信しない。無補正・無再samplingで保持したdecoder-normalized PNGであり、wire JPEG自体は保持していない。再スキャンは不要。

同じ原稿のiPhone写真があれば、その使用許可済みローカル保存先を指定する。本人操作はPC／iPhoneそれぞれについて、対象入力・browser・日付と、原本保持、preview、明示適用、undo、保存、再OCR時の旧OCR保持を確認する。未実装／不適格なAI候補を製品へ接続して受入を通さない。今ある自動browserテストを本人端末の受入としない。

## 判定と再開

入力提供だけでは完了ではない。注釈と条件を検査・hash固定し、Phase65の不足を埋めてからPhase66/67比較、Phase68本人受入へ進む。製品256MiB依存容量基準は変更しない。未達の場合は実測不合格として記録し、欠測を合格に変えない。milestoneは未完のまま保持する。
