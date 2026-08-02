# ochakai 設計ドキュメント 0049: キューの長さを数える — ループが人を呼べるようにする

Status: Superseded by [0069](0069-the-loop-and-what-measures-it.md)
Date: 2026-07-28

三本のキューを*一覧する*代わりに*数える*ことと、空でない間 2 で終了する
`--exit-code` を足すと決めた — 配送もスケジューラも持たず、押すのは運用者の
cron である。全文は Superseded 直前のコミット
[1f58114](https://github.com/na0fu3y/ochakai/blob/1f5811407b79ed42f05bb49516e1ff3bec61a72c/docs/design/0049-queue-counts.md)
にある。
