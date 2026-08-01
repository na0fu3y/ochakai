# ochakai 設計ドキュメント 0005: OKF 互換とナレッジの構造

Status: Superseded by [0036](0036-okf-schema-first.md)
Date: 2026-07-17

OKF バンドルとの双方向互換を導入した最初の決定。ID をスラッシュ区切りの
階層スラグにし、タイプを 5 種の閉集合から自由文字列に開き、バンドル単位の
import/export と往復規則(未知キーの保存・パス優先・サーバー所有キーの
拒否)を定めた。全文は Superseded 直前のコミット
[7f5b1e5](https://github.com/na0fu3y/ochakai/blob/7f5b1e59ea825df390d5d53125ab648b3fa0034d/docs/design/0005-okf-compatibility.md)
にある。
