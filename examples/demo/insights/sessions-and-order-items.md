---
type: Insight
title: セッションと明細のつなぎ方
description: 購入セッションは明細1つずつに対応し、客・商品・10分以内の時刻で1対1に結べる。注文ではなく明細につながるので「注文のチャネル」は決められない(draft。許容幅が裁定されていない)
tags: [web, marketing, attribution, join]
generated: { by: analysis_agent/claude-fable-5, at: 2026-08-19T08:00:00Z }
status: draft
---

[events](/tables/events.md) には `order_id` が無く、[order_items](/tables/order-items.md)
には `session_id` が無い。それでも、購入で終わるセッションと明細は
1 対 1 に対応している。購入セッションは見る商品がちょうど 1 つで、
`purchase` がちょうど 1 回ある。その客・商品・時刻が明細の三つと合う
ものを結ぶ。

```sql
ON  s.user_id    = oi.user_id
AND s.product_id = oi.product_id
AND ABS(TIMESTAMP_DIFF(oi.created_at, s.purchased_at, SECOND)) <= 600
```

`s.product_id` はセッションの `/product/<id>` の閲覧から取り、
`s.purchased_at` は `purchase` の時刻である。組み立て全体は
[流入元別の受注](/computations/orders-by-session-source.md)にある。
今のデータでは、この結合で購入セッションと明細が過不足なく対応する。
時刻の差は半数が 0 秒で、残りも数分以内に収まる。

## 分かること

- **流入元は明細ごとに決まる。** 需要が落ちた月に、どの経路の訪問が
  減ったかが分かる([受注点数](/metrics/booked-items.md)の次の一手)。
- **「注文のチャネル」は決められない。** 複数の明細を持つ注文のうち
  4 分の 3 ほどは、明細ごとに別の流入元の訪問から来ている。注文に
  チャネルを一つ付けたいなら、誰かが配分の規則を決める必要がある。

## draft である理由

- **10 分という幅は、データを見て置いた値である。** 2 分に狭めると 6 件に
  1 件ほどが落ちる。広げても今は増えないが、同じ客が同じ商品を短い間隔で
  二度買うようになれば二重につながる。生成器の仕様が書かれたものは無く、
  この幅が正しいと言える根拠は「今は 1 対 1 になる」ことだけである。
- **期間で `events` を切ると取りこぼす。** 最後の商品閲覧から `purchase`
  まで最大 4 日空くので、期間の始点を 5 日前まで広げて読む必要がある。
