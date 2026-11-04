# Homelab向けの軽量要件チェック

2026-11-04時点のsource/config確認。実NAS・CSI・rootless Docker・本番クラスタの受入を示すものではない。配布候補は未release。

| 観点 | 現状 | 残件・運用条件 |
|---|---|---|
| 導入 | frontend/API/worker/SANE/OCRを単一Linux amd64 binaryへ同梱。distroless、Compose、標準Kubernetes例あり | 公開release/registryは未完。URL、認証、scanner IP、保存先の設定は必要 |
| UID/GID | image・Compose・Helmの既定は10001:10001。Helm runtimeIdentity.uid/gid、Compose PUID/PGIDで両roleの実行identityを設定。private directory0700/file0600 | 初回volume準備と実storageの検証が残る。PUID/PGIDはComposeのuser指定へ展開され、アプリ自身のユーザー切替機能ではない |
| 所有権・NAS ACL | キャッシュ/GC等は所有UIDやprivate modeを検査。既存mountのownerはimage側USER設定だけでは変わらない | GIDだけ合わせても0600のファイルは共有できない。API/worker/管理CLIの実行UIDを揃える。NASの強制ACL・ID変換を実ボリュームで確認 |
| rootless/user namespace | 現検証はUID10001のnonroot実行 | rootless Docker等ではcontainer IDとhost IDの対応を確認。host側を一律10001へchownする前にmappingを確認する。rootlessの実受入は未実施 |
| 保存先 | stateとdocumentsを分離し、両方にSQLite DBを置く。1 replica/共有filesystem | NFS上のSQLiteは初期設計の対象外。SMBも未受入。NAS上のiSCSI等を単一writerのblock volume＋対応filesystemで使う場合は別途CSI/locking/fsync等を受入する。documentsだけ共有NASへ移す構成も現状はDBを含む |
| バックアップ・復旧 | 設定/認証の論理export/restoreを実装。復元時にsession等を無効化。書類の既定保持24h | documents全体をsnapshot/replication/offsiteから除外。stateの復旧とは分ける。既存scanservjs領域のNAS再帰snapshotをそのまま流用しない |
| LAN・認証 | HTTPSのpublic originとSecure cookie。simple認証とOIDC。local OCRは外部API不要 | LAN内でもDNS/HTTPS proxyと証明書を用意する。実IdP受入は未完。LLM利用は任意で、外部送信先/credential/課金はその場合に確認 |
| メモリ・一時領域 | 配備例のlimitはAPI512Mi＋worker1536Mi。各/tmpは512Mi。native cacheに実行可能領域が必要 | 768Miでの限定fixture成功は全負荷の最低RAM要件ではない。Compose tmpfsはRAMを使う。Helm emptyDirはmedium未指定で、nodeのephemeral storage/実行可否も確認 |
| スキャナー接続 | 明示登録のsane-airscan。USB device mountや自動発見を必須にしない | scanner VLAN/port到達性、printing維持、他LAN端末/legacy writerとの排他、physical readinessの受入が残る。新規acquisitionは既定disabled |
| 更新・日常運用 | immutable image digest、Recreate、lease/停止確認、quota/TTL/cleanup、ログ機密保護。main preview/tag release方針 | 実upgrade/rollback、容量不足/停電/CSI障害、時計安定化、hosted CI/publisher、監視への接続は未完。health probeだけでは機器のreadyを証明しない |

## UID/GIDの要点

最初に専用の実行identityを決め、API・worker・`user create`等の管理CLI・保存先の所有UIDを揃える。GIDも運用上統一するが、private0600/0700の共有は同一UIDが前提となる。バックアップ/復旧時にも数字のidentityと権限を保持する。

現chartはfsGroupを付けず、事前にprivate storeを準備する設計。fsGroupや共有group向けのchmodだけで直すとprivate permission検査と衝突する。rootless/CSI/NASの実際のID mappingを先に確かめる。

軽量レビュー後にUID/GID設定を追加した。初回volumeの準備/確認手順は配備例へ追記済み。現在の優先残件は、実storageでの手順受入、対応可否とbackup除外の確認。既存データの一括chownや権限緩和は、このレビューでは実施していない。

参照: [配備例](../../deploy/README.md)、[storage契約](state-storage.md)、[運用・release](operations-release.md)、[physical fencing](physical-writer-fencing.md)。
