# ochakai 設計ドキュメント 0040: デプロイ単位の read-only

Status: Superseded by [0066](0066-four-postures-one-word.md)
Date: 2026-07-27

デプロイ全体を読み取り専用にできるようにし、**強制点をサービス層の
一箇所**に置くと決めた — 呼び出し元を区別しないので認可ではなく、四つの面
それぞれが自分の語彙で告げ、利用テレメトリだけが凍結の外に残る。全文は
Superseded 直前のコミット
[2bc9c77](https://github.com/na0fu3y/ochakai/blob/2bc9c77b487def57ad1b68847f138489b1a1edef/docs/design/0040-read-only-mode.md)
にある。
