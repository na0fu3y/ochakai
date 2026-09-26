---
type: Skill
title: BigQuery の計算を実行する
description: このバンドルで runtime bigquery の Attested Computation をどう動かすか、実行が何を持ち帰るか、そして過去の receipt をどう読み直すか
tags: [bigquery, executor, runbook]
generated: { by: human:sato@example.co.jp, at: 2026-07-24T07:40:00Z }
verified:
  - { by: human:tanaka@example.co.jp, at: 2026-07-28T01:40:00Z }
status: stable
---

このバンドルの `runtime: bigquery` の concept が `executor` として名指す
手順である。`computations/` の計算はすべてこれを名指し、
[アクションを実行する](/skills/run-an-action.md)はこの手順の上に段を
足す。

## 実行する

1. concept の `# Computation` フェンスから SQL を読む。**そのまま実行し、**
   フィルタも LIMIT も列も足さない。書き換えたクエリは別の計算であり、
   attester がそう判定する。
2. `parameters` の各項目を、宣言された型のクエリパラメータとして束縛する。
   `DATE` は `2026-06-01` の形で渡す。文字列として SQL に埋め込まない。
3. 自分の Google Cloud プロジェクトで実行する。
   [thelook_ecommerce](/datasets/thelook-ecommerce.md) は公開データセット
   なので読む許可は要らないが、課金は実行側のプロジェクトに付く。
   ロケーションは `US` である。
4. 結果と一緒に、下の receipt を返す。

## receipt

| フィールド | どこから来るか |
|---|---|
| `job_id` | BigQuery のジョブ。監査の跡であり、あとから実行を読み直す手段 |
| `executed_sql` | 送られたとおりのクエリ。送るつもりだったものではない |
| `row_count` | 返った行数。0 行は結果であって、失敗ではない |

attester が実際に読むのは `executed_sql` である。SQL を送っておいて別の
ものを報告する、という失敗はここで捕まる。

## 過去の receipt を読み直す

このデータセットは履歴ごと毎日作り直されるので、同じ SQL を今日走らせても
先週の数字は出ない。読み直す方法は二つある。

- **結果そのもの。** `bq show -j <job_id>` でジョブを引き、結果の一時
  テーブルが残っていればそれを読む。残るのはおよそ 1 日である。
- **その時刻のデータ。** 7 日以内なら、ジョブの作成時刻でタイムトラベル
  できる。テーブルの参照すべてに同じ時刻の
  `FOR SYSTEM_TIME AS OF TIMESTAMP '<作成時刻>'` を付ける。これは SQL の
  書き換えなので、その実行の receipt は元の計算の証明にはならない。
  元の数字と一致することを確かめるための読み直しである。

7 日を過ぎた数字は、receipt と一緒に書き残したものの中にしか無い。
診断を書き戻すときに数字と receipt を並べて残すのは、このためである
([メトリクスが沈んだときの調べ方](/skills/diagnose-a-metric.md))。

## ochakai の側でやらないこと

計算を実行することも、receipt を確かめることも、クエリを走らせる側(この
バンドルの想定では Claude Code)の仕事である。ochakai はこの手順と、それを
指す契約を保存するだけで、どちらも実行しない。
