# ochakai 設計ドキュメント 0025: 書き戻しループを締める — failed が再検証を呼ぶ

Status: Superseded by [0069](0069-the-loop-and-what-measures-it.md)
Date: 2026-07-22

時間にもとづく陳腐化に**証拠にもとづく陳腐化**を足して `sort=failed` の
再検証フィードを置き、検証を独立した操作にすると決めた — 空にできない
キューは読まれなくなる。全文は Superseded 直前のコミット
[1f58114](https://github.com/na0fu3y/ochakai/blob/1f5811407b79ed42f05bb49516e1ff3bec61a72c/docs/design/0025-closing-the-loop.md)
にある。
