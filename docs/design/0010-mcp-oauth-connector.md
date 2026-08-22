# ochakai 設計ドキュメント 0010: MCP OAuth コネクタサービス

Status: Superseded by [0012](0012-retire-mcp-oauth-connector.md)
Date: 2026-07-18

claude.ai / ChatGPT リモートコネクタ向けに、到達制御と provenance のための
MCP OAuth を、本体とは分離した公開第二サービス(CIMD、opaque トークン、
`hd` クレーム強制)として実装すると決定した。実装は撤去され、**再実装の
出発点もここではなくなった**([0116](0116-the-connector-price-changed-not-its-condition.md))。全文は Superseded 直前のコミット
[527f879](https://github.com/na0fu3y/ochakai/blob/527f879f10faf8df8c7b60ce8e8f2e977760c8b8/docs/design/0010-mcp-oauth-connector.md)
にある。
