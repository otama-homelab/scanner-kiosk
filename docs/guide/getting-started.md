# Docker Composeで導入する

[README](../../README.md) · [使い方](usage.md)

## 用意するもの

- Linux amd64のDockerホストとDocker Compose。
- ブラウザから使うHTTPSのURLと、そのホストへのリバースプロキシ。スマートフォンのカメラもHTTPSで利用します。
- 同じホスト上の保存領域を2つ。設定・アカウント用のstateと、期限付き原稿用のdocumentsを分けます。NFS共有をSQLite保存先の代わりには使わないでください。
- スキャンを使う場合は、対応するスキャナーとホストからの接続。ファイル取り込みだけでも使用できます。

## 1. 配布物を展開する

[最新Release](https://github.com/otama-homelab/scanner-kiosk/releases/latest)から`scanner-deployment.tar`と`SHA256SUMS`を取得します。
ダウンロードしたファイルのSHA256を該当する行と照合してから展開します。

```sh
sha256sum scanner-deployment.tar
mkdir scanner-kiosk
tar -xf scanner-deployment.tar -C scanner-kiosk
cd scanner-kiosk
```

ソースから利用する場合はリポジトリを取得して、そのルートで以降の操作を行えます。配布imageの利用だけならコンパイルは不要です。

## 2. ディレクトリと設定を用意する

新規導入の例です。既存領域に対して所有者や権限を一括変更しないでください。

```sh
sudo install -d -m 700 -o 10001 -g 10001 /srv/scanner/state /srv/scanner/documents
```

`scanner.env`を作成します。`SCANNER_IMAGE`には、選んだReleaseのcore imageの**repository digest**を設定します。image設定IDとは別の値です。
URLと保存先は利用する環境に合わせて変更してください。

```dotenv
SCANNER_IMAGE=ghcr.io/otama-homelab/scanner-kiosk@sha256:REPLACE_WITH_RELEASE_CORE_DIGEST
SCANNER_PUBLIC_URL=https://scanner.example.com
SCANNER_STATE_HOST_PATH=/srv/scanner/state
SCANNER_DOCUMENT_HOST_PATH=/srv/scanner/documents
SCANNER_AUTHENTICATE_TYPE=simple
SCANNER_ACQUISITION_ENABLED=false
SCANNER_SCANNERS_JSON=[]
PUID=10001
PGID=10001
```

```sh
chmod 600 scanner.env
docker compose --env-file scanner.env -f deploy/compose.yaml config --quiet
docker compose --env-file scanner.env -f deploy/compose.yaml up -d
```

`serve`と`worker`は同じimage・OSユーザー・保存先を使います。`scanner.env`はComposeが読み込みます。実行ファイル自体に`.env`自動読み込み機能はありません。

## 3. ローカルアカウントを作る

以下はBashで、パスワードを画面や引数に出さず標準入力へ渡す例です。

```bash
read -r -s -p 'Scanner password: ' scanner_password
printf '\n'
printf '%s\n' "$scanner_password" | docker compose --env-file scanner.env -f deploy/compose.yaml run --rm --no-deps -T api user create admin
unset scanner_password
```

このアカウント名は`admin`ですが、名前による特別な管理権限を与える設定ではありません。OIDCを使う場合は[配備設定](../../deploy/README.md)の認証設定を選んでください。

## 4. HTTPSを接続する

ComposeのAPIはホストの`127.0.0.1:8080`へ公開します。同じホスト上のCaddyを使う場合の設定例です。

```caddyfile
scanner.example.com {
    reverse_proxy 127.0.0.1:8080
}
```

実際のDNS・証明書の取得条件に合わせて設定してください。プロキシ自体を別コンテナで動かす場合、そのコンテナの`127.0.0.1`はホストを指しません。ネットワークと転送先を別途設定します。

ブラウザで設定したURLを開き、作成したアカウントでログインします。APIとworkerの起動確認には次を使います。

```sh
docker compose --env-file scanner.env -f deploy/compose.yaml logs --tail=50 api worker
curl --fail https://scanner.example.com/health/ready
```

## 5. 取り込みを有効にする

保存先の権限・容量とworkerの起動を確認したら、`scanner.env`の`SCANNER_ACQUISITION_ENABLED`を`true`へ変更し、同じComposeコマンドで反映します。

LANのBonjour eSCL機器をDocker上で検出する場合は、[discoveryのネットワーク条件](../../deploy/README.md#scanner-discovery)を確認し、次のoverrideを追加します。

```sh
docker compose --env-file scanner.env -f deploy/compose.yaml -f deploy/compose.discovery.yaml up -d
```

最初にアップロードした画像でPDFの保存を確認し、その後、機器を選択してスキャンしてください。
保存先を旧scanservjsの出力領域へ直接向ける使い方はサポートしていません。

## OCRを追加する

通常のPDF保存を確認してから、[配備設定のOCR設定](../../deploy/README.md)に沿って追加します。
Tesseractを使う場合は、同じReleaseのTesseract imageを選びます。LLMを使う場合はVision対応モデル・正確な応答model名・送信先・認証情報と、日本語対応の適切なライセンスのテキストレイヤー用フォントが必要です。

## 更新・バックアップ

受付を停止し、処理中ジョブがない状態で、stateとdocumentsの整合を取って更新してください。
両roleは同じReleaseへ更新します。schema変更を伴う場合、古いimageだけへの差し戻しは安全な復元とは限りません。
通常のバックアップ対象はstateです。documentsは期限付きの私的原稿なので、保存・廃棄方針を分けてください。詳しい条件は[配備設定](../../deploy/README.md)と[設計資料の索引](../README.md)を参照してください。
