# ochakai 設計ドキュメント 0024: リンクは本文から導出する

Status: Superseded by [0074](0074-the-document-and-the-vocabulary-that-asks-it.md)
Date: 2026-07-20

構造化された `links` 入力を廃し、**本文が関係である**と決めた — `rel` は
機械可読な型として導入されたが機械が一度も読んでおらず、本文と `links` は
二重管理で同期していなかった。全文は Superseded 直前のコミット
[f2bab02](https://github.com/na0fu3y/ochakai/blob/f2bab028f8553e6a2e9134613d638d6224683bac/docs/design/0024-links-from-body.md)
にある。
