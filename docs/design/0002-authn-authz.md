# ochakai 設計ドキュメント 0002: 認証・認可

Status: Superseded by [0065](0065-identity-and-provenance.md)
Date: 2026-07-15

認可機構を一切持たず、**到達できた者は読み書きできる**と決めた
— 誰を到達させるかはデプロイ側(Cloud Run IAM)の責務であり、ochakai が
ヘッダから読むのは provenance だけで、信頼の判断は参照側が provenance を
見て行う。全文は Superseded 直前のコミット
[2bc9c77](https://github.com/na0fu3y/ochakai/blob/2bc9c77b487def57ad1b68847f138489b1a1edef/docs/design/0002-authn-authz.md)
にある。
