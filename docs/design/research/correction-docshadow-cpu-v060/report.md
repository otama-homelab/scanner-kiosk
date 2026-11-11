# 旧DocShadow ONNX CPU実測

2026-11-11。現在のFSENetとは別候補として、元作者が紹介するfabio-sim版の旧DocShadow SD7Kを実行した。公式v1.0.0 ONNXは120,041,674 bytes、SHA256 `8c09b9320a0fb3c53806cdf9cb8410b3706c77caed81f4ec1cb0bf2cbd14b049`。MITと公式preprocess/runを無変更で使用。薄いglueだけで既存CPU ORT1.23.2のSessionOptionsをintra2/inter1にし、2core affinity・timeout・RSS監視を設定した。新規依存・補正アルゴリズム・exporterはない。

## 実測

既存の合成影6枚＋同じclean6枚を公式CLIと同じ512x512入力へresizeし、12プロセスで各3forward、全36回成功。session load0.976–1.166秒、forward0.527–0.697秒、peak RSS476–511MB（10進）。追加で元900x700をresizeなしで3forwardし全成功、1.560–1.610秒、714.5MB。合計39forward、13プロセス。CPUExecutionProviderのみ使用。現構成ではtimeout/RSS killなし。import時のORT GPU discovery警告はstderrに残し、GPU providerを実行したとは扱わない。

512診断の影PSNR改善は6件中4件で+1dB基準を満たしたが、青色紙/桃色紙はそれぞれ-3.384dB/-2.315dBと悪化。clean入力のchannel MAEは全6件が1/255基準を超過（0.0341–0.0785）。同じ512→原寸法のresize-only baselineも別に保存し、モデルとresizeの両方の影響を明記した。青色/桃色の影はresize-only baseline比でも悪化。したがって、この512構成は不合格・apply不可。元解像度品質全体や現FSENetの不合格を意味しない。

OpenCV既存PSNR/norm APIを使用し独自metric実装は作っていない。標準SSIM実装が既存環境にないためSSIMは未測定。私文書・実写真・OCR品質・変換元checkpoint parity・本人端末受入は対象外。native.stdoutのscope文字列には共通glueの512診断表記が残るが、native固有のmodel_size900x700とoutput_shape700x900が実入力を示す。summaryのnative注記を優先する。

## 証拠と再現

runs.json / native-attempt.jsonに実コマンド・時間・終了状態、各stdout/stderrに生記録、quality.jsonに原寸no-opとresize-only baseline、全raw NPYのhashを記録。model/output bytesは/tmp/scanner-docshadow-v060に保持しsourceへ追加していない。公式sourceとglueをこの証拠directoryへ保存しsummary.jsonでhash固定した。

未実施: SSIM・全画像のnative品質・OCR・ONNX/PyTorch parity・製品統合・配備。リスク: 合成512診断の一部改善を写真全般やclean文書への安全な自動適用と読み替えないこと。
