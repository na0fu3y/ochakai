# ochakai 設計ドキュメント 0093: 予算は応答全体を縛り、一位は名前だけでは返らない

Status: Superseded by [0108](0108-the-context-pack-retires.md)
Date: 2026-08-10

context pack の `budget` は `hits`・`outline`・`concepts` を合わせた
**応答全体**のバイト数であり、一位が収まらなかったときだけ outline 行が
本文の冒頭(`excerpt`)を運ぶ、と決めた — pack ごと退役したので規則も
退役した。全文は Superseded 直前のコミット
[168db82](https://github.com/na0fu3y/ochakai/blob/168db82c34a5dbeebd1f7ab3386846f6f9f08bde/docs/design/0093-the-budget-governs-the-whole-response.md)
にある。
