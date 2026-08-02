# ochakai 設計ドキュメント 0046: バンドルがアドレス空間 — 受け取った文書を書き換えない

Status: Superseded by [0075](0075-the-bundle-is-the-address-space.md)
Date: 2026-07-28

バンドルを「パス → オブジェクト」の写像そのものとし、**バンドルに入った
ものは出てくる**を中心の不変条件に据えた。保存は受け取ったバイト列、正準形は
導出値、他者の trust family は `received:` の主張として残る。全文は Superseded 直前のコミット
[f2bab02](https://github.com/na0fu3y/ochakai/blob/f2bab028f8553e6a2e9134613d638d6224683bac/docs/design/0046-bundle-address-space.md)
にある。
