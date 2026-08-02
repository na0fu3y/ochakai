# ochakai 設計ドキュメント 0051: 答えられなかった問いを記録し、ループをインスタンスで測る

Status: Superseded by [0069](0069-the-loop-and-what-measures-it.md)
Date: 2026-07-29

ヒット 0 の検索を一行として残し、インスタンスを一回で答える
`GET /api/v1/stats` を置くと決めた — 「何回引かれたか」はエントリの属性、
**「答えられたか」はインスタンスの属性**である。全文は Superseded 直前のコミット
[1f58114](https://github.com/na0fu3y/ochakai/blob/1f5811407b79ed42f05bb49516e1ff3bec61a72c/docs/design/0051-instance-metrics-and-search-misses.md)
にある。
