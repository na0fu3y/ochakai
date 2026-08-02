# ochakai 設計ドキュメント 0050: 一覧はカーソルでページングし、順位はしない

Status: Superseded by [0068](0068-how-a-face-is-added-and-removed.md)
Date: 2026-07-28

一覧に不透明な keyset `cursor` を足し、**検索にはページングを持たせない**と
決めた — 融合の窓が順位そのものなので、51 位を出すことは正しく答えられない
問いに答えの形だけを与えることになる。全文は Superseded 直前のコミット
[1f58114](https://github.com/na0fu3y/ochakai/blob/1f5811407b79ed42f05bb49516e1ff3bec61a72c/docs/design/0050-listings-page-rankings-do-not.md)
にある。
