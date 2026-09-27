---
type: Metric
title: 受注点数
description: その月に受注した明細の数。状態を問わない。売上の分解の一つ目で、需要を表す
tags: [sales, demand]
generated: { by: analysis_agent/claude-fable-5, at: 2026-09-18T02:00:00Z }
verified:
  - { by: human:tanaka@example.co.jp, at: 2026-09-26T03:00:00Z }
status: stable
grain: item
synonyms: [受注数, 需要, booked items]
unit: count
---

その月に `created_at` を持つ[明細](/tables/order-items.md)の数である。
`Cancelled` も `Returned` も、まだ届いていない `Processing` / `Shipped`
も数える。[売上](/metrics/revenue.md)を分解したときの一つ目の要因で、
客がどれだけ買いに来たか、つまり需要を表す。

- **1 日あたりで月におよそ 4% ずつ伸びる。** 生成器が成長を作り込んで
  いるためで、伸びていること自体には情報が無い。
- **月の日数で 1 割動く。** 数は日数に比例するので、2 月は前月より 1 割
  少なく、3 月は 1 割多く出る。前月と比べるときは、
  [月次売上の要因分解](/computations/revenue-drivers.md)の `d_days` を
  引いて読む。
- **売上が落ちた月でも、たいてい伸びている。** 売上が落ちたら、まずこの
  数字が日数の分を除いて落ちていないことを確かめ、落ちていなければ需要の
  話ではない([売上が落ちるときの要因](/insights/why-revenue-falls.md))。
- **落ちたときに理由を探す先がこのデータには無い。** 流入元の内訳までは
  [流入元別の受注](/computations/orders-by-session-source.md)で
  見られるが、広告費やキャンペーンの記録は無い
  ([不足しているデータ](/insights/missing-data.md))。

注文の数ではなく明細の数で数えるのは、売上の分解がちょうど積になるように
するためである。注文あたりの点数は 1.4 前後でほとんど動かない。
