# ochakai 設計ドキュメント 0092: ページはモジュールになる — テストできるように

Status: Superseded by [0130](0130-the-web-ui-and-the-fields-of-a-document.md)
Date: 2026-08-10

配信するものを一枚から一式の ES モジュールに直し、バンドラもフレームワークも
npm 依存も入れないまま、純粋なモジュールを `node --test` が値で確かめられる
形にした。全文は Superseded 直前のコミット
[f705147](https://github.com/na0fu3y/ochakai/blob/f7051476e7586529cb4783b8f3a366e1c0406b2d/docs/design/0092-the-page-is-modules-so-it-can-be-tested.md)
にある。
