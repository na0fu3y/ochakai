# ochakai 設計ドキュメント 0013: 添付ファイルの一般化と GCS 一本化

Status: Superseded by [0046](0046-bundle-address-space.md)
Date: 2026-07-18

添付を画像限定から「Claude が読めて gemini-embedding-2 が埋め込める形式」
(画像・PDF・text/plain)に一般化し、バイト列の置き場を GCS 一本にした
(bytea との二重化は廃止)。sniff によるメディアタイプ判定と GCS 未設定なら
markdown-only で動くことは 0046 も維持し、SVG / HTML の書き込み拒否だけが
配信側の防御に置き換わる。全文は Superseded 直前のコミット
[428fbf0](https://github.com/na0fu3y/ochakai/blob/428fbf070d09d6f6b3a367786ec019211ca6a8c4/docs/design/0013-attachment-files-gcs-only.md)
にある。
