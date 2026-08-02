# ochakai 設計ドキュメント 0007: DB 直結コマンドの廃止(API 一本化)

Status: Superseded by [0067](0067-four-faces-and-what-they-decline.md)
Date: 2026-07-17

データの出し入れをすべて API 経由に一本化し、**DB の隣でしか動かない
コマンドは `serve` だけ**にすると決めた — 同じ成果物への二経路目は、以後の
仕様追加のたびに片方だけ直る事故を生む。全文は Superseded 直前のコミット
[1f58114](https://github.com/na0fu3y/ochakai/blob/1f5811407b79ed42f05bb49516e1ff3bec61a72c/docs/design/0007-api-only-cli.md)
にある。
