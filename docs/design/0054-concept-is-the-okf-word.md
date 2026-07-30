# ochakai 設計ドキュメント 0054: 知識の単位は concept と呼ぶ

Status: Accepted(2026-07-29)。**§3.5 の「MCP のみ」は
[0057](0057-concept-is-the-word-a-reader-meets.md) が補った** — 決めた
語を、利用者が読む英語の散文(README・docs・CLI のヘルプ・Web UI の
ラベル・ツール説明・OpenAPI の description・エラー文)にも広げる。
本ドキュメントの §3.4(識別子は射程外)はそのまま維持される。
MCP のツール名 5 本が持つ `knowledge` を、
OKF SPEC §2 が定義する語 `concept` に改める — `search_concepts` /
`get_concept` / `put_concept` / `delete_concept` / `get_concept_usage`。
リソーステンプレートの表示名も同じ語に揃える。**ツールは 8 本のまま**で、
引数・応答・スキーマ・能力はどれも変わらない。ツール名を設定に書いて
いるクライアントが壊れる
Date: 2026-07-29

## 1. 問題: 同じものに二つの名前が残っている

[0046](0046-bundle-address-space.md) §3.5 の畳み込みが終わった時点で、
**ワイヤ上に残る `knowledge` は MCP のツール名 5 本だけ**になった。

| 面 | `knowledge` を含む要素 |
|---|---|
| REST | 0(`/api/v1/knowledge` は `/search` に、`/knowledge/{id}` は `/bundle/{path}` に) |
| CLI | 0(元から) |
| MCP | **5**(`search_knowledge` / `get_knowledge` / `put_knowledge` / `delete_knowledge` / `get_knowledge_usage`) |

一方 OKF SPEC §2 は、バンドルの中の知識の単位を **concept** と定義して
いる — 「A single unit of knowledge within a bundle, represented as one
markdown document」。パスから `.md` を除いたものが **concept ID** で
ある。

そして**ochakai 自身が既にその語で書いている**。0046 §2 の表は
「**概念(concept)**」と書き、§3.5 は「概念かファイルかを決めるのは
frontmatter の `type` キー」と書く。`internal/restapi` のコメントは
"a concept at `<id>.md`" と書く。残っているのはツール名だけである。

> **同じものを、文書では concept と呼び、エージェントには knowledge と
> 名乗っている。**

## 2. 世界観: 綴りが割れたら OKF を採る — 既に二度した判断

これは新しい判断ではない。

- [0023](0023-okf-type-vocabulary.md) は内部スラグと OKF 表示名の
  二重語彙を廃し、型の値を OKF の綴りそのものにした。
- [0038](0038-type-vocabulary-realignment.md) は推奨型の語彙を OKF の
  証拠に合わせ直した。

どちらも「ochakai の語と OKF の語が割れたとき、OKF を採る」という同じ形
である。今回は**単位そのものの名前**で、二重語彙としては最後に残った
ものである。

理由は 0023 が書いたのと同じで、**二つの語彙は利用者に翻訳表を持たせる**
から。ochakai のバンドルを読む者は SPEC の語に必ず出会う。そこで
concept と呼ばれているものが、ツールでは knowledge と呼ばれていると、
覚えることが一つ増える。

## 3. 決定

### 3.1 改名する 5 本

| 旧 | 新 |
|---|---|
| `search_knowledge` | `search_concepts` |
| `get_knowledge` | `get_concept` |
| `put_knowledge` | `put_concept` |
| `delete_knowledge` | `delete_concept` |
| `get_knowledge_usage` | `get_concept_usage` |

**検索だけ複数形**である。`knowledge` は不可算名詞なので単複を問われ
なかったが、`concept` は可算で、検索は複数を返し残りは一つを扱う。
名前が数を偽らないほうがよい。

### 3.2 変えないもの

- **`get_context` / `report_outcome`** — どちらも concept を名指して
  いない。前者は問いに対する読み、後者は結果の報告である。
- **`get_attachment`** — 0046 が「添付」という概念を溶かしたので、この
  名前も古びている。だが**それは別の問い**(ファイルを何と呼ぶか)で
  あり、OKF はファイルをファイルと呼ぶ。本ドキュメントは知識の単位の
  名前に限る。
- **ツールの本数 8**、引数、応答、スキーマ、`ochakai://` の URI 形。
  改名であって、能力の変更ではない。

### 3.3 リソーステンプレートの表示名

`Name: "knowledge"` / `Title: "Knowledge entry"` も `concept` に揃える。
エージェントが `@` で参照するときに見える名前であり、ツール名と別の語で
あってよい理由がない。

### 3.4 `domain.Knowledge` は改名しない

Go の型名は 281 箇所にあるが、**利用者は内部パッケージを覚えなくてよい**
([docs/surface.md](../surface.md) の「数えていないもの」)。外形を 1
バイトも変えない機械的差分は、この決定の一部ではない。いつか触るとすれば
それ自身の理由でであって、この記録の射程ではない。

### 3.5 各サーフェス(0015)

**MCP のみ。** REST と CLI は既に `knowledge` を持たず(§1)、Web UI は
画面の語で、`ochakai://` の URI はパスである。1 面だけの変更になるのは、
他の面が先に畳まれたからである。

## 4. 反対の論拠と、採らなかった案

**いちばん強い反対は「`search_knowledge` はエージェントに『このサーバは
何屋か』を伝えている」**である。初見のエージェントが `search_concepts`
を見ても、OKF を知らなければ何の concept か分からない。`knowledge` は
その一語で用途を名乗る。

採らなかった理由は二つある。ツール名が担うのは**他サーバのツールと並べて
曖昧でないこと**であり(`internal/mcpserver` の冒頭がそう書いている)、
何屋かを伝えるのは **description** である — description は
「knowledge base」と言い続ける。そして OKF を知らないエージェントも、
ochakai のバンドルを読めば SPEC の語に出会う。二つの語を覚えさせるほうが
高い。

- **両方の名前を出す(別名)。** ツールが 8 本から 13 本になる。
  スキーマはエージェントのコンテキストから支払われるので本数は予算で
  ある(0015 §3.1)。互換のために予算を 6 割増やすのは、この
  プロジェクトが最も避けてきた形の支払いである。
- **`search_okf_concepts` のように出自を名前に書く。** 冗長で、
  ochakai のツールが OKF のものであることはツール名の仕事ではない。
- **何もしない。** §1 の二重語彙が残る。0023 と 0038 で二度同じ判断を
  している以上、ここだけ残す理由がない。

## 5. 壊れるもの

- **ツール名を設定に書いているクライアント** — allow-list、フック、
  プロンプトの中の名指し。MCP はツール一覧を接続時に配るので、名前を
  書いていないクライアントは何もしなくてよい。
- 0.x では互換を保証しない([docs/compatibility.md](../compatibility.md))。
  changelog に旧名と新名の対応表を置く。
- **サーバの能力は変わらない。** 同じ 8 本、同じ引数、同じ応答。
  改名だけである。
