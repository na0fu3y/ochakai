# ochakai 設計ドキュメント 0133: OKF の瞬間は、瞬間である

Status: Superseded by [0139](0139-ochakai-does-not-choose-a-spelling.md)
Date: 2026-08-28

`stale_after`・`sources[].last_modified`・`usage_window` が日付しか受けな
かったのを改め、**RFC 3339 と `YYYY-MM-DD` の二つの綴りを取る**ことにした。
日付はそれが開く UTC の真夜中で、列は `timestamptz` へ移り比較は `now()` に
なった。入りのこの決定は 0139 が引き継ぎ、**出口(来た綴りで戻し、真夜中は
日付に畳む)だけが裏返る**。全文は Superseded 直前のコミット
[7830c61](https://github.com/na0fu3y/ochakai/blob/7830c613a4f2a10163e926ce32acb768720acf30/docs/design/0133-an-okf-moment-is-an-instant.md)
にある。
