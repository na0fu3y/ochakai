---
type: Policy
resource: https://wiki.example.co.jp/finance/revenue-recognition
title: 売上計上ポリシー (FY2026)
description: どの明細を、いつ売上として数えるか、そして動く過去とどう付き合うか
tags: [finance, revenue, policy]
generated: { by: human:sato@example.co.jp, at: 2026-07-24T06:00:00Z }
verified:
  - { by: human:tanaka@example.co.jp, at: 2026-07-28T02:05:00Z }
status: stable
---

数字が従っている規則である。この concept の resource が指す経理の wiki
ページの写しで、正であるのは向こうのページ。ここにあるのは、
[売上](/metrics/revenue.md)を読んでいるエージェントが、ナレッジベース
を出ずに規則を見られるようにするためである。

FY2026 について、あえてそう決めた三つ:

1. **売上は配達完了ベースである。** 数えるのは `status = 'Complete'` の
   [明細](/tables/order-items.md)だけ([完了した注文](/glossary/completed-order.md))。
   受注時点でも出荷時点でもない。受注ベースの合計は
   [月次受注額](/queries/sales/monthly-bookings.md)と呼び、売上とは
   呼ばない。
2. **返品は遡って消える。相殺しない。** 明細が `Returned` になった
   時点で、その明細は売れた月の売上から抜ける。マイナスの行や相殺の
   列は作らない。**過去の月の数字は動くのが正しい** — 動かない数字が
   要る場面(締め、社外報告)では、締めの時点で
   [月次売上](/queries/sales/monthly-revenue.md)を実行し、その receipt
   を数字と一緒に保管する。数字の固定は台帳の仕事であって、クエリの
   仕事ではない。
3. **月は UTC の暦月で切る。** 店は US で、倉庫のタイムスタンプは UTC
   である。Asia/Tokyo に直してから切ると、月末の注文が隣の月に落ちて、
   誰の数字とも合わなくなる。

規則 2 がこのデータセットでは特に効く: `status` に履歴が無いので、
receipt を残さなかった月次は再現できない
([thelook_ecommerce データセット](/references/thelook-dataset.md))。
規則の見直しは年度ごとで、変わったときは
[売上](/metrics/revenue.md)が書き直しになる — 再検証ではなく、書き直し
である。
