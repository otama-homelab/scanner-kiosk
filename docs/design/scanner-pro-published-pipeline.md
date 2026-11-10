# Scanner Pro：公式公開の処理方式

確認日2026-11-10。Readdle公式説明を参照。実機比較、内部実装の検査、checkpoint取得はしていない。

- [境界検出の技術記事](https://readdle.com/blog/scanner-pro-border-detection)：MobileNet-based keypoints → capture-time segmentation refinement; IMU/Kalman tracking. Article describes experimental rollout, not independent performance verification.
- [従来の影・色補正](https://readdle.com/blog/scanner-pro-shadow)：illumination-background estimation, Metal, coarse-to-fine processing; historical implementation, not proof of the2026color-mode architecture.
- [2026年v8.34の更新](https://readdle.com/blog/why-scanner-pro-scans-look-better-than-ever)：neural color enhancement with global/local/patch stages, on-device ANE. Specific architecture, model weights and OSS equivalence are not identified in this article.

Scanner-kioskへの示唆：検出・幾何・照明を分けて既成実装を選定する。この記事を理由に自作補正／学習を追加しない。MobileNetという分類だけでDocAlignerやFairScanと同一モデルとは扱わず、紹介された市販appの品質と今回のOSSモデル測定を混同しない。Scanner Pro自体の導入／登録／購入／原稿送信は行っていない。
