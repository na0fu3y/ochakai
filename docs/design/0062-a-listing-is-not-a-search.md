# ochakai 設計ドキュメント 0062: 一覧は検索ではない

Status: Accepted(2026-07-31)。**BREAKING。**
`ochakai search` の `--sort` と `--cursor` を廃し、一覧を
`ochakai list [feed]` に分ける — `--sort` の 4 値は
`ochakai list usage | verified_at | failed | stale_after` の引数になり、
クエリ無しの `--source` / `--links-to` も `ochakai list` に移る。
`ochakai search` はクエリを必須にする。
[0056](0056-one-question-one-command.md) の「能力が一つならコマンドも
一つ」に、その逆向き — **一つのコマンドが二つの能力を持つなら、
コマンドも二つ** — を足す。`ochakai stats` がキューの横に印字する
コマンド文字列も `ochakai list …` に変わる([0049](0049-queue-counts.md)
§3.4・[0059](0059-a-queue-is-named-by-its-listing.md) の印字形式を改訂
する)。**ワイヤは一行も動かない** —
`GET /api/v1/search?sort=` も MCP の `search_concepts` もそのままで、
記録される内容・台帳の形・語彙(`sort.*` の 4 語)も変わらない。
Date: 2026-07-31

## 1. 問題: 一つのコマンドが、二つの規則で動いていた

[0050](0050-listings-page-rankings-do-not.md) は、一覧と順位を**別物
として**決めた。一覧は全順序なのでページングでき、順位は関連度の順な
のでページ二枚目に意味がない。決定は正しく、本ドキュメントは一歩も
動かさない。動かすのは、その二つが CLI では**一つのコマンド**に載って
いたことのほうである。

`ochakai search` の実際の規則を並べるとこうなる。

| | `--sort` 無し | `--sort` あり |
|---|---|---|
| 何を返すか | 関連度の順位 | 全順序の一覧 |
| `--limit` の既定 / 上限 | 10 / 50 | 100 / 1000 |
| `--cursor` | 無意味(渡せる) | 進み方そのもの |
| 先頭カラム | スコア | フィードの数か日付 |
| クエリ | 必須 | **禁止**(併用はエラー) |

**五つの行のうち、四つで意味が違う。** 同じコマンドの同じフラグが、
別のフラグの有無で別の意味になる。

さらに、クエリを省いたときの規則が二つあった。

- `ochakai search --source <uri>`(クエリ無し)は順位付けをやめ、
  住所順の一覧になる。`--links-to` も同じ。
- どれも無しでクエリも無いときだけエラーになる。

つまり `search` は、**引数の有無で自分の意味を三通りに変えていた**。

その代金はヘルプに出ていた。`ochakai search -h` はフラグ一覧に届く前
に **30 行の散文**を置いており、その 20 行は「`--sort` を渡したときに
このコマンドは何になるか」の説明である。[docs/surface.md](../surface.md)
の FLAG の節は `ochakai search` が 14 フラグ持つことを「REST の契約
全体(11 操作)より多い」と書いていたが、**14 のうち 2 つは他の 12 の
意味を変えるフラグ**だった。

そして [docs/surface.md](../surface.md) 末尾の「次の逃げ道」は、この形を
名指ししていた。

> 4 つのモードは 4 語と数えられるようになったが、**それぞれが別の
> limit 既定値と別の cursor 可否を持つことはまだどこにも数えられて
> いない。**

数えられないのは、**それが一つのコマンドの中に隠れていたから**である。

## 2. 世界観: 0056 の系の、書かれていなかった半分

0056 §2 はこう書いた。

> **能力が一つなら、コマンドも一つでよい。** 二本目が買うのは
> capability ではなく convenience である。

これは真だが、片側しか言っていない。もう半分はこうである。

> **能力が二つなら、コマンドも二つである。** 一本に押し込むと、
> 消えるのはコマンド数だけで、利用者が覚える規則は消えない —
> フラグのヘルプに移る。

0056 が `backlinks` を畳んだのは、`backlinks <id>` と
`search --links-to <id>` が**同じ問い**だったからである。`search` と
`search --sort usage` は同じ問いではない。前者は「revenue に一番近い
知識は」、後者は「レビュー待ちのドラフトはどれだけ溜まっているか」で
ある。**翻訳表を持たされる**という 0056/0054 の基準で言えば、いま一行
持たされているのは「`search` と打つが検索ではない」のほうである。

0015 §3.1 の「CLI は完全性の面」も、これを禁じていない。0056 §2 が
書いたとおり完全性とは**能力**の完全性であって、REST 操作との一対一で
はない。REST の `GET /api/v1/search?sort=` は一本のままで、CLI が二本
になるのは面の重複ではなく、**一本が二役だったのをほどいた**結果で
ある。

## 3. 決定

### 3.1 `ochakai list [feed]`

`--sort` の 4 値が位置引数になる。

```
ochakai search --sort usage --status draft   →  ochakai list usage --status draft
ochakai search --sort verified_at            →  ochakai list verified_at
ochakai search --sort failed                 →  ochakai list failed
ochakai search --sort stale_after            →  ochakai list stale_after
ochakai search --source <uri>                →  ochakai list --source <uri>
ochakai search --links-to <id>               →  ochakai list --links-to <id>
```

`list` は `--cursor` を持ち、`--limit` の既定は 100(上限 1000)。
フィードを渡さないときは `--source` か `--links-to` が要る — 住所順で
答えが集合になるのはこの二つだけで、他のフィルタは**一覧を狭めるが、
一覧を作らない**。

### 3.2 `ochakai search <query>`

クエリが必須になる。`--cursor` は消える(順位にページ二枚目はない、
0050)、`--limit` の既定は 10(上限 50)。フィルタ 9 つは両方のコマンド
に同じ意味で載る。

### 3.3 語彙は増えも減りもしない

`verified_at` / `usage` / `failed` / `stale_after` は
[docs/surface.md](../surface.md) の VOCAB に `sort.*` として 4 語ある。
本ドキュメントはその 4 語を**置き場所だけ変える** — フラグの値から
コマンドの引数へ。覚える語は同じで、これは 0059 が数えたのと同じ基準で
ある(**一つのものが二つの名前を着ていたら 1**、逆もまた同じ)。

ワイヤの `sort=` はそのままなので、REST・MCP・Web UI は一行も変わら
ない。

### 3.4 `stats` の印字

0049 §3.4 は、キューの数の横に**それを一覧するコマンド**を印字すると
決めた。0059 はそのコマンドがキューの名前を決めると書いた。どちらの
決定も動かさず、印字される文字列だけが変わる。

```
drafts	12	ochakai list usage --status draft
failed	1	ochakai list failed
stale_after	0	ochakai list stale_after
```

## 4. 表面

- **CLI 25 → 26。** 天井を上げる決定であり、そう言って上げている。
- **FLAG 29 → 28。** `--sort` がフラグでなくなった。
- **VOCAB 36 のまま。** §3.3。
- **REST・PARAM・HEADER・MCP・ENV 変化なし。** ワイヤは動かない。
- **DOC 25 のまま、DOC-LINES 5,700 → 5,760。** [cli.md](../cli.md) が
  9 つのフィルタを二度書く。**コマンドを割ることの代金は参照文の重複**
  で、これは今まで数えられていなかった。

数の上では ±0 に近い(CLI +1、FLAG −1)。買っているのは数ではなく、
**どちらのコマンドの `-h` も一つの仕事しか説明しない**ことである。
`search -h` の前置きは 30 行から 9 行になった。

## 5. やらないこと

- **REST を割らない。** `GET /api/v1/search?sort=` は一本のまま。
  0046 §3.5 と 0055 が畳んだものを戻す提案ではない — ワイヤで一覧と
  順位が同じ操作なのは、**フィルタが完全に共通で、応答の形も同じ**
  だからである。分けたいのは、人が打つときの規則のほうである。
- **`browse` を畳まない。** 階層を一段だけ見る操作(0014・0017)で、
  一覧でも順位でもない。
- **`--sort` を deprecation 期間つきで残さない。**
  [compatibility.md](../compatibility.md) のとおり 0.x に非推奨期間は
  無く、`ochakai search --sort usage` は「そんなフラグは無い」で落ちる。
  移行は上の表が全部である。
