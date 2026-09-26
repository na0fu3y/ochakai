---
type: Skill
title: メトリクスが沈んだときの調べ方
description: 「なぜ売上が落ちたのか」と訊かれたときの手順。当月を外し、要因に分け、落とした要因を掘り、行動か行き止まりのどちらかで答え、receipt 付きで書き戻す
tags: [diagnosis, runbook, agent]
generated: { by: human:sato@example.co.jp, at: 2026-09-19T07:00:00Z }
verified:
  - { by: human:tanaka@example.co.jp, at: 2026-09-22T01:20:00Z }
status: stable
---

メトリクスが落ちたと訊かれたときの手順である。因果の中身は
[売上が落ちるときの因果](/insights/why-revenue-falls.md)が持ち、ここは
順番だけを持つ。

## 手順

1. **その月は読んでよい月か。** 当月なら、落ちているのではなく未完で
   ある([売上の読み方](/insights/reading-revenue.md))。直近の完了した
   月で問い直す。
2. **定義を合わせる。** 訊いた人の「売上」が[売上](/metrics/revenue.md)
   なのか、受注ベースの[月次受注額](/computations/monthly-bookings.md)
   なのかを確かめる。古い資料の数字は後者のことが多い。
3. **要因に分ける。** [月次売上の要因分解](/computations/revenue-drivers.md)
   を、問題の月とその前月を含む範囲で走らせる。三つの `d_` のうち、一番
   負のものが落とした要因である。
4. **落とした要因を掘る。** 次に走らせる計算は要因ごとに決まっている
   (因果の表の「次に見る」列)。完了率なら、返品・キャンセル・未完了の
   どれに流れたかを先に見る。
5. **行動か、行き止まりかで答える。** 因果の表の先に
   [actions/](/actions/review-high-return-products.md) の concept があれば、
   それを候補として示す。実行は[アクションを実行する](/skills/run-an-action.md)
   の手順で、人の裁定で止まる。表の先がデータの欠落なら、
   [不足しているデータ](/insights/missing-data.md)のどの行で止まったかを
   答えに含める。止まった先を推測で埋めない。
6. **書き戻す。** 診断を `insights/cases/<YYYY-MM>-<何の件か>` に
   `Insight` の draft で書く。結論、要因の表、止まった場所、そして
   receipt(計算ごとの `job_id`)を必ず入れる。データは毎日作り直される
   ので、receipt の無い診断は翌日には確かめられない。見本は
   [2025 年 7 月の売上減の診断](/insights/cases/2025-07-revenue-dip.md)。
7. **報告する。** 使った concept ごとに `report_outcome` で worked か
   failed かを返す。行き止まりに当たった回数も、ここに残る。

## やらないこと

- 要因に分ける前に、カテゴリや流入元で割り始めない。どれかは必ず
  落ちているので、最初に割った切り口が原因に見えてしまう。
- 「客が減った」と答えない。このデータで売上を落とすのは、たいてい需要
  ではない。受注点数を見てから言う。
- draft を自分で verify しない。確かめるのは人である。
