---
type: Skill
title: dogfood ループの回し方
description: ochakai 自身の開発ナレッジをローカルインスタンスで貯め、レビューし、kb/ に書き戻す手順
tags: [dogfood, runbook]
status: draft
sources:
  - id: compose
    resource: deploy/compose.yaml
    title: ローカル開発ハーネス
  - id: hooks
    resource: examples/claude-code/README.md
    title: 想起と書き戻しのループの原型
---

ochakai の開発で得た知識 — リリースレビューの学び、環境の癖、どこにも
書かれていない慣行 — を、ochakai 自身のローカルインスタンスで管理する
手順。identity の使い分けは [identity 規律](/policies/ai-human-identity.md)
が先で、この手順はその上に乗る。

## 立てる

```sh
docker compose -f deploy/compose.yaml up -d
ochakai use http://localhost:8080
ochakai import kb/bundle   # 初回だけ。回すのは人(README の re-stamp 規則)
```

`pgdata` volume が唯一の永続層で、**検証台帳(trust)はここにしか
住まない**。volume を消すと知識は kb/ から戻せるが、誰が何を確認した
かは戻らない。

## 日々のループ

- **想起は自動。** `.claude/hooks/ochakai-recall.sh` が
  UserPromptSubmit で `ochakai search` を引き、関連 concept への
  ポインタ行を注入する。読むかどうかはエージェントの選択で、fetch は
  MCP の `get_concept` から行う(identity 規律)。指された concept は
  記録され、Stop 時に
  write-back フックが「使った知識は持ちこたえたか(report_outcome)、
  新しく学んだことは無いか(put_concept で draft)」を一度だけ訊く。
- **AI の書き込みは draft で入る。** レビューキューは
  `ochakai list usage --status draft`、未処理がある間
  `ochakai stats --exit-code` が 2 で終わる — cron に置くならこれ。
- **人のレビュー。** `ochakai ui` で読んで Verify / Reject、または
  `ochakai verify <id>` / `ochakai reject <id> --note <理由>`。
  却下は消さずに台帳に残る — AI は起草前に `search --rejected` で
  同種の提案が断られていないかを確かめられる。

## kb/ への書き戻し

節目(リリース後など)に、確定した知識を git に戻す:

```sh
ochakai export kb/bundle
```

差分を見て commit する。export の `verified:` キーは git 上では
「この instance でそう確認されていた」という記録で、次に import した
ときは**実行者の検証**として立て直される — だから import は人が回す。
