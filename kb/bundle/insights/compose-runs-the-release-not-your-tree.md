---
type: Insight
title: compose が動かすのはリリース版で、作業ツリーではない
description: deploy/compose.yaml の up は ghcr の :latest を pull する — 検索の診断は --build しないと未リリースの修正が「壊れている」ように見える
tags: [dogfood, diagnosis, search]
status: draft
sources:
  - id: pr-583
    resource: https://github.com/na0fu3y/ochakai/pull/583
    title: 複合クエリの名前ルール(この罠を踏みながら診断した PR)
generated:
    by: process:claude-code
    via: human:anonymous
    producer: claude-code/1.0
    at: "2026-08-10T04:30:00Z"
created_by: process:claude-code via human:anonymous using claude-code/1.0
---

`docker compose -f deploy/compose.yaml up -d` が動かすイメージは
**ghcr から pull した `:latest`** で、いま checkout している作業ツリーでは
ない。検索まわりを診断するとこの差が結果ごと嘘になる — 実際、名前一致
ルール(a7bf4a1)と日本語 tsv 検索(9dce56e)が未リリースだった期間、
compose 上の測定は「単独クエリでも定義が 1 位にならない」を示し、
HEAD には存在しない不具合を追いかけさせた。

型は三つ:

1. **診断は `up -d --build`** で作業ツリーからビルドする。ただし
   `--build` はローカルの `:latest` タグを**上書きする** — リリース版に
   戻すには `docker compose pull`。
2. 8080 を別プロジェクト(twin など)が使っていたら、**別プロジェクト名
   + port override で並走**させる:
   `docker compose -p <name> -f deploy/compose.yaml -f <override> up -d --build`
   (override は `ports: !override` で別ポートを充てる 4 行)。消すときも
   同じ `-p` で `down -v`。
3. ランキングの再現コーパスは**定義 concept 数件 + 全用語を併記する散文
   数件**が最小形 — 単独クエリと複合クエリの順位差がそのまま診断になる。
   この形は PR #583 で `TestIntegrationLexicalSearchLiftsWhatACompoundQueryNames`
   として恒久化した。

ランキング変更そのものの門番は searcheval の golden set である。#583 の
初案(スペース区切り言語の全単語を用語扱い)は lexical MRR 0.92→0.86 で
弾かれ、用語抽出を漢字・カタカナ含みの run に絞る判断はこの数字から出た。
「変更が golden set を下げたら、まず変更の側を疑う」が効いた実例。
