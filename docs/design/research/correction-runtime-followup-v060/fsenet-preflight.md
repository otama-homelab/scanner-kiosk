# FSENet公式元モデルのCPU preflight

2026-11-11。公式revision `b9395ed333c916051d7ae58be212ac62119b61f5` の11テキストと既存隔離環境のdistribution metadataを確認。[証拠](fsenet-preflight.json)。重み取得やモデル実行を行う前に、**公式コードを変更せずCPU実行する経路は成立しない**と判定した。

## 最優先の阻害点

公式`utils/utils.py`の`load_checkpoint`は次の固定callbackを持つ。

```python
torch.load(weights, map_location=lambda storage, loc: storage.cuda(0))
```

`infer.py`も`single.py`も`Accelerator.prepare`より前にこの関数を呼ぶ。したがって`Accelerator()`がCPUを選べることは十分ではない。既存の`torch 2.12.1+cpu`環境では、非空tensor storageをロードするとCUDA device 0を要求する。CUDA非使用の現制約を守るには、CPU対応済みの公式改訂/別配布経路が必要。monkey patchや独自loaderを用意すれば「公式のまま実行」とは言えず、本確認では実施しない。近年のtorchの`weights_only`既定とcheckpoint内容の互換性も未確認であり、pickle実行による切り抜けは行わない。

## 既存環境とデータ契約

`/tmp/scanner-v060/cpu-venv`の`importlib.metadata`のみ使用し、torch/modelはimportしなかった。Torch/torchvision/Pillow/NumPy/tqdm/PyYAMLは存在するが、実際の`infer.py` import経路に必要な**accelerate、torchmetrics、yacs、einops、albumentationsがない**。requirements全体はtorch2.0.0+cu118/torchvision0.15.1+cu118等の旧CUDA環境を固定するため、そのまま導入する方法は現在のCPU境界に合わない。他のtraining用依存を全て推論必須と誤認しない。

公式`config.yml`を設定するだけの部分と、コード改変が必要な部分を分離する。`TRAINING.VAL_DIR`配下に`input/`と`target/`、`TESTING.WEIGHT`にcheckpointを置く契約。画像はRGB/[0,1]。input/targetは別々にsortされ、stem照合を行わないので、同じ正解ペアの名前と順序が事前一致する必要がある。`TRAINING.ORI=True`が既定で実解像度のまま、Falseなら512x512へ既存resizeを使う。batch1だがnum_workers8はコード固定で、現在のbounded CPU実行方針にそのまま適合する保証はない。結果はcwdの`result/`へ保存され、同じ場所を他検証と共有しない。既存paired fixtureへの接続と30秒/2GiB等の資源評価は実行していない。

## 判定

MIT配布の読み直しにより「必ず権利者回答が先」という一律停止理由は修正できる。しかし、元FSENet公式実装について今回特定した停止理由は**CUDA固定checkpoint loader**と**未充足の推論依存**である。重みを取得してもこの原因は解消しないため、ダウンロードは省略した。独自アルゴリズム・source patch・GPU変更なしで進めるなら、CPU対応した公式改訂、または元FSENetとは区別した作者紹介DocShadow ONNXが次の候補になる。現FSENet品質やILL-01/02完了を主張しない。

未実施: 依存導入、重み取得/deserialize、推論、資源/品質/parity測定。
リスク: CPU選択可能なAcceleratorの存在だけで公式FSENetがCPU対応と判断すると、checkpointロード時点で停止する。
