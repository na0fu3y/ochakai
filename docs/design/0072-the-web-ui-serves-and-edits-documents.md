# ochakai 設計ドキュメント 0072: Web UI — 二つの配信経路と、文書としての編集

Status: Superseded by [0130](0130-the-web-ui-and-the-fields-of-a-document.md)
Date: 2026-08-03

Web UI の配信を `ui` と `serve-ui` の二経路に分け、編集を正準 OKF 文書の
テキスト編集一本にすると決めた。行エディタを戻す条件と、そのとき作るもの
(サーバ側の「文書 → 構造」の面)を §3.1 に書き置いており、0130 はそれを
実行した記録である。全文は Superseded 直前のコミット
[f705147](https://github.com/na0fu3y/ochakai/blob/f7051476e7586529cb4783b8f3a366e1c0406b2d/docs/design/0072-the-web-ui-serves-and-edits-documents.md)
にある。
