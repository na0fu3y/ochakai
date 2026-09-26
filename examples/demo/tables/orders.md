---
type: BigQuery Table
resource: bigquery://bigquery-public-data.thelook_ecommerce.orders
title: orders
description: 注文1件につき1行のヘッダ。金額の列は無く、状態は明細の写し
tags: [sales, orders, bigquery]
generated: { by: analysis_agent/claude-fable-5, at: 2026-07-25T03:10:00Z }
verified:
  - { by: human:tanaka@example.co.jp, at: 2026-07-28T01:20:00Z }
status: stable
---

注文のヘッダで、1行が注文1件である。このテーブルには**金額の列が無い**。
`orders` だけをどう集計しても売上は出ない。金額はすべて
[order_items](/tables/order-items.md) の `sale_price` にあり、素直に書いた
SQL は、たいていここで最初につまずく。

# Schema

| 列 | 型 | 注記 |
|---|---|---|
| `order_id` | INT64 | 主キー。`id` ではない |
| `user_id` | INT64 | 常に入っている。ゲスト購入という状態はこのデータセットに無い |
| `status` | STRING | その注文のすべての明細の `status` と常に同じ。状態の混ざった注文は無い |
| `num_of_item` | INT64 | 明細の行数と常に一致する。明細側に数量列が無いので、これが個数にあたる |
| `gender` | STRING | [users](/tables/users.md) の値の写し |
| `created_at` | TIMESTAMP | UTC。明細の `created_at` と同じ |
| `returned_at` / `shipped_at` / `delivered_at` | TIMESTAMP | 起きていなければ null |

状態が明細と一致しているので、一部だけ返品された注文はこのデータに
現れない。それでも集計は明細側で行う。金額が明細にしか無いことと、一致
しているのが生成器の性質であって約束ではないことが理由である
([完了した注文](/glossary/completed-order.md))。

# Joins

- [order_items](/tables/order-items.md) — `oi.order_id = o.order_id`。
  明細を持たない注文は無い。
- [users](/tables/users.md) — `u.id = o.user_id`。指す先の客は必ずいる。
  最初の注文が会員登録より前に来ることは無い。
