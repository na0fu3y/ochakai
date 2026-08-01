# ochakai 設計ドキュメント 0043: 文書を真とする — document-first への全面置き換え

Status: Superseded by [0046](0046-bundle-address-space.md)
Date: 2026-07-28

保存もワイヤも正準化された OKF 文書そのものにし、写像を恒等に潰した:
DB は文書の索引とインスタンス固有の台帳(検証・却下・利用・リビジョン)に
徹し、status は OKF の 3 値、ETag は内容ハッシュ、actor は
`human:`/`process:` にした。全文は Superseded 直前のコミット
[428fbf0](https://github.com/na0fu3y/ochakai/blob/428fbf070d09d6f6b3a367786ec019211ca6a8c4/docs/design/0043-document-first.md)
にある。
