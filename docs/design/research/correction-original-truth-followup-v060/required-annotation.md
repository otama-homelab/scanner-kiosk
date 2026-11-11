# 残る独立注釈の具体的対象

追加の撮影・scan・別datasetは不要。必要な平面参照は取得済みで、対応関係も公式評価実装により確認した。

必須対象はcanonical原写真 `/tmp/scanner-v040/corpus/crop_2_2_copy.png`、SHA256 `b59f2a03e16a9c5105f72b32998b4e17b83456bfe6eb06b83576768005388d96`（1365×2421）。対応参照はraw archive scan `/tmp/scanner-original-truth.vhhyGR/archive_scan_2.png`（944×2702、RGBA）で、元archive memberは `scan/2.png`。hashはexact-original-pair-bindings.jsonに固定。

独立した確認者が補正候補を見ずに、原写真と平面scanだけを使って以下を固定する必要がある。

1. 数字・記号の正確な転記、原写真上のpixel領域、可読／判読不能の区別。OCRやLLM出力を正解にしない。
2. 細線・薄字・表罫線の原写真上の領域／線分と、対応するscan領域。見えない線の存在を推測して補わない。
3. ページ／文字領域の対応点と対応不能領域。scanを原写真へ既に位置合わせ済みと扱わない。
4. 確認者・確認日・両入力hash・注釈revision。scanはalphaが非不透明なので、表示背景／compositing条件も明記する。

他の原写真5枚も同じ独立corner/content注釈条件が残る。全6枚と参照1/2/3のexact pairingはJSONにある。追加UVDoc3文書や生成画像をこの原写真注釈へ置き換えない。

この票は注釈そのものでも受入でもなく、必要な作業対象を具体化したもの。LLM・OCR・補正モデル・推定registrationによる自動補完は未実施。
