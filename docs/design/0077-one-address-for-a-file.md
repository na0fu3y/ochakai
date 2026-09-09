# ochakai 設計ドキュメント 0077: ファイルの住所は一つである

Status: Superseded by [0140](0140-one-address-reads-as-well-as-writes.md)
Date: 2026-08-03

`ochakai attach` / `detach` を `ochakai put` / `ochakai delete` に畳み、
ファイルを「concept の id と、その中での名前」ではなく**それが置かれて
いるバンドルパス**で名指すと決めた(CLI 26 → 24、新しいフラグは無し)。
全文は Superseded 直前のコミット
[41a8ae1](https://github.com/na0fu3y/ochakai/blob/41a8ae1584ef76f6b1a72d2d9f6c6705cf7a55b4/docs/design/0077-one-address-for-a-file.md)
にある。
