---
type: Glossary Term
title: 完了した注文
description: 売上が数える状態と数えない四つの状態、大文字始まりの書き方、そして履歴が無いこと
tags: [sales, orders, glossary]
generated: { by: analysis_agent/claude-fable-5, at: 2026-07-25T05:00:00Z }
verified:
  - { by: human:tanaka@example.co.jp, at: 2026-07-28T01:30:00Z }
status: stable
---

注文は、配達が完了した時点で「完了」になる。`status = 'Complete'` が
それで、[売上](/metrics/revenue.md)が数えるのはこの状態だけである。

状態は全部で5つあり、**いずれも大文字始まり**である。`'complete'` と
小文字で書いたクエリはエラーにならず、0行を返す。

| 状態 | 意味 | 売上でない理由 |
|---|---|---|
| `Processing` | 受注済み、未出荷 | まだ何も動いていない |
| `Shipped` | 出荷済み、配達前 | 届く前にキャンセルされうる |
| `Complete` | 配達完了。これが売上 | — |
| `Cancelled` | 出荷前に取り消された | 何も届いていない |
| `Returned` | 返品された | 代金が戻っている |

## status は二箇所にあり、読むのは明細側

`status` は[注文](/tables/orders.md)と[明細](/tables/order-items.md)の
両方にある。このバンドルの規則は、常に明細側を読むことである。明細
ごとに状態を持つので、3点のうち1点だけ返った一部返品も、明細1行の
`Returned` として特別扱いなしに数えられる。注文側だけを読む集計は、
その1行を見落とす。

## 「完了」は現在の状態であって、記録ではない

status は上書きされ、履歴が無い。今日 `Complete` の明細が来週
`Returned` になれば、その明細は過去に遡って完了の集合から出る。先月の
売上が今日見ると減っている場合、理由はたいていこれである。この扱いを
決めているのは[売上計上ポリシー](/policies/revenue-recognition.md)で、
締めた数字の固定には receipt を使う。

会話に出てくる隣の語と混同しないこと。「受注」は注文が入った時点
(`Processing` を含む)を、「出荷」は `Shipped` を指して使われることが
多く、どちらも売上が数える集合ではない。`Cancelled` は「キャンセル」、
`Returned` は「返品」と呼ぶ。受注ベースの合計が必要なときは
[月次受注額](/queries/sales/monthly-bookings.md)を見ること。deprecated
だが、読み直すために残してある。
