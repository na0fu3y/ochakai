# ochakai 設計ドキュメント 0055: 裁定は一つの面から下す

Status: Superseded by [0056](0056-one-question-one-command.md)
Date: 2026-07-29

`POST /api/v1/verify/{id}`、`POST /api/v1/reject/{id}`、
`DELETE /api/v1/reject/{id}` という構造をすべて共有する三つの面を、
`ruling: verified | rejected | withdrawn` を本文に取る
`POST /api/v1/review/{id}` 一本に置き換えた。記録される内容・台帳の形・
信頼の導出は変わらない。全文は Superseded 直前のコミット
[1fac002](https://github.com/na0fu3y/ochakai/blob/1fac0027112068c8b3a8722c4eecb0fc5064206d/docs/design/0055-one-ruling-one-face.md)
にある。
