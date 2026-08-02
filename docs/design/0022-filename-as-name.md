# ochakai 設計ドキュメント 0022: ファイル名が名前 — title の任意化・ID の検索対象化・NFC 正規化

Status: Superseded by [0074](0074-the-document-and-the-vocabulary-that-asks-it.md)
Date: 2026-07-20

`title` を任意にし、表示名を id の末尾セグメントにフォールバックさせ、id を
検索対象に入れ、**鍵として比較される文字列だけ**を NFC に正規化すると決めた
— 住所の末尾は、多くの場合そのまま名前である。全文は Superseded 直前のコミット
[f2bab02](https://github.com/na0fu3y/ochakai/blob/f2bab028f8553e6a2e9134613d638d6224683bac/docs/design/0022-filename-as-name.md)
にある。
