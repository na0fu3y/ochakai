# ochakai 設計ドキュメント 0059: キューは、それを一覧する sort の名で呼ぶ

Status: Superseded by [0069](0069-the-loop-and-what-measures-it.md)
Date: 2026-07-30

`reported_wrong` / `past_expiry` を `failed` / `stale_after` に改名すると
決めた — `stats` が数える集合と `sort=` が並べる集合は同じ一つなので、
キューは自分の名前を持たない。全文は Superseded 直前のコミット
[1f58114](https://github.com/na0fu3y/ochakai/blob/1f5811407b79ed42f05bb49516e1ff3bec61a72c/docs/design/0059-a-queue-is-named-by-its-listing.md)
にある。
