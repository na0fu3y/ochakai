---
type: Metric
title: 完了率
description: 受注した明細のうち、売上になった(Complete の)明細の割合。売上の分解の二つ目で、残りは返品・キャンセル・未完了に流れる
tags: [sales, fulfillment, returns]
generated: { by: analysis_agent/claude-fable-5, at: 2026-09-18T02:20:00Z }
verified:
  - { by: human:tanaka@example.co.jp, at: 2026-09-20T01:05:00Z }
status: stable
grain: item
synonyms: [completion rate, 完了割合]
unit: ratio
---

その月に受注した[明細](/tables/order-items.md)のうち、`status = 'Complete'`
の明細の割合である。[売上](/metrics/revenue.md)を分解したときの二つ目の
要因で、受注が売上になった割合を表す。

完了しなかった明細は三つのどこかに流れる。完了率が落ちたときは、どこに
流れたかで次に見る先が変わる。

| 流れた先 | 状態 | 次に見る先 |
|---|---|---|
| 返品 | `Returned` | [返品率](/metrics/return-rate.md)と[返品率の高い商品の見直し](/actions/review-high-return-products.md) |
| キャンセル | `Cancelled` | 理由の記録が無い([不足しているデータ](/insights/missing-data.md)) |
| 未完了 | `Processing` / `Shipped` | 待っても完了にならない(下) |

**このデータでは、状態は時間が経っても進まない。** 7 年前の明細も、
半分は `Processing` か `Shipped` のままである。状態は生成時に振られて
いて、受注から配達へ進んでいく記録ではない
([完了した注文](/glossary/completed-order.md))。そのため完了率は月に
よらず 4 分の 1 前後で、その周りで数ポイント上下する。この上下が、売上が
落ちる月の半分ほどを説明する([売上が落ちるときの因果](/insights/why-revenue-falls.md))。

値の水準を現実の EC と比べてはいけない。「受注の 4 分の 3 が売上に
ならない店」ではなく、そう振られた生成器である。
