# ochakai 設計ドキュメント 0021: ナレッジの move(パス変更)と Web UI の微調整

Status: Superseded by [0075](0075-the-bundle-is-the-address-space.md)
Date: 2026-07-20

`POST /api/v1/move` を足し、id を記録しているすべての場所の書き換えを
**一トランザクション**で行うと決めた — 履歴も利用実績も内向き参照も一緒に
移り、移動先が埋まっていればソフト削除済みでも 409 である。全文は Superseded 直前のコミット
[f2bab02](https://github.com/na0fu3y/ochakai/blob/f2bab028f8553e6a2e9134613d638d6224683bac/docs/design/0021-move-and-webui-refinements.md)
にある。
