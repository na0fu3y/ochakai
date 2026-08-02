# ochakai 設計ドキュメント 0019: v0.10.0 リリース前の整合調整 — ID 文字種の OKF 同等化・インポートのスキップ拡張・compile のモデル解決

Status: Superseded by [0074](0074-the-document-and-the-vocabulary-that-asks-it.md)
Date: 2026-07-19

id セグメントの文字種を**パス安全性だけ**に緩和し、取り込みはサーバーが
拒否した文書を**その一件のスキップ**として続行すると決めた(§4 の compile の
モデル解決は、compile 撤去により対象消滅)。全文は Superseded 直前のコミット
[f2bab02](https://github.com/na0fu3y/ochakai/blob/f2bab028f8553e6a2e9134613d638d6224683bac/docs/design/0019-release-review-adjustments.md)
にある。
