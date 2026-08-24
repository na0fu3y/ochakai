# ochakai 設計ドキュメント 0126: ブラウザは、どこから来たかを言う

Status: Superseded by [0130](0130-the-web-ui-and-the-fields-of-a-document.md)
Date: 2026-08-23

クロスサイトの書き込みを断る規則を `ochakai ui` と `ochakai serve-ui` の
両方に掛け、`ochakai serve` には置かないと決めた。ヘッダを送らない呼び出し元
(CLI・エージェント・curl)は一行も変わらない。全文は Superseded 直前のコミット
[f705147](https://github.com/na0fu3y/ochakai/blob/f7051476e7586529cb4783b8f3a366e1c0406b2d/docs/design/0126-a-browser-says-where-it-came-from.md)
にある。
