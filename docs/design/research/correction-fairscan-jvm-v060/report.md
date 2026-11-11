# 公式FairScan JVM compile実測

2026-11-11。固定公式source `2c5230b12f69f3cee2cc5a970013da20848d1272` を無変更で隔離取得し、公式JVM評価moduleのcompileを実際に試行した。前回の「公式desktop binary待ち」から、実測した準備資源の不足へ論点が進んだ。製品コード・cluster・global Java設定は変更していない。

## 実測結果

公式Temurin JDK `21.0.13+9` と公式wrapper設定のGradle `9.8.2` を取得。各downloadは256MiB未満、JDKとGradleは公式公開digest一致。Adoptium APIは403で、公式GitHub release経路へ切り替えた。sourceZIP/JDK/GradleのURL・bytes・SHA256は [downloads](downloads.json)。source ZIPは固定commit取得で、272source filesの事前SHA256を保持した。

実行commandは [build.json](build.json)、watchdog実装は [build-command.py](build-command.py)、実際のstdout/stderrは [build.log](build.log)。CPU affinity2、Gradle workers2、wall180秒、process-group RSS2GiB、全作業tree1GiBを監視した。rootのheavy semaphoreはその後取得できたが、compileが終わらず評価は実行していない。

**101.57秒でtemp-tree上限を検出しprocess groupを停止、exit -9。** 観測時treeは1,116,906,697 bytesで、1GiBより43,164,873 bytes超過していた。0.5秒samplingによる検出後停止であり、超過なしのhard filesystem quotaとは主張しない。peak process-group RSSは214,081,536 bytes。ソース272filesの元bytesは全件不変。

logは `:evaluation:checkKotlinGradlePluginConfigurationErrors` と `:imageprocessing:checkKotlinGradlePluginConfigurationErrors` まで到達した。SDKなしのconfiguration全体成功やcompile成功は未成立だが、Android SDK不在による失敗とは確認していない。公式Gradle設定のdaemon `-Xmx2048m` は起動環境の`JAVA_TOOL_OPTIONS -Xmx768m`より後に指定されており、heap768MiBのhard enforcementとは扱わない。別の実RSS2GiB監視を用いた。

## 容量原因と終了処理

JDK約361.8MB、Gradle約170.5MB、Gradle user home約583.9MB。元の取得archivesは展開直後に削除済みで、archiveを消して同じ条件でretryする余地はなかった。Gradle dependency/plugin解決・instrumented cache・途中downloadが増加した。[storage](storage.json) は停止直後の内訳。無根拠な同条件retryや上限拡張はしていない。

停止したprocess groupの残存なしを確認し、109,460,896-byteの自分が作成した未完了Gradle downloadだけをhash記録後に削除し、treeを1GiB未満へ戻した。[cleanup](cleanup.json)。source・正常cache・toolchainは保持した。

## 能力判定と再開

`VERIFY-01/02` は未完。今回成立したのは、公式JDK/Gradleの隔離取得・固定source identity・公式module compileの具体的起動と実際の容量停止まで。6synthetic入力を処理したとも四隅精度を測ったとも扱わない。公式mask generatorとJVM四隅stageの差、独立元写真truthの不足は既存追補のとおり残る。

次の最小の再開案は、toolchain準備treeと実行tempを分離して上限の目的を明確にすること、または既存hostの公式JDK/Gradleを再利用して重複配布を減らすこと。今回の固定1GiB全tree上限を黙って実行時tempだけの上限へ変えない。変更がないままcompileを反復しない。公式sourceをpatchしてSDKや依存を削る回避は行わない。

未実施: compile完了、公式quad evaluator、モデル再推論、画像変換・入力配置、APK実行。リスク: JVM起動が成立しても公式HTML/overlayは数値精度評価ではなく、元実写真truthと完成pipelineの検証が別途必要。
