# ochakai 設計ドキュメント 0056: 一つの問いに一つのコマンド、一つの裁定に一つの語

Status: Accepted(2026-07-29)。**BREAKING。**
CLI から `ochakai backlinks` を廃し、`ochakai search --links-to <id>`
一本にする — [0046](0046-bundle-address-space.md) §3.5 が REST の
`GET /api/v1/backlinks/{id}` を畳んだときに残った、CLI 側の第二の
住所である。同じ理由で `ochakai queues` を `ochakai stats` に畳み
([0049](0049-queue-counts.md) §§3.4-3.5 を改訂する)、却下を取り消す
綴りを `withdraw` 一語にする([0055](0055-one-ruling-one-face.md) §3.4
の `--lift` と §5 が据え置いたリビジョン値 `unreject` を改訂する)。
四つとも 0.16.0 でリリース済みなので、どれも差し替えではなく改訂で
あり、新しい番号を取る([0048](0048-decision-records-for-wire-contracts.md)
§2.3)。
[0015](0015-surface-consistency.md) §2 の「CLI は完全性の面」に 1 行
足す — 完全性とは**能力**の完全性であって、REST 操作との一対一でも、
畳まれた面をコマンドとして保存することでもない。記録される内容・
台帳の形・ワイヤの形はどれも変わらない
Date: 2026-07-29

## 1. 問題: ワイヤを畳んだのに、CLI に旧い面が残っている

[0046](0046-bundle-address-space.md) §3.5 と
[0055](0055-one-ruling-one-face.md) は、REST の面を 19 → 14 → 11 に
畳んだ。どちらも「同じものに二つの住所がある」ことを理由にしている。

畳んだ後の CLI を数えると、こうなっていた。

| CLI コマンド | 同じ答えを返すもの | ワイヤ上の面 |
|---|---|---|
| `ochakai backlinks <id>` | `ochakai search --links-to <id>` | 無い(0046 §3.5 が畳んだ) |
| `ochakai queues` | `ochakai stats` | 無い(0049 §3.1 が畳んだ) |

どちらも**ヘルプ自身がそう書いている**。`backlinks` の説明は
「Same question as `ochakai search --links-to <id>`, which is where it
lives on the wire」であり、`stats` の説明は「The three queue numbers
are the ones `ochakai queues` prints」と読者を送り返していた。

第二の住所は無料ではなかった。

- **`backlinks --limit` のヘルプは嘘になっていた。** 「server default
  20, max 100」と書いてあるが、`/backlinks` が消えた後の実際の経路は
  `search` の一覧であり、既定は 100・上限は 1000 である。**二つ目の面
  は、二つ目の説明を持ち、そちらだけが腐る。**
- **`queues` と `stats` は同じ三つの数を違うキーで印字していた** —
  `reported wrong` と `reported_wrong`、`past expiry` と `past_expiry`。
  片方を grep する読み手には、これは別の語彙である。

そして裁定の綴りは、一つの行為に四つあった。

| どこ | 綴り |
|---|---|
| REST | `{"ruling": "withdrawn"}`(0055 §3.2) |
| CLI | `ochakai reject --lift` |
| Web UI | `Lift rejection` |
| リビジョンの `change` | `unreject` |

`withdrawn` は 0055 §3.2 が**選んだ**綴りである — 「読めてはいけない
ものが読めない綴りを選ぶ」と書いて DELETE を退けた。その判断は正しく、
本ドキュメントは一歩も動かさない。動かすのは、選んだ語を三つの面が
使っていないことのほうである。

## 2. 世界観: 「完全性の面」は能力の完全性である

[0015](0015-surface-consistency.md) §2 は CLI の既定を yes にしている。
その理由は No FDE(C4)であって、**REST 操作と一対一に並べることでは
ない** — 0049 §3.5 自身がそう書いている。

そこから素直に出てくる系がある。

> **能力が一つなら、コマンドも一つでよい。** 二本目が買うのは
> capability ではなく convenience であり、[docs/surface.md](../surface.md)
> の三つ目の問い(「既存の面で回避できるか」)の答えが「できる」なら、
> 足さないのが既定である。

これは [0054](0054-concept-is-the-okf-word.md) が語彙について書いた
ことと同じ形である。0054 は「二つの語彙は利用者に翻訳表を持たせる」
と書いて、ツール名 5 本を OKF の語に改めた。**コマンドも語彙である。**
`backlinks` と `search --links-to` を両方覚えている人は、翻訳表を
一行持たされている。

畳まないものもある。**`ochakai verify` と `ochakai reject` は 2 本の
ままである。** 0055 §3.4 がそう決め、本ドキュメントはそれを支持する
— ワイヤでは同じ操作の値違いだが、ターミナルで打つ人にとって
「検証する」と「却下する」は別の行為であり、`--ruling` を打たせるのは
CLI を REST の写像にすることである。**畳むのは能力が同じ二本であって、
行為が違う二本ではない。**

## 3. 決定

### 3.1 `ochakai backlinks` は廃止する

`ochakai search --links-to <id>` が同じ問いに答える。クエリ無しの
`--links-to` はランキングではなく**アドレス順の一覧**になり
(0037 §2.3 と同じ扱い)、`--cursor` で続きが読め、`--type` などの
フィルタと組み合わせられる — 廃止するほうができないことは無い。

```
ochakai backlinks metrics/revenue     →  ochakai search --links-to metrics/revenue
```

出力は 1 列増える(先頭のスコア列)。`--json` の形は `search` のもの
になる。

### 3.2 `ochakai queues` は `ochakai stats` に畳む

0049 §3.4 が `queues` に持たせた二つの性質は、**どちらも失わない**。

- **各行が次に打つコマンドを持つ。** `stats` のキュー 3 行が第 3
  フィールドにそれを持つようになる。キーと数は第 1・第 2 フィールドの
  ままなので、`cut -f2` も grep も従来どおり効く。

  ```console
  $ ochakai stats --prefix teams/growth
  …
  drafts          12  ochakai search --sort usage --status draft --prefix teams/growth
  reported_wrong   1  ochakai search --sort failed --prefix teams/growth
  past_expiry      0  ochakai search --sort stale_after --prefix teams/growth
  …
  ```

- **`--exit-code`。** `ochakai stats --exit-code` が、3 本のどれかが
  空でない間 **2** で終了する。1 が「コマンドが失敗した」であることも、
  既定が 0 であることも 0049 §3.4 のままである。

0049 §3.5 が `queues` を残した理由は「3 つの数と次に打つコマンドだけが
欲しい人に `stats` の全体を読ませる理由は無い」だった。読ませてはいない
— `ochakai stats --exit-code` は何も読ませずに終了コードだけを返し、
`ochakai stats | grep '^drafts'` は 1 行を返す。**支払っていたのは、
読む行数ではなくコマンド名と、そこにぶら下がる二つ目の説明と、
二つ目のキーの綴りだった。**

`ochakai queues` は 0.16.0 に乗っている。0.x の間は破壊的変更が minor
で入る([docs/compatibility.md](../compatibility.md))ので、畳むなら
いまである — 一つ上のリリースを跨ぐたびに、説明し続ける年数が増える。

### 3.3 却下の取り消しは `withdraw` 一語

| 面 | 旧 | 新 |
|---|---|---|
| REST | `{"ruling":"withdrawn"}` | 変更なし |
| CLI | `ochakai reject --lift` | `ochakai reject --withdraw` |
| Web UI | `Lift rejection` | `Withdraw rejection` |
| リビジョンの `change` | `unreject` | `withdraw` |

ワイヤの ruling は分詞(`verified` / `rejected` / `withdrawn`)、
リビジョンの `change` は動詞(`verify` / `reject` / `withdraw`)で
ある。二つの活用があるのは既にそうなっており、**語幹が割れていたのは
この一語だけ**だった。

`unreject` は 0055 §5 が「保存されたデータは 1 バイトも変わらない」と
書いて据え置いたものである。据え置きの理由は原理ではなく、その変更が
0055 の範囲外だったことである。ここで動かす:
マイグレーション `0034` が既存行の `change` を書き換える。
**書き換えるのは事実ではなく符号である** — 誰がいつ何を取り消したかは
一行も動かない。旧行を残す選択は、二つの綴りが並ぶ唯一の場所に、
二つの綴りを永久に残すことになる。

### 3.4 各サーフェス(0015)

- **REST = 変更なし。** 唯一の契約であり、`links_to=` も
  `GET /api/v1/stats` も `ruling: withdrawn` も既にある。今回の変更は
  すべてその**クライアント側**にある。応答スキーマで動くのは
  リビジョンの `change` の enum 1 語だけである。
- **CLI = 2 本減る**(27 → 25)。`--exit-code` が `stats` に移り、
  `reject` の `--lift` が `--withdraw` になる。
- **Web UI = ボタンのラベル 1 つ。** 叩く先も権限も変わらない。
- **MCP = 変更なし。** `backlinks` も `queues` も裁定も、元からツール
  ではない(0015 §3.1)。

## 4. 却下した代替案

- **`backlinks` を残し、ヘルプの `--limit` の記述だけ直す。** 数が
  減らない。腐ったのは説明だが、腐った理由は**二つ目の説明があった**
  ことなので、直しても次のずれを待つだけである。
- **`queues` を残し、`stats` 側の「`queues` を見よ」を消す。** 送り返し
  は症状であって病気ではない。三つの数が二箇所から二つの綴りで出る
  ことが病気である。
- **逆に畳む — `stats` を廃して `queues` を残す。** `stats` は
  0051 の instance metric 全体(検索ミス、フローの数)を運んでおり、
  キューはその一部である。小さいほうに大きいほうを畳むことはできない。
- **`ochakai stats --queues` のようなフラグで絞る。** コマンドは減るが
  フラグが増える。しかも「絞る」フラグは、絞らない既定を読む人には
  ただの覚えることである。3 行は全体の中にあってよい。
- **`unreject` を保存されたデータとして残し、新しい行だけ `withdraw`
  にする。** 履歴の中で綴りが日付で割れる。最悪の選択肢である。
- **綴りは `lift` に統一する(ワイヤを `lifted` に変える)。**
  0055 §3.2 が `withdrawn` を選んだ理由は「削除と読まれない綴り」で
  あり、`lift` にはその論拠が無い。**選ばれた語のほうへ揃える。**
- **何もしない。** どれも動いており、壊れてはいない。それでもやるのは
  0055 §4 と同じ理由である — 0.x の間だけが、説明を増やさずに畳める
  期間だからである。

## 5. 壊れるもの

- **`ochakai backlinks` は消える**(unknown command)。移行は 1 行:
  `ochakai search --links-to <id>`。
- **`ochakai queues` は消える。** `ochakai stats`(必要なら
  `--exit-code`)。0.16.0 に乗っていたので、これは破壊的変更である。
- **`ochakai reject --lift` は `--withdraw` になる。**
- **リビジョンの `change` の値 `unreject` は `withdraw` になる** —
  応答の enum と、マイグレーションで既存行の両方。この値で分岐している
  クライアントは 1 語を書き換える。
- シェル補完は 3 つとも同時に更新される(`ochakai completion`)。
- 保存された知識・台帳・エクスポート形式・信頼の導出は変わらない。

## 6. 関連

- [0015](0015-surface-consistency.md) §2 — CLI は完全性の面。維持する。
  本ドキュメントは「完全性は能力の完全性である」を明文化する。
- [0046](0046-bundle-address-space.md) §3.5 — 一つのオブジェクトに
  一つの住所。同じ物差しを CLI のコマンド名に当てた。
- [0049](0049-queue-counts.md) §§3.4-3.5 — キューの数え方と終了コード。
  中身は維持し、置き場所だけを `stats` に移す。
- [0054](0054-concept-is-the-okf-word.md) — 二つの語彙は翻訳表を
  持たせる。本ドキュメントはそれをコマンド名と裁定の綴りに当てた。
- [0055](0055-one-ruling-one-face.md) §§3.2・3.4・5 — 裁定は一つの面
  から。`withdrawn` の綴りの選択を支持し、残る三面をそこへ揃える。
- [docs/surface.md](../surface.md) — CLI は 27 → 25、上限も下げる。
