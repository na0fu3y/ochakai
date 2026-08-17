---
type: Glossary Term
title: 完了した注文
description: ここに出てくる売上が数える注文の状態と、数えない三つの状態
tags: [sales, orders, glossary]
generated: { by: analysis_agent/gemini-2.5-pro, at: 2026-07-17T04:20:00Z }
verified:
  - { by: human:tanaka@example.co.jp, at: 2026-07-20T02:10:00Z }
status: stable
---

注文は、出荷され、かつ入金が確定した時点で **完了** になる。
[shop.orders](/tables/shop-orders.md) では `status = 'completed'` がそれで、
[売上](/metrics/revenue.md)が数えるのはこの状態だけである。

状態は他に三つあり、どれも売上ではない:

| 状態 | 意味 | 売上でない理由 |
|---|---|---|
| `pending` | 注文は入ったが入金が未確定 | 与信が通らないことがある |
| `cancelled` | 出荷前に取り消された | 何も届いていない |
| `returned` | 出荷後に全品返品された | 代金が戻っている |

**一部返品は状態ではない。** 3点のうち1点が返ってきた注文は `completed` の
ままで、`total_price` も元のままである。10,000 円で出荷して3分の1が返品
された注文も、まるごと数えられる。これは
[売上計上ポリシー](/policies/revenue-recognition.md)が決めたことであって、
クエリの不具合ではない。経理が締めた数字よりこの数字が大きい最大の理由が
これで、どれくらい動くかは[売上の読み方](/insights/reading-revenue.md)に
ある。

「完了」は*注文*の状態であって、出荷の状態ではない。分割出荷の注文が完了
するのは一度、最後の荷物が出たときである。

会話に出てくる隣の語と混同しないこと。**受注**は注文が入った時点
(`pending` を含む)を、**出荷**は `shipped` を指して使われることが多く、
どちらも売上が数える集合ではない。`cancelled` は **キャンセル**、
`returned` は **返品** と呼ぶ。
