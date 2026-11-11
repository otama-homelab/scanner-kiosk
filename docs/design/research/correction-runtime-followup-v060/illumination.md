# 照明モデルの利用条件・変換経路の追加確認

2026-11-11。取得した一次資料16件は[HTTP証拠](illumination-sources.json)に本文・URL・SHA256付きで保存。重み取得、推論、変換、外部連絡は実施していない。

## 結論

「重み専用の許諾文がないから必ず権利者回答待ち」とする従来の一律判定は強すぎる。MITは使用・変更・販売・再配布を認め、専用モデルカードの存在を条件にしていない。著作権表示と許諾文の保持が必要。今回確認したLICENSE・README・release本文には、非商用限定や重み除外の明示は見つからない。同じMIT repositoryが直接配布し、READMEが推論用として案内するartifactについては、その配布文脈を含めた通常のMIT適用解釈と、実際の技術的同一性の未証明を分けて記録するべきである。独立した権利保証が得られたとまでは主張しない。

| 対象 | 一次資料から確認できたこと | 実際に残る技術的問題 |
|---|---|---|
| FSENet公式 | `CXH-Research/DocShadow-SD7K`はMIT、同じrepositoryの`Weights` releaseが`Pretrained weights of FSENet`としてSD7K/Jung/Kligler.pthを配布。READMEがそのreleaseと`python infer.py`を直接案内。重み除外なし。 | artifact bytes/hash未取得。現モデルとcheckpointのload適合、CPU資源、品質は未測定。 |
| 作者紹介DocShadow ONNX | 元作者READMEがfabio-sim実装を容易な実装として紹介。派生repoもMIT、releaseにONNX opset12・export環境を記載し、同じrepoのdownload.shが3ファイルを指定。 | 2023-08-31 ONNXが2023-12-17のFSENet releaseと同一だという証明はない。変換元URL自体が異なる。 |
| SyntheticDoc | MIT repositoryの`training/model/README.md`がETH公式hostの2checkpointを指定。公式`training/inference.py`にCPU指定・shadow処理を含む完成した推論CLIがある。 | GitHub外のcheckpointにも同じ条件が適用される明示範囲はrepo内artifactより弱い。artifact identityとロード互換性、性能、品質未測定。ONNX変換はこの既存PyTorch経路には不要。 |

## ONNXがFSENetと同じだと扱えない具体的証拠

元repo `b9395ed333c916051d7ae58be212ac62119b61f5` のREADMEは、論文版からGDFNへ変更し性能が変わったと記載する。派生repo `ec926bf36b4ac0778f836a2d34e27021447df27c` の`export.py`は`DocShadow.models.DocShadow`を生成し、`DocShadow/utils/utils.py`の別Google Drive IDからcheckpointをロードする。現FSENetのGitHub release URLは使わない。`torch.onnx.export`とopset12は存在するが、元/ONNX出力を比較するassertや誤差報告はない。releaseの「exported and tested」は数値parityの証拠ではない。

したがって既存ONNXを評価する場合は、**作者紹介の旧DocShadowという別候補**として固定する。現FSENet合格には流用しない。公式既存exporterはあるので「exporterがない」も不正確だが、そのexporterで現在のFSENet重みを変換できるとは言えない。

## 最小の進め方

1. まず公式FSENetの**元PyTorch経路**を候補として扱う。元モデルで評価する限りONNX parityは前提ではない。配布元・MIT・release・artifact identityを固定し、既存隔離CPU環境で公式`infer.py`と既存paired metricを使用する計画に分岐する。独自補正や新exporterは不要。現課題の承認範囲では重みを取得していないため、本報告は実行合格ではない。
2. 旧DocShadow ONNXを選ぶなら、元checkpointは派生repoの既存`load_checkpoint`が指すものと対応させる。現FSENetとは別名・別revisionで保持する。配布ONNXだけのCPU動作測定はできても、数値parity未証明を隠さない。新しいparityハーネスやexporterを無断で作らない。
3. SyntheticDocは公式CLI `python training/inference.py --checkpoint … --input-dir … --output-dir … --device cpu` があるため、CPU処理を独自実装する必要はない。標準PyTorch checkpoint読み込みは保存内容によってpickleを含み得る。既存隔離と公式提供元確認を維持する。モデル固有の利用範囲が必要なら、このGitHub外配布範囲だけを質問対象として絞る。

元のILL-01/02は未実測なので完了にしない。ただし「全候補が権利者回答まで取得・実行不能」という一括停止ではなく、FSENetの公式元モデル評価と、旧DocShadowの別候補評価を計画できる。製品採用・再配布・品質合格は、それぞれ別gateのまま。

未実施: 重み取得、モデル実行、変換、数値parity測定、権利者への連絡。
リスク: MITの配布文脈を技術的同一性や品質の証明に読み替えないこと。旧ONNXと現FSENetを混同すると比較結果の意味が変わる。
