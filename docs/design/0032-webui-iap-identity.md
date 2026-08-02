# ochakai 設計ドキュメント 0032: Web UI の書き込みを IAP の検証済み identity で記録する

Status: Superseded by [0065](0065-identity-and-provenance.md)
Date: 2026-07-26

両プロキシはブラウザ由来の委譲ヘッダを**常に捨て**、`serve-ui` は
`OCHAKAI_IAP_AUDIENCE` があるとき IAP の検証済みアサーションからのみ
作り直す(検証できなければ 403)と決めた — identity を差し替える者は、
identity の自称を運んではならない。全文は Superseded 直前のコミット
[2bc9c77](https://github.com/na0fu3y/ochakai/blob/2bc9c77b487def57ad1b68847f138489b1a1edef/docs/design/0032-webui-iap-identity.md)
にある。
