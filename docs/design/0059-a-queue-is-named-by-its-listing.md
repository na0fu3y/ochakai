# ochakai 設計ドキュメント 0059: キューは、それを一覧する sort の名で呼ぶ

Status: Accepted(2026-07-30)。**BREAKING。**
`GET /api/v1/stats` の `queues` の 2 キーを改名する —
`reported_wrong` → `failed`、`past_expiry` → `stale_after`。
[0049](0049-queue-counts.md) §3.3 が付けた名前を改訂する(数える集合も
数え方も、キューの本数も変えない)。`drafts` は据え置く。
[docs/surface.md](../surface.md) の VOCAB の数え方に 1 行足す —
**sort の名を着たキューは第二の語ではない**ので 1 と数える
Date: 2026-07-30

## 1. 問題: 同じ一つのキューが、二つの名前を持っていた

[0049](0049-queue-counts.md) は三つのフィードを*一覧する*代わりに*数える*
読み取りを足し、数えた結果に `drafts` / `reported_wrong` / `past_expiry`
という名前を付けた。フィードのほうは既に名前を持っていた — `sort=usage`
(+ `status=draft`)・`sort=failed`・`sort=stale_after` である。

結果として、**キュレーターは同じ一つのものについて二つの綴りを覚える**
ことになった。`ochakai stats` の出力がそれをそのまま印字している:

```
reported_wrong	1	ochakai search --sort failed
past_expiry	0	ochakai search --sort stale_after
```

各行は「この数がいくつで、中身を見るには次にこう打て」と言っている。
**左の語と右の語が違う。** 行そのものが、二つの名前が同じものを指して
いる証拠になっている。

これは [0056](0056-one-question-one-command.md) が CLI について片付けた
のと同じ形の重複である。0056 §1 は「`queues` と `stats` は同じ三つの数を
違うキーで印字していた」ことを、第二の面が高くついていた証拠として挙げた
— そのとき畳んだのはコマンドであって、**キーの綴りはそのまま残った**。

### 1.1 数える仕組みは、この重複を見なかった

[docs/surface.md](../surface.md) の VOCAB は語を `族.値` で数える。
`queue.reported_wrong` と `sort.failed` は別の族の別の綴りなので 2 語で
あり、**改名しても数は動かない** — `queue.failed` と `sort.failed` は
やはり 2 語だからである。

つまりこの改名は、数える仕組みの上では「何も起きていない変更」に見える。
VOCAB がそもそも足されたのは、[0054](0054-concept-is-the-okf-word.md) の
「本数は動かないが、利用者が知る語が変わった」を見えるようにするため
だった。**その次元が、同じ種類の変化をもう一つ見落としていた。**

## 2. 決定

### 2.1 キューは、それを一覧する sort の名で呼ぶ

`reported_wrong` → `failed`、`past_expiry` → `stale_after`。

方向はこちらである(sort を改名してキューに合わせるのではない)。理由は
**sort の名前は既にある語を使い回しており、キューの名前は発明語だった**
から:

- `failed` は呼び出し元が報告する outcome の値である(`report_outcome`)。
  報告する語・その報告が溜まったフィードの語・その本数の語が、一つに
  なる。`reported_wrong` は**その三つ目としてだけ存在する語**だった。
- `stale_after` は OKF SPEC §5 の frontmatter キーそのものであり、
  数えている日付の出どころである。`past_expiry` はその日付に付けた
  ochakai だけの別名だった。

[0057](0057-concept-is-the-word-a-reader-meets.md) と
[0054](0054-concept-is-the-okf-word.md) が知識の単位について適用した規則
— **読む人が既に出会っている語を使う** — の、キューへの適用である。

### 2.2 `drafts` は据え置く

`drafts` は単一の sort ではない(`sort=usage` + `status=draft`)。名前を
持たないものに sort の名を着せることはできず、`usage` と呼べば
「利用の多い順の一覧」全体と混ざる。**規則が当てはまらない一件を、規則の
形に合わせて曲げない。**

三つのうち二つが sort 名で、一つが自分の名前を持つのは非対称に見えるが、
その非対称は**実際にそうである** — 二つは一つの listing の本数で、一つは
listing にフィルタを掛けた先の本数である。

### 2.3 VOCAB は、sort 名を着たキューを 1 と数える

`docs/surface.md` の数え方に、既にある規則の裏面を足す。

いまの規則: **一つの綴りが二つの族で別の意味を持つとき、覚える語は 2 つ**
(`failed` は outcome の値であり一覧モードでもある — 報告する行為と、
一覧を求める行為は別である)。

足す規則: **一つのものが二つの名前を着ているとき、覚える語は 1 つ。**
`stats` が数える集合と `sort=` が並べる集合は同じ一つなので、キューは
自分の語を持たない。VOCAB は 38 → 36 になる。

実装は `cmd/ochakai/surface_test.go` で、`QueueCounts` の json キーのうち
`domain.ListSorts` に無いものだけを数える。**キューが sort でない名前を
また持ったら、その瞬間に語として数に出る** — 規則が守られていることを
覚えている必要は無い。

## 3. 選ばなかった案

### 3.1 sort を改名してキューに合わせる

`sort=reported_wrong` / `sort=past_expiry` にする案。語数の減り方は同じで
ある。採らないのは §2.1 の理由 — sort 側の綴りは outcome と OKF のキーを
使い回しており、そちらを捨てると**使い回していた分の重複が復活する**
(`failed` を報告して `reported_wrong` を一覧することになる)。加えて
`sort=` の値は REST・MCP スキーマ・CLI のフラグ値・Web UI の URL に載って
おり、`queues` の応答キーより広い。

### 3.2 両方を残し、片方を別名として受ける

応答に 5 キー並べる案。**二つの名前を覚える問題に、二つの名前を残す解**
であり、この文書が問題にしているものそのものである。

### 3.3 `drafts` も何かの sort 名にする

§2.2 のとおり、当てはまる sort が無い。`sort=usage` を借りると、
status=draft の絞り込みが名前から消える。

### 3.4 何もしない

改名は BREAKING であり、買えるのは覚える語 2 つである。それでも採るのは、
**`ochakai stats` の各行が「左の名前で数え、右の名前で見ろ」と印字して
いる**からで、これは読む人が毎回払っている。0.16.1 時点でこの応答を
読んでいるのは同梱の CLI と Web UI(どちらもこの PR で追随する)であり、
外部の消費者が増えるほど改名は高くなる。**いま安い。**

## 4. サーフェス(0015)

| 面 | 変化 |
|---|---|
| REST | `GET /api/v1/stats` の `queues` の 2 キーが改名。操作数・パラメータは不変 |
| CLI | `ochakai stats` の行ラベルが `failed` / `stale_after` に。コマンドもフラグも不変 |
| MCP | 変化なし — `stats` は MCP に載らない([0051](0051-instance-metrics-and-search-misses.md) §4) |
| Web UI | バナーの読み出しキーが追随。表示文言(「reported wrong」「past expiry」)は人向けの散文なので変えない |

surface.md の REST・PARAM・MCP・CLI・ENV は動かない。**VOCAB だけが
38 → 36 に下がる**(上限も一緒に下げる)。

## 5. 互換性

**BREAKING。** `GET /api/v1/stats` の `queues.reported_wrong` と
`queues.past_expiry` を読んでいるクライアントは、値が `undefined` になる
(キーが消え、`failed` / `stale_after` が現れる)。0.x に deprecation
window は無い([compatibility.md](../compatibility.md))。

`ochakai stats --json | jq .queues.reported_wrong` を CI に書いていた
デプロイは `jq .queues.failed` に直す。`--exit-code` の挙動は不変で、
これは**キー名を読まない**問い方であり、そちらを使っていれば何もしなくて
よい。

数える集合・数え方・`Total()` の意味・キューの本数は変わらない。
