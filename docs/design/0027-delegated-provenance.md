# ochakai 設計ドキュメント 0027: 呼び出し元によるエンドユーザー identity の委譲

Status: Superseded by [0065](0065-identity-and-provenance.md)
Date: 2026-07-25

許可された呼び出し元が `On-Behalf-Of` ヘッダでエンドユーザーを名乗れる
ようにし、**置き換えではなく合成**(`human:… via process:…`)で記録すると
決めた — 埋め込みホストの provenance が一つのサービスアカウントに潰れる
問題を、区別できる形で解く。全文は Superseded 直前のコミット
[2bc9c77](https://github.com/na0fu3y/ochakai/blob/2bc9c77b487def57ad1b68847f138489b1a1edef/docs/design/0027-delegated-provenance.md)
にある。
