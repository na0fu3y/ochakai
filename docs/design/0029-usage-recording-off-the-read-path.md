# ochakai 設計ドキュメント 0029: 利用測定を読み取りパスから外す

Status: Superseded by [0069](0069-the-loop-and-what-measures-it.md)
Date: 2026-07-26

利用イベントをメモリにバッファして定期フラッシュし、**利用測定は
best-effort である**と明示すると決めた — 失われてはいけないのはナレッジ
本体であって、引かれた回数の端数ではない。全文は Superseded 直前のコミット
[1f58114](https://github.com/na0fu3y/ochakai/blob/1f5811407b79ed42f05bb49516e1ff3bec61a72c/docs/design/0029-usage-recording-off-the-read-path.md)
にある。
