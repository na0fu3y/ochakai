# ochakai 設計ドキュメント 0039: stdio しか話せないクライアントに橋を架ける

Status: Superseded by [0067](0067-four-faces-and-what-they-decline.md)
Date: 2026-07-27

公開サービスではなく CLI の `ochakai mcp-stdio` で stdio クライアントの穴を
埋めると決めた — JSON-RPC をメッセージ単位で素通しするので、ツール定義を
二重に持たない。全文は Superseded 直前のコミット
[1f58114](https://github.com/na0fu3y/ochakai/blob/1f5811407b79ed42f05bb49516e1ff3bec61a72c/docs/design/0039-mcp-stdio-bridge.md)
にある。
