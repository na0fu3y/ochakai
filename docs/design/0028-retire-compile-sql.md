# ochakai 設計ドキュメント 0028: compile_sql とセマンティックモデル面の撤去

Status: Superseded by [0070](0070-what-was-retired-and-why.md)
Date: 2026-07-25

決定的な SQL コンパイルとその周辺(REST・MCP・CLI・Web UI・書き込み時検証)を
すべて撤去すると決めた — エージェントが要るのは検証済みクエリとその注意点で
あり、両方 `get_context` から来る。全文は Superseded 直前のコミット
[f2bab02](https://github.com/na0fu3y/ochakai/blob/f2bab028f8553e6a2e9134613d638d6224683bac/docs/design/0028-retire-compile-sql.md)
にある。
