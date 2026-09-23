# ochakai 設計ドキュメント 0069: 検証ループと、それを測るもの

Status: Superseded by [0141](0141-a-miss-is-read-off-the-words.md)
Date: 2026-08-02

六つの記録を検証ループと利用測定の一冊に畳んだ — 三つのキュー、
読み取りパスに触れない利用測定、ヒット 0 の検索をミスとして記録すること、
`GET /api/v1/stats`。0141 がミスの定義を「どの concept の言葉にも一致
しなかった検索」に改め、積まれていた四冊とともに一冊に畳み直した。
全文は Superseded 直前のコミット
[6d05750](https://github.com/na0fu3y/ochakai/blob/6d0575075f45607b07f481369c14b9dd57edb77a/docs/design/0069-the-loop-and-what-measures-it.md)
にある。
