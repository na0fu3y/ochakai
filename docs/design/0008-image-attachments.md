# ochakai 設計ドキュメント 0008: 画像添付

Status: Superseded by [0046](0046-bundle-address-space.md)
Date: 2026-07-17

ナレッジエントリへの画像添付を導入した最初の決定。PostgreSQL のコンテンツ
アドレス blob 行に保存し、画像は「原本ではなく根拠」、正準レイアウトは
`<id>/<name>` とした。この位置づけと正準レイアウトは 0046 では帰属の
**導出規則**として残る。全文は Superseded 直前のコミット
[428fbf0](https://github.com/na0fu3y/ochakai/blob/428fbf070d09d6f6b3a367786ec019211ca6a8c4/docs/design/0008-image-attachments.md)
にある。
