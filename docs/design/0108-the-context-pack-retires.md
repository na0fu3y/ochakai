# ochakai 設計ドキュメント 0108: context pack は退役する

Status: Accepted(2026-08-16)。**BREAKING(REST 非コア・MCP・CLI)。**
[0093](0093-the-budget-governs-the-whole-response.md) を Superseded に
する — pack が無くなれば、その予算の規則も無くなる。
[0067](0067-four-faces-and-what-they-decline.md) §1・§4・§5.1 の
`get_context` を名指す行と、
[0096](0096-a-listing-is-not-a-search-here-either.md) §3(hint の
置き場所)を改訂する。[0070](0070-what-was-retired-and-why.md) §3 の
「compile_sql の答えは `get_context` から届く」という理由づけの綴りは、
search → get に読み替える(結論は動かない)。前提は
[0106](0106-a-read-carries-what-points-at-it.md)(単読の完全性)と
[0107](0107-the-freeze-holds-the-okf-core.md)(`/context` はコアの外)。
Date: 2026-08-16

## 0. この記録が決めたこと

1. **pack は全面から同時に退役する。** `GET /api/v1/context`、MCP
   `get_context`、`ochakai context` — 検索 + 上位の全文 + リンク一 hop +
   バイト予算という合成は、どの面にも残らない。
2. **エージェントの読みは search → get である。** pack が同梱していた
   逆向きの hop は、concept 自身の `linked_from`(0106)が運ぶ。
3. **書き戻しの hint は `get_concept` の応答に移る**(§3)。
4. **フックはポインタ注入器になる**(§4)。`ochakai search --json` の
   順位を注入し、取りに行くのはエージェント自身である。

## 1. 通行量はあった — 降ろす根拠は別である

[0076](0076-two-tools-leave-mcp.md) がツールを降ろした基準(通行量が
無い)は、ここでは使えない。同梱のフックも配布 CLAUDE.md も docs/loop.md
も、全部 pack を通っていた。降ろす根拠は逆向きである: **pack の前提の
ほうが古びた。**

pack は「往復はエージェントにとって高価である」という前提の機構である。
一回で全部を返すために、何を全文で渡し、何を名前だけにするかを**サーバが
先に決める** — greedy packing、outline、一位だけの excerpt(0093)、
二方向の hop。エージェントが search → get の反復を自前で回せるように
なった今、その判断はサーバが持つ理由を失い、むしろ取り上げている。
「LLM を持たない」([0081](0081-what-ochakai-is-and-what-it-refuses-to-hold.md))
この製品で、何を読むべきかの判断はいちばん LLM に近い仕事であり、
それは呼び出し側にいる。

pack で最も動いていた部品が、最も仮説含みだったことも同じ事実の別の
面である — 0093(予算の意味)は退役の 6 日前に直っている。

## 2. 需要シグナルは、選ばれた fetch のほうが正直である

pack には「丸ごと配られた concept だけを fetched に数える」という規律が
あった(outline は数えない)。パッキングがサーバ内で起きるから守れた
規律である。退役後は数える対象そのものが消える: **エージェントが自分で
選んで叩いた get が fetched になり**、レビューフィードを駆動する需要
シグナルは、押し付けられた配達ではなく意図された読みを数える。
`linked_from` の行が fetched に数えられないこと(0106 §3)が、この
規律の残る半分である。

## 3. hint の置き場所(0096 §3 の改訂)

書き戻しの習慣を運ぶ一文([0069](0069-the-loop-and-what-measures-it.md))
は pack の応答に乗っていた。乗り物が退役するので、**知識が実際に配達
される場所 — `get_concept` の応答 — に移す**。常駐ではなく応答ごとで
あることは変わらず、outline や excerpt を指していた可変部分は消えて
一文の定数になる。instructions を落とすホストへの最後の辺、という
0096 §3 の理由はそのまま生きている。

## 4. フックと、置き換えの形

同梱の recall フック(UserPromptSubmit)は pack を丸ごと注入していた。
これからは `ochakai search --json` の順位 — id・型・説明・trust の行 —
を注入し、**fetch はエージェントの判断**として起きる(MCP の
`get_concept` か `ochakai get`。取りに行けば `linked_from` が付いて
くる)。注入されるバイトは pack の既定 12,000 から行数×百バイト強へ
一桁下がり、`OCHAKAI_RECALL_BUDGET` は行数の `OCHAKAI_RECALL_LIMIT` に
替わる。

## 5. 面への出方(0067)

| 面 | 何が変わるか |
|---|---|
| REST | `GET /api/v1/context` が消える。0107 でコアの外なので、凍結の指紋は動かない。`budget` は語ごと消える(PARAM 19 → 18) |
| MCP | `get_context` が消える(7 → 6)。常駐 13,460 → 11,629 バイト、`MCP-BYTES` は 12,000 へ**下がる**。改名ではないので [0088](0088-a-retired-name-answers-for-one-release.md) の窓は無い |
| CLI | `ochakai context` が消える(25 → 24、`--budget` も)。読みの完全性は search / get / list / browse が持つ |
| Web UI | 変わらない — pack は元から載せていない(0067 §5.4) |

## 6. 互換性

**BREAKING で、猶予は無い。** REST の `/context` は 0107 が不安定な面に
戻した後の削除で、MCP・CLI は元から 0.x で不安定である
([docs/compatibility.md](../compatibility.md))。呼んでいたクライアントの
移り先:

- `get_context(query)` → `search_concepts(query)`、読む価値のある hit を
  `get_concept`。caveat は取った concept の `linked_from` に載っている。
- `GET /api/v1/context?q=&budget=` → `GET /api/v1/search?q=` と
  `GET /api/v1/bundle/{id}.md`。応答の大きさは `limit` と行の上限が縛る。
- `ochakai context "<question>"` → `ochakai search "<question>"`、
  `ochakai get <id>`。フックは §4 の形に書き直した。

## 7. やらないこと

- **縮小版の pack を残すこと。** 「hit に本文の冒頭を足す」「get に
  companions を同梱する」は、pack を別の名前で作り直すことである。
  二本目の入口は二本目の説明を持ち、そちらだけが腐る
  ([0068](0068-how-a-face-is-added-and-removed.md) §1)。
- **`budget` を search に移すこと。** 予算は「全文を配る応答」の語彙で
  あり、順位の応答に要らない。
- **0088 の窓を作ること。** 改名ではなく削除である(0076 §6 と同じ)。
- **Web UI に読みの面を足すこと**(0067 §5.4 のまま)。

見直す条件: シェルも MCP クライアントの反復もできないホストの
エージェントが、一回の HTTP 呼び出しで「読むべきもの一式」を要すると
実際に詰まった一件が出たとき。そのときの出発点は 0093 までの pack の
設計であり、置き場所はコアの外である。
