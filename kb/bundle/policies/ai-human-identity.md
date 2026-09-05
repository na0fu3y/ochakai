---
type: Policy
title: AI と人の identity 規律
description: dev モードの匿名 identity の上で「人がレビューしたか、AI オンリーか」を台帳で区別し続けるための規律
tags: [dogfood, provenance, trust]
status: draft
sources:
  - id: httpauth
    resource: internal/httpauth/httpauth.go
    title: dev モードでも委譲ヘッダを処理する実装
  - id: d65
    resource: docs/design/0065-identity-and-provenance.md
    title: 設計ドキュメント 0065(identity と provenance)
---

trust tier(`unverified` / `machine-confirmed` / `human-reviewed`)は
検証台帳のうち**立っている行**(`at` が内容の最終変更以降の行)の
actor の kind から導かれる — いまの内容について `human:` の行が
一つでもあれば human-reviewed である(OKF SPEC §5.3、設計ドキュメント
0065 §6 を 0138 が改訂)。ところが dogfood インスタンスは `OCHAKAI_MODE=dev` で動き、
そこでは**全員が `human:anonymous`** になる。素のままだと AI の書き込みも
「human」として記録され、AI が verify を打てば human-reviewed が偽造
できてしまう。

dev モードは委譲を全呼び出し元に開いている[^httpauth]ので、規律は
一行で書ける:

**AI は常に `process:` を名乗る。人はそのままでよい。**

具体的には:

1. **AI の書き込みは MCP 経由だけ。** リポジトリの `.mcp.json` が
   `Ochakai-On-Behalf-Of: process:claude-code` を全リクエストに載せる
   ので、`put_concept` / `report_outcome` は
   `process:claude-code via human:anonymous` として記録される
   (producer も clientInfo から自動で残る)。
2. **AI は CLI で書かない。** dev インスタンスに対する CLI は匿名 =
   human として記録されるため、書き込み系コマンド(put / verify /
   delete / purge / move / reembed / import / report)は
   `.claude/settings.json` の deny に載せてある。
3. **裁定は人の行為。** verify / reject は人が web UI か CLI から打つ。
   MCP に裁定の面はそもそも無い(設計ドキュメント 0067 §6)し、
   `put_concept` は人が裁定済みの concept の置き換えを拒否する。
4. **import を回すのも人。** import は文書の verified キーを実行者の
   検証として記録し直す(review gate)。AI が回すと human の検証が
   立ってしまう — deny に入っているのはそのためでもある。

この上で trust tier はこう読める:

| tier | この instance での意味 |
|---|---|
| `unverified`(created_by が `process:`) | AI オンリー。draft キューで人を待っている |
| `human-reviewed` | 人が確認した — 台帳に human の行がある |
| `machine-confirmed` | 機械の確認だけ(将来の canary / CI verify の枠) |

**限界も規律の一部である。** これは設定による規律であって構造的な
強制ではない — 認可を持たないのが ochakai の設計で、担保は制限ではなく
記録である(0065 §1)。AI が deny を迂回して素の HTTP で書けば匿名で
記録されてしまう。それが問題になる規模になったら、dev モードではなく
本物の Cloud Run + AI 専用のサービスアカウントに移す — そこでは認証
そのものが区別になり、規律は構造になる。

[^httpauth]: dev モードでも委譲ヘッダを処理する実装
