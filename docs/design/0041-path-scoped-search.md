# ochakai 設計ドキュメント 0041: 住所で検索の範囲を絞る — prefix フィルタ

Status: Superseded by [0075](0075-the-bundle-is-the-address-space.md)
Date: 2026-07-28

「パスが住所」を検索と context に届かせる `prefix` フィルタを足すと決めた —
セグメント境界で照合し、繰り返せば OR で結ぶ。**閲覧を制限する機構では
ない。**全文は Superseded 直前のコミット
[f2bab02](https://github.com/na0fu3y/ochakai/blob/f2bab028f8553e6a2e9134613d638d6224683bac/docs/design/0041-path-scoped-search.md)
にある。
