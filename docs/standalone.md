# 単一ファイル配布（試作・未リリース）

homelab向けの配布目標は、フロント・API/worker・SANE/airscan・Tesseract・英語/日本語OCRデータを含むLinux amd64実行ファイル1本。ホストへのNode/SANE/Tesseractインストールは不要とする。

初回実行で `$XDG_CACHE_HOME/scanner-kiosk/native/`（未設定ならOSのユーザーキャッシュ）へ同梱ランタイムを展開する。キャッシュは所有ユーザー専用0700、ファイルとリンクはmanifestと照合し、変更を検出した場合は停止する。書込み可能かつ実行可能なキャッシュ領域が必要。単一ファイル配布は、展開不要・単一プロセスという意味ではない。

既存の `serve` / `worker` コマンドと設定を利用する。同じ実行ファイルをそれぞれ起動し、既存の認証・scanner登録・永続state/documents設定を行う。設定手順は既存の運用ドキュメントに従う。同梱ツールのパスは自動設定し、明示した環境変数による上書きは維持する。

検証済みのアプリ実行ファイルと、出自を記録した再配置済みnative runtimeを入力として生成する。

trunk/tagのapp buildでは `python3 scripts/build-web.py --output /absolute/path/scanner --ref "$GITHUB_REF" --sha "$GITHUB_SHA"` を使い、検証したversion/commitを埋め込む。完全なGit checkoutと`origin/main`履歴が必要。任意のversion文字列を直接注入せず、[release channel契約](design/release-channels.md)で許可されたrefとcommitだけを使う。両identity引数を省略したlocal buildは開発版表示のまま。

```sh
python3 scripts/pack-standalone.py --app /absolute/path/scanner --native /absolute/path/native-bundle
CGO_ENABLED=0 GOTOOLCHAIN=go1.27.3 go build -trimpath -buildvcs=false -tags bundled -ldflags='-s -w' -o scanner-onefile-linux-amd64 ./cmd/scanner-standalone
```

CIなどから完成したappを包装する場合は、source treeへ生成payloadを書かない次のcommandを使う。

```sh
python3 scripts/build-standalone.py \
  --app /absolute/path/scanner-with-embedded-frontend \
  --native /absolute/path/native-bundle \
  --native-manifest native/runtime-manifest.json \
  --output-dir /absolute/path/new-output-directory
```

Go 1.27.3を使う既存build環境で実行する。入力appは事前に検証済みのものとし、Go build metadataでも`scanner-kiosk/cmd/scanner`、Linux amd64、CGO無効、`webui` tagを検査する。tagだけでは画面内容の検証を代替しない。manifestはpackager形式のreview済み固定入力で、`app/scanner`以外の全file/link/size/mode/hashを新payloadと完全比較する。余分なfileやOSD辞書の欠落、差替えを許容しない。launcherは一時directoryでbuildし、既存output directoryは上書きしない。新outputには`scanner-onefile-linux-amd64`、`LICENSE`、`manifest.json`、`SHA256SUMS`、`build.json`が入る。`LICENSE`は自作部分の原文で、binaryとともに配布する。checksumはbinaryとLICENSE双方を含み、build receiptにもlicense SHA256を記録する。`build.json`がないpartial outputは配布入力にしない。

このcommandはnative資材を取得せず、実行もしない。compilerへ渡す環境はbuild用キーに限定し、launcher buildではnetwork取得とworkspace/ambient Go flagsを無効化する。native資材とnotice/sourceの出自・security/license受入は別の必須条件。再現性確認やchecksum一致だけでpublish gateを通過したことにはならない。appのVersion/Revisionは事前のapp buildで固定し、包装時には変更しない。

生成payload/manifestはgit対象外。packagerは依存物を取得せず、入力のライセンス・安全性を保証しない。現試作のAlpine native依存には脆弱性gate不合格の検出結果があり、配布許可を意味しない。実機capture、全native動的ロード、TLS関連データ、配布notice/sourceの充足は未検証。日本語/英語の合成原稿による認識検証は実機の認識精度受入と区別する。


既存のOCR検証は `scripts/check-tesseract.py --standalone-binary /absolute/path/scanner-onefile-linux-amd64` を追加して実行できる（他の必須引数は同scriptの `--help` を参照）。workerは空の専用キャッシュで同梱パスを自動設定し、ホストPATHやnativeパス上書きを与えずに起動する。検証用Python/Pillow/pikepdf/Poppler/日本語フォントは配布runtimeの要件ではない。Go試験の全生ログもreceiptと並べて保存する。


通知同梱候補では、展開先の `usr/share/scanner-kiosk/notices/` にGoと選択したnative依存の通知原文・出自・配布ファイル一覧を保持する。`--native` 入力にこのdirectoryを含めることで、通常のpackagerが同じmanifest検証対象として同梱する。初期の通知候補は64ファイルを持ち、現在の固定native入力はsource-build資料を含む80ファイルを持つが、native全依存の義務充足や配布承認を示すものではない。詳細は [配布とライセンスの境界](design/license-distribution.md#experimental-single-file-linux-amd64-package) を参照。

## Kubernetesでの起動


同じimageのENTRYPOINTを用い、APIは `args: [serve]`、workerは `args: [worker]` とする。Helmは各containerに専用の書込み・実行可能な `/tmp` emptyDirと `XDG_CACHE_HOME=/tmp` を用意し、同梱ツールの自動パス設定を維持する。永続state/documentsは別々の既存PVCを使用する。詳細と未受入条件は [deployment](../deploy/README.md) を参照。

配布containerはdistrolessを目標とし、Alpineはnative資材の試作取得元としてのみ使用する。scratch検証成功はdistroless imageの動作検証を代替しない。

専用imageの試作には root `Dockerfile` と `.dockerignore` を、空のbuild contextへコピーし、検証済みの同梱実行ファイルを `scanner` として配置し、同じ包装出力の `LICENSE` も置く。repo全体をcontextへ追加しない。

```sh
docker build --platform=linux/amd64 --network=none -t scanner-kiosk:experimental /path/to/prepared-context
```

公式 `gcr.io/distroless/static-debian13:nonroot` のindex digestを固定し、image内UID/GIDは既存PVC契約に合わせ10001とする。imageの既定CMDはserve、workerには引数workerだけを渡す。root Dockerfileもこの配布方式を既定とする。native更新・security/license gate前の公開や配備は行わない。対応するonefileは現在Linux amd64のみ。

配布物は未リリース。実機・認証・端末・保存先・security/licenseの受入と公開gateは、buildやversion起動の成功とは別に確認する。個別候補の検証履歴は非公開の作業記録で管理する。
