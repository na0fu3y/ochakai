---
type: Glossary Term
title: 完了した注文
description: 売上が数える状態と数えない四つの状態、大文字始まりの書き方、そして状態が時間で進まないこと
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

| 状態 | 意味 | 売上でない理由 | 入るタイムスタンプ |
|---|---|---|---|
| `Processing` | 受注済み、未出荷 | まだ何も動いていない | 無し |
| `Shipped` | 出荷済み、配達前 | 届く前にキャンセルされうる | `shipped_at` |
| `Complete` | 配達完了。これが売上 | — | `shipped_at`・`delivered_at` |
| `Cancelled` | 出荷前に取り消された | 何も届いていない | 無し |
| `Returned` | 返品された | 代金が戻っている | 三つとも |

受注した明細のうち `Complete` になった割合が[完了率](/metrics/completion-rate.md)
である。

## 状態は時間で進まない

このデータでは、状態は受注から配達へ進んでいく記録ではなく、明細が
生成されたときに振られた値である。7 年前の明細も半分は `Processing` か
`Shipped` のままで、どの年に受注した明細も状態の比率はほぼ同じになる。
待っても `Complete` は増えない。

履歴も無い。いつ何の状態だったかは、このデータからは分からない
([不足しているデータ](/insights/missing-data.md))。先月の数字が今日
変わっているのは状態が変わったからではなく、データセットが履歴ごと毎日
作り直されるからである([thelook_ecommerce](/datasets/thelook-ecommerce.md))。

## status は二箇所にあり、読むのは明細側

`status` は[orders](/tables/orders.md)と[order_items](/tables/order-items.md)
の両方にあり、今のデータでは常に一致する。一部だけ返品された注文は現れ
ない。それでもこのバンドルは常に明細側を読む。金額は明細にしか無く、
一致は生成器の性質であって約束ではないからである。

会話に出てくる隣の語と混同しないこと。「受注」は注文が入った時点
(`Processing` を含む)を、「出荷」は `Shipped` を指して使われることが
多く、どちらも売上が数える集合ではない。`Cancelled` は「キャンセル」、
`Returned` は「返品」と呼ぶ。受注ベースの合計が必要なときは
[月次受注額](/computations/monthly-bookings.md)を見ること。deprecated
だが、読み直すために残してある。
