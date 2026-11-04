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


## 空のLinux環境での限定検証

通知同梱候補は、OSライブラリ・shell・package managerを含まないscratch imageで、UID/GID 10001、read-only root、network none、host bind mountなしの条件で検証した。専用cache volumeへの初回展開と別containerでの再利用、eng/jpn列挙、空の専用SANE設定でのversion、引数なしhelperの安全な拒否、合成日英原稿のOCRが成功した。検証imageは配布用imageへの採用ではなく、実行ファイルの依存境界を確認するfixture。

この範囲ではホストへのSANE/Tesseract追加導入は不要。実機capture・全codec/backendの動的ロード・TLS証明書や設定の互換性・実IdP接続までを保証しない。Docker記録上、この検証環境（1 CPU・768 MiB制限）では初回展開から終了まで約92秒、cache再利用のversionは約2.6秒だった。安定時計下の性能benchmarkではなく、起動時間の受入は未完。


## Kubernetesでの起動

同じimageのENTRYPOINTを用い、APIは `args: [serve]`、workerは `args: [worker]` とする。Helmは各containerに専用の書込み・実行可能な `/tmp` emptyDirと `XDG_CACHE_HOME=/tmp` を用意し、同梱ツールの自動パス設定を維持する。永続state/documentsは別々の既存PVCを使用する。詳細と未受入条件は [deployment](../deploy/README.md) を参照。

配布containerはdistrolessを目標とし、Alpineはnative資材の試作取得元としてのみ使用する。scratch検証成功はdistroless imageの動作検証を代替しない。

専用imageの試作には root `Dockerfile` と `.dockerignore` を、空のbuild contextへコピーし、検証済みの同梱実行ファイルを `scanner` として配置し、同じ包装出力の `LICENSE` も置く。repo全体をcontextへ追加しない。

```sh
docker build --platform=linux/amd64 --network=none -t scanner-kiosk:experimental /path/to/prepared-context
```

公式 `gcr.io/distroless/static-debian13:nonroot` のindex digestを固定し、image内UID/GIDは既存PVC契約に合わせ10001とする。imageの既定CMDはserve、workerには引数workerだけを渡す。root Dockerfileもこの配布方式を既定とし、旧Debian候補は `deploy/debian` へ隔離した。native更新・security/license gate前の公開や配備は行わない。対応するonefileは現在Linux amd64のみ。

2026-11-03: 専用distroless試作のbuildと、ENTRYPOINTを維持した引数 `version` の起動を確認した。UID10001・network none・read-only root・512Mi実行可能tmpfs・PATHなしの空cacheで成功し、検証containerと一時contextの削除も確認。これは同image上のAPI/worker/OCR統合や実機capture、脆弱性gate通過の証拠ではない。

続くdistroless専用fixtureでは、product imageの全layer維持と既存実行fileを使用し、空cache展開・再利用、辞書、SANE/helper、airscan初期化、合成日英OCR、PDF framingの8項目が成功した。PDF内容の独立解析、実機capture/TLS、製品API-worker全経路はこの検証に含まない。

最新試作（2026-11-03）は無改変upstream Tesseract5.5.4を同梱した単一ファイル55,685,280bytes（SHA256 c284a3a7…）、distroless image58,059,853bytes。ENTRYPOINT維持・image内実行fileのhash一致と、隔離fixtureの9項目（5.5.4 identityを含む上記native検証）成功。旧APK由来のconfigs/dictionariesは維持し、新コードのsource/build出自を同梱noticeへ追加した。新候補のfresh vulnerability scan、製品API/worker通し・独立PDF内容解析、実機/TLS、license/source offerを含む配布受入はまだ完了していない。

同5.5.4試作は、4製品packageのrace検査166件、2page OCR worker公開と独立PDF7項目（原pixels/描画・page別日英抽出・順序/rotation等）、loopback APIの5asset hash照合にも成功した。基準IPA合成原稿は旧検証とbyte一致。別のNoto合成原稿では旧5.5.2/新5.5.4とも「300」を「③00」と誤認識することを確認しており、合成fixture合格を全書類の精度受入には使わない。実機・TLS・HTTP認可/実IdP・LLM・mobileとsecurity/license/releaseは引き続き未完。

native HTTPSの隔離検証で、同梱airscanが未信頼・ホスト名不一致の証明書でもGETを送ることを確認した。暗号化と接続先認証は別であり、native TLS受入は未通過。Go側の証明書検証は維持している。詳細は [native TLSの境界](design/native-tls.md) を参照。検証は模擬サーバーへのオプション照会のみで、実機やスキャン開始要求は使っていない。

2026-11-03追記: PDF合成時のユーザー設定directory作成を無効化した更新候補は
onefile SHA256 `a883e172ebbf0e379e092deeb65803ca1bccafe149f460eb9a2a5166c21678c3`
（55,685,280bytes）、distroless image
`sha256:eb19afcd383985c25232fc1fa16a9e7f991fd1f6dc4b04a63fe4afdafc5a44c1`
（58,059,853bytes）。旧候補とnative182entryの内容/linkが一致し、空cacheの
ENTRYPOINT/version起動を確認した。実原稿のOCR/PDF component検査と関連4package
raceは成功したが、現sourceの全Goは別のidentity-bound scan runtime testで不合格
（記録nowがstartedより11ms前）。更新候補そのものの全worker/API回帰、fresh security、
license/source提供・配備/全体gateは未完。以前のcandidateの証拠を新binaryの全体合格に
流用しない。詳細は親計画 `stateless-pdf-onefile-20261103/current-evidence.json` と
[PDF設定の境界](design/pdf-text-layer.md#no-user-configuration-directory) を参照。

更新候補a883e172…の追加回帰では、同runtime sourceの全Go race1754 pass/14opt-in skip、
別の実Tesseract/OCR-worker166 pass/0skip・独立PDF7・loopback配信5が成功した。
source snapshot後の変更は固定時計guard testのみで別race10回確認。
同時clock観測はcontainer内でも逆行24回を記録したため、collectorのclock gateは不合格を
保持する。全Go成功をclock安定化・全体配布受入へ読み替えない。今回の成功は実IdP/LLM/
mobile、物理fence/readinessや実機障害・fresh security/license/releaseの合格ではない。
親計画 `stateless-full-regression-20261103/current-evidence.json` に生結果を対応保存。

2026-11-04: 回転原稿のOCRを改善した最新試作はonefile `86168fca…`
（55,685,280bytes）、distroless `57b8ecde…`（58,059,853bytes）。
`auto`が方向検出を使い、`osd.traineddata`を必須・モデル版へ含める。
既存native182entryはbyte/link一致でOSDも既同梱。空cache/ENTRYPOINT/version、
更新binaryのOCR/scope/composition/worker race185件・独立PDF7・配信5に成功。
修正実原稿は本人が概ね読めると確認。全Goは別のcatalog authority caseで不合格、
時計逆行も継続しており、実機排他・全UI/mobile/実provider・fresh security/license/
releaseを含む全体受入ではない。詳細は親計画の `orientation-onefile-20261104/`
および `real-device-ocr-orientation-20261103/` を参照。

2026-11-04 UI/name修正同梱試作: onefile `6eab16eb…`
（55,685,280bytes）、distroless `9e74103d…`（58,059,853bytes）。
現sourceをoffline隔離buildし、native182entryは前候補と一致。新onefile自身の
設定→実HTTP→embedded画面でASCII/日本語/絵文字256コードポイントの機器名を保持、
空文字/257コードポイントを起動時に拒否。5asset配信hash一致、幅320/390/1440と
coarse pointerで検査。image内の同binary照合、UID10001/RO/network none/768MiB/
512MiB exec tmpfs・空cacheからENTRYPOINT `version`成功。
全capture browser/他画面・実phone、新候補OCR-worker、clock/全Go、fresh security/
license/source/release/本番置換の受入は未完。前候補の185 OCR検査を新binary全体の
合格として流用しない。親計画 `ui-name-onefile-20261104/current-evidence.json` を参照。


## 同梱CAとTLS修正候補

現在の未公開候補はMozilla CA 118件を含むPEM bundleを必須資材とし、
app/native双方の`SSL_CERT_FILE`既定値を検証済みcache内へ設定する。
管理者が指定した値は優先し、native側では欠落・空指定からsystem trustへ
暗黙に戻らない。worker configは明示的な空pathを起動前に拒否する。
private CAを使う場合は、適切なPEM fileを明示してプロセスを再起動する。
ホストへSANE/Tesseract/CA更新ツールを追加導入する必要はない。

local forkのTLS検証backend・再build済helper・CA dataと来歴を含むnative191件を
新onefileへ包装した。fresh cacheから実scanimage/parameters wrapperで26TLS caseが
成功し、現在のfrontend5assetは前候補とbyte一致した。distrolessでは同じexe bytesと
UID10001/read-only/768MiB/fresh-cache versionを検証した範囲に限る。
新候補のOCR/実機・全API/worker・container TLS・full security/license・配備/公開は未完。


同じdbef72cf…候補は、元distroless image全layerを保持しstdlib/CGO0の検証probeだけを追加した隔離containerで、実API起動・health live/ready・5asset hash一致・idle `worker --once`・API正常停止を確認した。768MiB/2CPU/128PID、network none/read-only/UID10001/512MiB exec tmpfs、native path上書きなし、OOMなし。probeがserve/workerを起動するため、このrunでimage既定ENTRYPOINT起動を再検証したとはしない（config一致と先のversion smokeは別証拠）。32MiBの検証file上限ではlauncher自身のcacheコピーが失敗したため、fixture-only128MiBへ修正し失敗記録を保持。idle起動のみで、OCR/TLS/実Compose/実機・最低RAM/負荷・security/license/release受入は未完。

同じ候補のdistroless runtime layerを保持したprobe containerで、API起動中に同梱Tesseract wrapperのPSM1/jpn+engを実行。既存同等の合成日英15行を0/90/180/270度に回したTXT4出力で日英3phrase、PDF4出力でexit0/header/sizeを確認した。実native exe/library/3modelは現manifest hash一致。768MiB cgroup/2CPU/128PID/512MiB tmpfs、network none/read-only/UID10001でexit0/OOMなし。これはnative実行確認で、製品job経由のOCR公開、PDFの独立解析・抽出/描画、実原稿精度、最低RAMや全負荷の受入ではない。

最新dbef72cf…/distroless e2dfe991…は、元runtime layersを保持しCGO0/stdlib Go fixtureだけを追加したcontainerで、scanimage/parameters各13件・計26件の直接TLS peer/trust確認に成功。明示CAのIPv4/DNS+SNI/IPv6/scopedIPv6はGET到達、default bundleのprivateCA・不明CA・名前違い・期限切れは拒否、missing/emptyCAはHTTPS拒否かつHTTP継続。接続した22件は実cached backendのmapsをhandshake/GET時点で観測、14GET/非GET0・実scanなし。768MiB/swapなし/2CPU/128PID/RO/network none/UID10001/512MiB tmpfs、exit0/OOMなし。image/binaryは同一bytes、追加native libs/Python/host mountなし。redirect/downgrade方針、実証明書配備、製品job経由・全security/license/release/physical fence受入は未完。古い段落のcontainer TLS未確認は当時のscopeで、この追加検査とは分離する。

最新候補はonefile b85a7dae…/56,258,720bytes、distroless fef2bd85…/58,633,293bytes。backend480e1bbd…のHTTPS downgrade guardと新patch/build情報を同梱し、191nativeの変更はlibrary/patch/build.jsonの3件だけ。Go app/frontend9b26a1c0…とAPK105件は不変、source/lockの対応6tests成功。ENTRYPOINT/空cache/version確認に加え、元runtime layer＋CGO0 Go fixtureだけのcontainerでscanimage/parameters各20件（direct13＋redirect7）、計40件成功。証明書/名前/期限/default trust拒否・HTTPS→HTTP/upgrade後downgrade拒否を実cached backend mapsとGET countsで確認、relative/trusted HTTPS/HTTP互換性維持。768MiB/swapなし/2CPU/128PID/512MiB tmpfs、exit0/OOMなし・非GET0/実scanなし。旧dbefのdirect26証明はdowngrade修正を含まない履歴として保持。新候補の製品job/OCR/独立PDF/実Compose・実機/全security/license/source/release受入は未完。

## キーボード導線同梱候補（2026-11-04）

最新の未公開onefileは `e5363774…`（56,316,064 bytes）、distrolessは
`sha256:636cd516…`。日英の本文スキップリンクを現フロントから再buildして
同梱した。native232件は固定manifestと完全一致し、Go module verifyも成功した。
実onefileの5asset hash一致、capture回帰12項目、日英・幅320/1440・
session/home/historyのキーボード12case、API正常停止を確認済み。
ブラウザーの検証用TLS証明書だけを許容し、製品のTLS検証設定は変更しない。

root Dockerfileから同じonefileのdistroless imageをbuildし、既存base layers、
ENTRYPOINT/CMD/nonroot設定、image内binary SHAを照合した。外部networkなし・
read-only・768MiB/swapなし・空cacheから既定ENTRYPOINTの`version`が成功し、
OOMなし。Go/nativeの実装変更はないが、新candidateでのOCR/全API-worker回帰、
実phone/screenreader、時計/障害試験、fresh security/license/source提供・release
受入は未完。表示は開発版の`0.0.0-dev`/`unreleased`で、previewの公開ではない。
親計画の `keyboard-onefile-20261104/current-evidence.json` を参照。

同じe5363774…候補について、既存`check-web.py`のbuild部分だけを検証済み
onefile配置へ置換した隔離fixtureで、総合ブラウザー検証も完走した。5assetを
実HTTPからbuild hashと照合し、文書操作19・実simple認証9・capture12・
レート制限8・keyboard12を確認。実SQLite/APIで利用者間の分離、PDF/PNG
ダウンロード、削除後のworker file cleanup、guest終了を検証した。
模擬job終端遷移やcapture/keyboardのHTTP fixtureは維持しており、実スキャンの
停止保証、native OCR、実IdP/LLM、phone/screenreaderの受入とは区別する。
検査はbrowserを含む3GiB上限で65.213秒、観測peak707.9MiB・swap0で完了。
この数値は製品の最低RAM要件を示さない。
親計画 `current-onefile-full-browser-20261104/current-evidence.json` を参照。

同じ候補は、その後native OCRを有効にした総合ブラウザーfixtureも完走した。
空cacheへ実展開した233entryのfile/link/mode/size/SHAを検査し、workerへの
native tool/data path上書きなしで既定の同梱Tesseractを使用。実catalog公開から
ブラウザーのja/en OCR受付、daemon処理、PDF downloadと予約解放まで成功した。
pikepdf/Popplerによる独立検査で2pageの日英text・順序・A4寸法・0/90度rotation・
元の可逆画素保持を確認。検査用Pillow/pikepdf/fontはruntime依存ではない。
同時の10ms間隔clock観測は逆行2回（最小step -28.280ms）を記録しており、
この成功はclock gate合格やOCR100反復・物理capture/停止保証を示さない。
親計画 `current-onefile-browser-ocr-20261104/current-evidence.json` を参照。
