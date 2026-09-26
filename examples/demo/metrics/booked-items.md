---
type: Metric
title: 受注点数
description: その月に受注した明細の数。状態を問わない。売上の分解の一つ目で、需要を表す
tags: [sales, demand]
generated: { by: analysis_agent/claude-fable-5, at: 2026-09-18T02:00:00Z }
verified:
  - { by: human:tanaka@example.co.jp, at: 2026-09-20T01:00:00Z }
status: stable
grain: item
synonyms: [受注数, 需要, booked items]
unit: count
---

その月に `created_at` を持つ[明細](/tables/order-items.md)の数である。
`Cancelled` も `Returned` も、まだ届いていない `Processing` / `Shipped`
も数える。[売上](/metrics/revenue.md)を分解したときの一つ目の要因で、
客がどれだけ買いに来たか、つまり需要を表す。

- **月におよそ 4% ずつ伸びる。** 生成器が成長を作り込んでいるためで、
  伸びていること自体には情報が無い。
- **売上が落ちた月でも、たいてい伸びている。** 売上が前月を割った月を
  並べると、受注点数は平均でほぼ横ばいか微増だった。売上が落ちたら、
  まずこの数字が落ちていないことを確かめ、落ちていなければ需要の話では
  ない([売上が落ちるときの因果](/insights/why-revenue-falls.md))。
- **落ちたときに理由を探す先がこのデータには無い。** 流入元の内訳までは
  [流入元別の受注](/computations/orders-by-session-source.md)で
  見られるが、広告費やキャンペーンの記録は無い
  ([不足しているデータ](/insights/missing-data.md))。

注文の数ではなく明細の数で数えるのは、売上の分解がちょうど積になるように
するためである。注文あたりの点数は 1.4 前後でほとんど動かない。
