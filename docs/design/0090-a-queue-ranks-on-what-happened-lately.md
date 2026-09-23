# ochakai 設計ドキュメント 0090: キューは、最近あったことで並ぶ

Status: Superseded by [0141](0141-a-miss-is-read-off-the-words.md)
Date: 2026-08-10

`sort=usage` と `sort=failed` を生涯累積ではなく直近 90 日の数で並べ、
窓を `usage.recent` として数と一緒にワイヤに出すと決めた。0141 §2.5 が
そのまま引き継いでいる。
全文は Superseded 直前のコミット
[6d05750](https://github.com/na0fu3y/ochakai/blob/6d0575075f45607b07f481369c14b9dd57edb77a/docs/design/0090-a-queue-ranks-on-what-happened-lately.md)
にある。
