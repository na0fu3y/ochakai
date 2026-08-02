# ochakai 設計ドキュメント 0061: dry run は書き込みを止めたものであって、別の読み方ではない

Status: Superseded by [0074](0074-the-document-and-the-vocabulary-that-asks-it.md)
Date: 2026-07-30

書き込み面そのものに `dry_run` を足し、**同じ判定を書かずに返す**と決めた —
`--strict` が落ちる理由の半分はバンドルの外にあり、関門が緑で本番が赤くなる
偽グリーンを生んでいた。全文は Superseded 直前のコミット
[f2bab02](https://github.com/na0fu3y/ochakai/blob/f2bab028f8553e6a2e9134613d638d6224683bac/docs/design/0061-a-dry-run-is-the-write-withheld.md)
にある。
