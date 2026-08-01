# ochakai 設計ドキュメント 0023: 型の語彙を OKF に一本化する

Status: Superseded by [0038](0038-type-vocabulary-realignment.md)
Date: 2026-07-20

内部スラグと OKF 表示名という二重語彙を廃止し、型の値を OKF 表示名の
綴りそのものにした(スラグ化・原表記の保存・別名解決という 5 つの変換
機構を消し、インポート/エクスポートを型について恒等にした)。全文は
Superseded 直前のコミット
[9091fed](https://github.com/na0fu3y/ochakai/blob/9091fed4a8f2ace8948b645ca4aa9d966d5d3466/docs/design/0023-okf-type-vocabulary.md)
にある。
