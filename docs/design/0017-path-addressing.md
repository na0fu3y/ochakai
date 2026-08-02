# ochakai 設計ドキュメント 0017: 配置とタイプの分離 — パスが住所、タイプは属性

Status: Superseded by [0075](0075-the-bundle-is-the-address-space.md)
Date: 2026-07-19

concept の id をバンドル・ルートからのフルパスとし、**パスは型を主張しない**と
決めた — 何であるかは frontmatter が言い、どこにあるかはパスが言う。配置は
利用者のものであり、サーバーは推測しない。全文は Superseded 直前のコミット
[f2bab02](https://github.com/na0fu3y/ochakai/blob/f2bab028f8553e6a2e9134613d638d6224683bac/docs/design/0017-path-addressing.md)
にある。
