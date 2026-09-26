---
type: Insight
title: 売上の読み方
description: 売上の系列を読む前に知っておく三つ。当月は高く出る、過去は毎日作り直される、上がっていることには情報が無い
tags: [sales, revenue, interpretation]
generated: { by: human:sato@example.co.jp, at: 2026-08-01T08:30:00Z }
verified:
  - { by: human:tanaka@example.co.jp, at: 2026-08-03T01:00:00Z }
status: stable
---

[月次売上](/computations/monthly-revenue.md)の系列を良し悪しで読む前に、
このデータセットに特有の三つを確かめる。動いた理由を調べるのはその
あとで、手順は[売上が落ちるときの因果](/insights/why-revenue-falls.md)に
ある。

## 当月は高く出る

月の途中だから低く出る、とは限らない。生成器が直近の数日に山を作るので
(直近 7 日の日次売上は、その前の平均の約 3 倍)、当月が系列の最高値と
して出る。

未来の日付の行もある。`created_at` が今日より先の明細が毎回千行以上
入っていて、その分が当月に積まれる。

したがって読み方は一つで、直近の完了した月で系列を切る。当月を出す場合は
未完だと書き、`MAX(created_at)` を添える。

## 過去は、毎日作り直される

データセットは履歴ごと毎日再生成される([thelook_ecommerce](/datasets/thelook-ecommerce.md))。
先週見た 3 月の売上と今日の 3 月の売上は合わないのが普通で、合わないこと
自体は不具合ではない。

返品が過去の月から売上を抜いていくのではない。状態は時間が経っても
進まない([完了した注文](/glossary/completed-order.md))。動いているのは
データそのものである。締めた数字が必要なら receipt を残し、7 日以内なら
その時刻のスナップショットを読み直せる([BigQuery の計算を実行する](/skills/run-bigquery-query.md))。

## 上がっていることには情報が無い

系列には右肩上がりの成長が最初から入っていて、[受注点数](/metrics/booked-items.md)
は月におよそ 4% ずつ伸びる。上がっているのは生成器の仕様であって、誰かの
成果ではない。前年比がプラスでも前月比がプラスでも、そこに情報は無い。

落ちて見えたときだけ読む価値がある。完了した月のおよそ 3 か月に 1 回は
前月を割り、大きいときは 1〜2 割落ちる。そのときの手順が
[売上が落ちるときの因果](/insights/why-revenue-falls.md)である。
