# Paperless-ngxで使う

[使い方](usage.md) · [導入ガイド](getting-started.md)

## PDFの検索性

Paperlessの全文検索では、PDFから抽出したテキストが重要です。OCR文字の位置ずれは文字選択・画像上の強調に影響し、文字そのものの欠落・誤認識は検索漏れにつながります。

ScannerのLLM補足もPDFの文字情報です。原文にはない説明語でもヒットするので、内容を探す手掛かりになる一方、曖昧な検索結果や自動分類への影響もあり得ます。`LLM supplement (interpretation, not OCR)`で認識原文と区別します。PDFだけでは、検索エンジンに原文と補足を別フィールドとして渡すことはできません。

## Paperless側のOCR設定

[公式設定資料（v3.2.1）](https://github.com/paperless-ngx/paperless-ngx/blob/v3.2.1/docs/configuration.md#PAPERLESS_OCR_MODE)では、以下の動作です。

- `auto`: `pdftotext`で十分な既存テキストが見つかれば再OCRを省略します。ScannerのOCR結果を利用する場合の基本となる設定です。
- `off`: OCRを起動せず、PDFのテキスト抽出のみ行います。画像だけの文書には文字が入らないため、全体の運用を確認して選びます。
- `redo` / `force`: 文字レイヤーの置換や画像化・再OCRを行います。原画像に見えないLLM補足は失われる可能性があります。

保存用PDF/Aの生成設定とOCRモードは別です。管理画面の設定が環境変数に優先する場合もあるため、実際の設定を確認してください。こちらの機能追加でPaperlessの設定を自動変更することはありません。

PopplerとPDF.jsによる抽出、原稿画像と認識文字位置の保持をローカルで検証しています。実際のPaperless consumerでの取り込み・PDF/A変換・検索結果は別の受入です。

## アプリからの直接送信

既存の直接送信アダプターはPaperless-ngx **v3.4.2/API9**向けの固定契約です。他のversionを互換とみなして送信しません。たとえばv3.2.1環境への直接連携は、version検査・task応答・checksum確認まで別途検証が必要です。

直接送信が未構成・未受入の場合は、まずScannerからPDFを保存してPaperless側へ取り込む運用で確認してください。アプリによる送信は、利用者が対象・送信先を確認して明示的に実行する操作です。通常のOCRやPDF保存が、自動的にPaperlessへ送信することはありません。
