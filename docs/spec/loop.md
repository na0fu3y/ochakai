# 検証ループと利用測定

エージェントが下書きし、人が裁定し、使った結果が戻る — その**ループが
回っているかを数で持つ**こと(C7)。数は推測ではなく台帳とイベントから
数え、押すのは数であって通知ではない。

## ループの三つの部品

1. **利用が測られている** — 検索のヒットと取得。
2. **検証が時効を持ち、古くなると浮上する** — 書き手が宣言した
   `stale_after` と、内容が変わった後に立っていない検証。
3. **再検証が証拠で優先される** — 使った人・エージェントの失敗報告。

## 裁定と trust

- **裁定は二つ**: 検証(`POST /api/v1/review/{id}` に `{"ruling":
  "verified"}`、CLI `ochakai verify`)と却下(`DELETE` に `note`、CLI
  `ochakai delete --note`)。どちらも人の面(REST・CLI・Web UI)にあり、
  MCP には無い。
- **draft への検証は、それを公開してから記録する**
  ([0157](../decisions/0157-verify-publishes-a-draft.md))。文書の
  `status` の一行だけを `stable` に書き換えて検証者の編集として保存し、
  そのうえで検証を記録するので、検証はいまの内容に対して立つ。stable と
  deprecated への検証は文書にも ETag にも触れない — 再確認が、編集中の
  人の If-Match を無効にしない。
- **裁定するのは人か、人が置いた確認ジョブ**である。`process:` の身元で
  記録された検証(CI の canary など)は `machine-confirmed` と読まれる。
  LLM は裁定しない([architecture.md](architecture.md))。
- **trust の段は、いまの内容に対して立っている検証から導く。** 立っている
  とは、台帳の行の `at` が内容の最後の意味のある変更(`generated.at`)以降
  であること。人の行があれば `human-reviewed`、他の行があれば
  `machine-confirmed`、無ければ `unverified`(SPEC §5.3)。
  - 編集と move は失効させ、整形だけの書き込みは失効させない。
  - verify は `GREATEST(now(), content_changed_at)` で記録する。
  - 台帳は一行も消えない。export の `verified` は立っている行だけを書き
    ([okf.md](okf.md))、全体は `log.md` と `?history` が持つ。
  - 立っている検証は検索で一順位ぶんの加点になる。**失効や失敗報告で
    順位を下げることはしない** — フィードで浮上させるだけである。
- **却下は削除である。** 理由はリビジョンに載り、`log.md` が
  `**Rejection**` として刷る。却下された id はどの面からも書き直せる —
  理由は情報であって壁ではない。却下された concept は検索にも一覧にも
  出ない。
- **status は裁定ではない**(draft / stable / deprecated は内容の段階で、
  書き手が書く)。MCP から書いた文書で status が無いものは draft として
  書かれる。draft を stable にするのは、書き手の編集か、draft への検証
  である。

## 四つのキューと、キューではない一つのフィード

| キュー | 一覧 | 空になる条件 |
|---|---|---|
| `drafts` | `ochakai list usage --status draft` | verify / 公開する編集 / 削除(却下を含む) |
| `failed` | `ochakai list failed` | 最後の失敗報告より後の verify |
| `stale_after` | `ochakai list stale_after` | 書き手が期限を宣言し直す編集 |
| `edited` | `ochakai list verified_at --trust unverified` | verify / 削除 |

**四本とも空にできる。** 空にできないキューは読まれなくなる。

- `failed` は `failed > 0` かつ未処理(`verified_at` が無いか、最後の失敗の
  方が新しい)。未検証の draft は最後に並ぶ — 認証済み concept の失敗の方が
  緊急である。
- `stale_after` は期限が今日(UTC)以前のものを、超過の大きい順に返す。
  **verify では空にならない** — 書き手の宣言をサーバーが書き換えることに
  なる。解消は期限を引き直す編集である。
- `edited` は台帳に行があり、立っている行が無い concept。MCP には出さない。
- `sort=verified_at` は確認が古い順のランキングで、キューではない(件数は
  ベースの大きさそのもので、0 にできない)。
- `sort=usage` と `sort=failed` は**直近 90 日の数**で並び、生涯累積は同点を
  割る。窓は `usage.recent` としてワイヤに出る。
- `source=` は `sources[].resource` の完全一致で引く逆引きである
  (フィードではない)。

## 利用測定

- 検索のヒットと取得は**メモリにバッファし、5 秒ごとに書く**。読み取りの
  経路に書き込みを載せない。best-effort で、上限 20,000 件を超えた分は
  捨ててログに残す。read-only でも記録する(サーバー自身の観測なので)。
- **結果報告(`report_outcome`、REST `POST /api/v1/usage/{id}`、CLI
  `ochakai report`)は同期的に書く**意図的な書き込みで、`worked` /
  `failed` と一言の note を持つ。`GET /api/v1/usage/{id}` は note 付きの
  報告を新しい順に 10 件まで返す(REST・CLI・Web UI の詳細ページ)。
- 生のイベントは **180 日で刈り**、合計は残る。180 日は他の窓(90 日、
  `stats` の `days` の上限)の根になっている。

## 答えられなかった問い(ミス)

**どの concept の言葉にも一致しなかった検索**をミスとして記録する。
lexical の一覧が空だったかで決め、ベクトルは見ない(床が無く、必ず何かを
返すので)。

- 記録はサービス層の検索の一か所で、どの面の問いも数える。一覧(問いの
  無いもの)は数えない。呼び出し元に返る順位は変わらない。
- 打たれた文字列の先頭 500 バイトを 180 日保存し、累計を持たない。
- `OCHAKAI_RECORD_MISSES=false` で止まる(既定 on)。`public` と `sandbox`
  は記録しない(識別しない相手の文字列を集めない)。
- 意味では答えられたが言葉が一致しなかった問いもミスに入る。直し方は
  concept を書き足すか、既存の concept に `synonyms` を足すことで、どちらかを
  決めるのは読む人である。

## `GET /api/v1/stats`

インスタンスを一回で答える。窓 `days`(既定 30、上限 180、超えは 400)を
取り、**いまの姿**と**窓の中で起きたこと**を分けて返す。

| いまの姿 | 窓の中 |
|---|---|
| `concepts.total` / `.status` / `.trust` | `concepts.created` |
| `queues.drafts` / `.failed` / `.stale_after` / `.edited` | `review.verifications` / `review.weekly` |
| `misses.recording` | `outcomes.worked` / `.failed` / `misses.count` / `.queries` |

- キューの数は、そのキューを一覧するフィードと同じ述語で数える。
- `status` と `trust` は語彙の全値を 0 込みで返す。
- `review.weekly` は検証の台帳から数えた、今日から遡る 7 日ずつ 8 本
  (古い順)。誰も検証していないベースには出さない。Web UI のレビュー
  ページに軸の無い小さな図として出る。
- `misses.recording` が false のとき、`count` と `queries` は無意味である
  (CLI は `-`)。`queries` は最大 10 件。
- `prefix` はミス以外のすべてを絞る(ミスには属する id が無い)。範囲を
  持つ呼び出し元は自分の範囲を数え、答えの `scope` がそう言う
  ([identity.md](identity.md))。範囲を持つ呼び出し元には `misses` を
  返さない。
- ほかに、このデプロイについて読む人に要ること — `embedding.vectors` /
  `.truncated`([search.md](search.md))、`files`、`sandbox` — も載る。
- ロールアップを持たず、すべてその場で数える。

**押すのは数である。** `ochakai stats --exit-code` は、どれか一本でも
キューが空でない間 **2** で終わる(1 は失敗)。届けるのは運用者の cron か
CI で、ochakai は通知もスケジューラも持たない。

## エージェントの turn と、比較の問い

デプロイ自身のエージェントの会話(turn)は、ループの入力として形だけを
180 日残し、答えへの 👍/👎 は答えが使った concept への人の結果報告に
なる([architecture.md](architecture.md))。

- ochakai 以外のエージェントも `POST /api/v1/agent/turns` で turn を残せ、
  `GET /api/v1/agent/turns` で読める(読めるのは本人と管理者)。
- 人が「比較に使う」とした問いは、比較の問いのセットになる。`ochakai eval`
  はそれを `dry_run` のエージェントで再生し(何も書かず、何も数えない)、
  読んだ concept と、SQL の結果の行が元の答えと一致するかを言う。SQL は
  打った人の手元で本人として走る。`--exit-code` で回帰が 2 になる。
- 問いのセットは export に載らない(ナレッジではなくループの測定なので)。

## やらないこと

- 自動の降格、status による出し分け、順位の減点。
- 配送・スケジューラ・閾値・抑制・時系列 API・キューの深さの時系列。
- 窓や形の長さの設定、減衰する列。
- ミスのフィード・検索・エクスポート・人ごとの内訳、報告者での逆引き
  (監視の道具になる)。ミスからの自動生成や名寄せ(LLM が要る)。
- 検索の後に何も取得されなかったことをミスとすること(呼び出しをまたぐ
  状態が要り、MCP は stateless である)。
- 👍 を検証にすること。

## 経緯

設計記録 [0141](../design/0141-a-miss-is-read-off-the-words.md)・
[0068](../design/0068-how-a-face-is-added-and-removed.md) §4・
[0135](../design/0135-a-rejection-is-a-deletion.md)・
[0142](../design/0142-ochakai-carries-a-data-agent-that-does-not-rule.md) §6・
[0144](../design/0144-a-turn-is-kept-whoever-answered.md)・
[0146](../design/0146-a-kept-question-is-replayed-where-the-person-runs-it.md)、
決定 [0155](../decisions/0155-export-the-verifications-that-stand.md)。
利用者向けの説明は [docs/loop.md](../loop.md)。
