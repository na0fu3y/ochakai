# ochakai 設計ドキュメント 0033: context の `hits` は順位に徹する

Status: Superseded by [0067](0067-four-faces-and-what-they-decline.md)
Date: 2026-07-26

`GET /api/v1/context` の `hits` から知識の複製を外し、全サーフェスで
`id` / `type` / `title` / `status` / `score` だけを運ぶ順位にすると決めた —
バイト予算が応答の一部しか縛っていなかった。全文は Superseded 直前のコミット
[1f58114](https://github.com/na0fu3y/ochakai/blob/1f5811407b79ed42f05bb49516e1ff3bec61a72c/docs/design/0033-context-hits-are-a-ranking.md)
にある。
