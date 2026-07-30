# ochakai 設計ドキュメント 0061: dry run は書き込みを止めたものであって、別の読み方ではない

Status: Accepted(2026-07-30)。`PUT /api/v1/bundle/{path}` に
`dry_run` クエリパラメータと `Ochakai-Plan` レスポンスヘッダを足す
([0046](0046-bundle-address-space.md) §3.5 の書き込み面を改訂する。
判定そのもの — 何を claim として残すか、何を unchanged とするか — は
0046 §2.2・§3.4 のままで、**変えるのは誰がそれに答えるかだけ**である)。
[docs/surface.md](../surface.md) の PARAM を 18 → 19、HEADER を 9 → 10
に上げる。`ochakai import --dry-run` はこのパラメータを使うようになり、
`--dry-run --strict` は本番 import と同じ判定に到達する
Date: 2026-07-30

## 1. 問題: 関門が通り、本番が落ちる

`ochakai import --dry-run --strict` は CI の関門として文書化されていた
(`--strict` のヘルプが「Parse-time ones are found before anything is
written, so a strict import either lands whole or writes nothing」と
書いていた)。実測されたのは次である:

| バンドル | `--dry-run` | 本番 import |
|---|---|---|
| `verified:` を持つ | 0 notes、exit 0 | **1 notes**(`received` 隔離) |

関門が緑で、本番が赤い。**偽グリーンである。** CI が dry run を関門に
している構成では、落ちるべき同期が素通りする。

### 1.1 なぜ dry run に見えないのか

`--dry-run` は**バンドルをパースして、書くつもりのものを並べる**だけ
だった。ところが `--strict` が落ちる理由の半分は、パース結果からは
決まらない:

- **trust family が claim として残るかどうか。** 文書が持つ
  `generated:` / `verified:` / `created_by:` は `received` の下に移され
  る(0046 §2.2)。それが**このインスタンス自身の観測が戻ってきたもの**
  なら落とされ、note は出ない。判定は
  `okf.IsObservation(claim, 保存済みの concept)` — つまり**保存されて
  いるものとの比較**であり、バンドルだけを見ても答えられない。
- **サーバがその文書を拒むかどうか。** 書き込み時 validation で 400 に
  なる concept は skip として数えられ、`--strict` を落とす。
- **書き込みが何も変えないかどうか。** 保存済みのバイト列との比較であり
  (0046 §3.4)、これもバンドルの外にある。

`--dry-run` が並べていたのは「パースできた」ことだけで、`--strict` が
判定するものの半分を見ていなかった。ヘルプの一文はその半分について
真ではなかった。

### 1.2 ついでに答えていなかったこと

同じ穴の裏側として、dry run は **created / updated / unchanged を
出し分けられなかった**。本番 import は
`0 created, 27 updated, 39 unchanged` と出すのに、dry run は全件
「would import」だった。**再同期こそ dry run が要る場面**である —
39 件が unchanged だと分かることが、その run を見る理由である。

## 2. 選択肢

### 2.1 クライアント側で作り直す(却下)

CLI が concept ごとに保存済みの姿を読み、claim の落ちる条件・unchanged
の条件を**自分で再現する**。表面は 1 本も増えない。

却下した。増えないのは表面だけで、増えるのは**書き込み経路の判断の
第二の実装**である。[0007](0007-api-only-cli.md) が「CLI は REST の
薄いクライアントである」と決めているのは、まさにこの種の重複を作らない
ためである。しかも再現は正確にならない — `observed.generated.at` は
行の更新時刻を返し、export 形の `generated.at` は内容の変更時刻
(0046 §3.4)であって、両者は reformat のあとでずれる。**推測に推測を
重ねた関門**は、直そうとしている偽グリーンと同じ種類のものである。

### 2.2 `--strict --dry-run` を「答えられない」として断る(却下)

正直ではある。しかし利用者が失うのは、この文書が守ろうとしている当の
機能である。**答えられないと言うより、答えるほうが小さい。**

### 2.3 書き込み経路に「書かない」と言う(採用)

サーバだけが答えを持っているのだから、サーバに聞く。同じ検証、同じ
読み取り、同じ規則、同じ note — そして書かない。

## 3. 決定

### 3.1 `PUT /api/v1/bundle/{path}?dry_run=true`

書き込み面のパラメータであって、第二の面ではない。同じ body、同じ
precondition、同じ 400 / 403 / 501、同じ `Ochakai-Note`。違うのは
**行が書かれないこと**と、答えが `Ochakai-Plan` で返ることだけである。

- 応答は常に **200**。何も作られていないので 201 は返さない。作られて
  いないものに 201 を返すのは、汎用クライアントに対する嘘である。
- **ETag は付けない。** 書かれていないのだから precondition に使える
  version は存在しない。
- DELETE では **400**。delete に「何が起きるか」は無く、パラメータを
  黙って無視すれば**聞いたつもりの呼び出し元のオブジェクトが消える**。
  安全な読み方は拒否だけである。

**ヘッダではなくクエリパラメータである理由**: proxy に落とされた
ヘッダは dry run を**本番の書き込みに変える**。落ちると壊れるより、
落ちると壊れないほうを選ぶ。URL は落ちない。

### 3.2 `Ochakai-Plan: created | updated | unchanged`

dry run の答えは三値で、`Ochakai-Unchanged` はそのうち一つしか言えない。
三値を二つのヘッダに割ると、クライアントは片方しか読まなくなる。だから
dry run では `Ochakai-Unchanged` を**出さず**、`Ochakai-Plan` が
置き換える。本番の書き込みには `Ochakai-Plan` を付けない — 起きたことを
「起きるはずのこと」の語で言い直す必要はない。

ヘッダを知らないサーバへの dry run は**エラーである**。0.16.1 以前の
サーバは `dry_run` を無視して書き込むので、`Ochakai-Plan` の不在は
「きれいだった」ではなく「書いてしまったかもしれない」を意味する。
クライアントはそれを沈黙ではなく失敗として扱う。

### 3.3 判定は一つの経路から出る

`Service.Plan` は `Service.Put` の判断部分(`settleUpdate`)を**共有
する**。別に書き下した plan は第二の意見であり、二つはいずれずれる。
共有するのは:

検証(`validate`)、保存済みの読み取り、precondition、claim を落とすか
の判定(`okf.IsObservation`)、status の既定、台帳の引き継ぎ、そして
保存されるバイト列の比較(`store.StoredDocument`)。ここまでが
`settleUpdate` で、`Update` はその先で書き、`Plan` はその手前で返す。

ファイルも同じ形にする(`settleFile` / `PlanFile`)。concept の plan が
聞かなくてよい問いが一つだけファイルにはある — **その住所を concept が
持っていないか**で、書き込みの中でしか判明しなかった。type キーを失った
markdown ファイルを積んだバンドルが dry run を通って import で落ちる、
という同じ穴なので、ここで聞く。

### 3.4 `ochakai import --dry-run` は plan を使う

出力は本番 import と同じ語彙で並ぶ:

```
would create ochakai://metrics/revenue
would update ochakai://metrics/arr
would leave unchanged ochakai://insights/seasonality
dry run: 66 concepts (0 created, 27 updated, 39 unchanged, 0 attachments, 3 files, 0 skipped, 1 notes)
```

`--strict` はこの数え上げに対して働き、**何も書かずに本番と同じ判定に
到達する**。パース時点の note と skip は今までどおり先に落とす(サーバ
に 66 回聞く前に落ちるほうが速く、判定は同じである)。

### 3.5 やらないこと

- **一括の plan エンドポイントは作らない。** 既存のエンドポイントの
  ループで足りるものに二つ目の経路を作らないのは
  [0015 §3.2](0015-surface-consistency.md) が決めている。import は今も
  concept ごとに 1 回呼んでおり、dry run もそうする。
- **MCP には載せない。** ツールスキーマはエージェントのコンテキストから
  支払われる(0015 §3.1)。dry run はバンドル同期を関門に通す人のもので
  あって、1 件を書くエージェントのものではない。
- **read-only インスタンスでも 403 のままにする。** dry run は「この
  書き込みは何をするか」を聞くものであり、書けないインスタンスの答えは
  「書けない」である。

## 4. 表面の代金

PARAM 18 → 19、HEADER 9 → 10。**天井を二つ上げる決定であり、そう言って
上げている。**[docs/surface.md](../surface.md) の三つの問いに対して:

1. **誰が詰まったか。** `--dry-run --strict` を CI の関門にしていた
   利用者が、関門を通ったバンドルで本番 import を落とした(§1)。
2. **既存の面で回避できるか。** できない。判定の半分はサーバのもので
   あり、クライアント側の再現は §2.1 のとおり不正確で、しかも 0007 が
   避けている重複そのものである。
3. **何が畳めるか。** 何も畳めない。増えるのは、**書き込み面が既に
   持っていた判断に問い合わせる口**であって、新しい能力ではない —
   `dry_run` が答えられることは、すべて書き込みが既に答えていたことで
   ある。単調に増えてよい理由はそこにある: 面は増えるが、ochakai が
   知っていることは 1 つも増えていない。
