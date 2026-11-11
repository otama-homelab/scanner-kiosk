# 既存dark-line contractのFSENet native補足測定

2026-11-12。`correction-synthetic-v060/analyze.py:15–20`をそのまま切り出し、呼出用indent除去だけで実行した。source dark<128、actual dark<128、固定6線のpixel-survival==1という既存判定を変更していない。新しいscorer・閾値調整・推論・補正/影生成はない。

## 数値

固定rendererのnon-faint table900×700に属するsource暗画素は5,987。公式FSENet nativeの保存済みclean/shadow出力は両方5,987/5,987 retained、survival1.0で、この限定contractには合格した。source identityと既存shadow no-opも5,987/5,987。

| 入力 | 保持dark pixels / reference | 既存contract |
|---|---:|---|
| source reference positive | 5987/5987 | pass |
| existing shadow no-op | 5987/5987 | pass |
| FSENet native table clean | 5987/5987 | pass |
| FSENet native table shadow | 5987/5987 | pass |
| 空の一定gray225 control | 0/5987 | fail |
| 空の一定white255 control | 0/5987 | fail |
| 空の一定black0 control | 5987/5987 | pass |

一定色controlsはPillow標準Image.newによるmetric妥当性試験だけで、評価候補の補正出力や新しいshadow fixtureではない。gray225を誤ってpassした前のwhite-distance>24 scorerとは異なり、このdark128 contractはgray/whiteをrejectする。一方、字形も線もないblack0画像をpassしてしまう。したがって**dark-pixel survival合格をsemantic line existenceやmissing0へ昇格しない**。

## 適用範囲

元sourceは独立した固定renderer table。出力は保持済み公式infer.py save_imageのclip/8bit量子化済みnative PNGで、原寸900×700が一致する。raw model NPYとGT/output PNGのhashを保存した。512へ変換したmaskや新しいresamplingは使っていない。

faint clean/shadowは`unsupported_contract`とし、この数値評価には入れなかった。既存faint recipeは(v+3*255)/4なので黒線0が191になり、元のsource dark<128 maskが空になるため。出力に合わせて128を変えていない。既存のmask/線座標が既知であることと、この古いdarkness contractがfaintにも有効であることは別である。

## 印影GTの調査

別添`renderer-truth-feasibility.md`は直前read-only調査の記録。固定blue/peachはRGB単層・metadata/EXIF無しで、印影ROI/mask/SVG/layer sourceはない。今の固定6pairと既存出力だけから印影semantic GTを標準libraryで自動抽出できるとは言えない。数字の独立renderer転記・線座標は再利用可能である。固定6を保存したまま別のrenderer-known stamp controlsを追加するdata-only経路はあるが、今回は実装/推論せず、missing0の測定契約を独立監査する必要がある。

元ILL-02の全比較完了は主張せず、rootの判定へ引き渡す。photo2_2の独立GTを合成で代用しない。

未実施: faint/512への新contract、新推論、印影GT作成、semantic missing0判定、製品統合・配備。リスク: all-dark fieldが通るrecall的proxyを内容完全保持の証明と誤認しないこと。
