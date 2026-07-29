# ochakai 設計ドキュメント 0054: 知識の単位は concept と呼ぶ

Status: Accepted(2026-07-29、2026-07-29 改訂 — リリース前、0048 §2.3)。
知識の単位を、OKF SPEC §2 が定義する語 `concept` で呼ぶ。MCP のツール名
5 本(`search_concepts` / `get_concept` / `put_concept` /
`delete_concept` / `get_concept_usage`)とリソーステンプレートの表示名、
**そして利用者が読む英語の散文すべて** — README、docs、CLI のヘルプと
出力、Web UI のラベル、MCP のツール説明、OpenAPI の description、
サーバのエラー文 — が同じ語になる(§3.6)。**ツールは 8 本のまま**で、
引数・応答・スキーマ・能力・JSON のフィールド名はどれも変わらない。
ツール名を設定に書いているクライアントが壊れる
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

**改名という意味では MCP のみ。** REST と CLI は既に `knowledge` を
持たず(§1)、`ochakai://` の URI はパスである。1 面だけの変更になるのは、
他の面が先に畳まれたからである。**ただし §3.6 の散文は全面に及ぶ。**

### 3.6 文書と表示の語も `concept` にする(2026-07-29 改訂)

当初この記録は**ワイヤ上の綴りだけ**を数えて「残っているのはツール名
だけ」と書いた。それが取りこぼしていたものがある — **利用者が読む英語**
である。改名した直後の実測はこうだった。

| どこ | `entry` | `concept` |
|---|---|---|
| README + `docs/`(英語) | **287** | 6 |
| `internal/mcpserver` のツール説明 | 78 | 2 |
| CLI のヘルプと出力 | 143 | 3 |
| Web UI のラベル | 193 | 1 |
| `api/openapi.yaml` の description | 201 | 42 |

つまり `get_concept` というツールの説明が「Fetch one **entry**」と書き、
`docs/knowledge.md` が「**An entry** is an OKF v0.2 document」と始まって
いた。§2 の根拠は「**ochakai 自身が既にその語で書いている**」だったが、
それが真だったのは日本語の設計文書と `internal/restapi` のコメントで
あって、**利用者が読む文書ではなかった**。翻訳表は消えておらず、置き場所
が変わっただけである。

だから散文も `concept` にする。**境界は「読む語」と「識別子」**である。

- **変える。** README・`docs/`・`examples/`・`deploy/` の英語散文、CLI の
  ヘルプと標準出力、Web UI のラベルとツールチップ、MCP のツール説明と
  `jsonschema` の説明文、OpenAPI の `description`、サーバのエラー文。
- **変えない。** JSON のフィールド名(`entries` は配列を名指すキーで
  あって単位の名前ではない)、Go の識別子と型名(§3.4)、
  データベースの列名、日本語の設計ドキュメント(不変)、CHANGELOG の
  過去の記述。
- **英語として別の意味の "entry" は残す** — `catalog entry`(カタログ
  の項目)、`sources` の要素、changelog の 1 項目、CSS の
  `.tree-entry`。同じ綴りでも同じものではない。

`entries` というキーを残すことは二重語彙ではない。0054 が最初から
「ツール**名**を変えるが引数と応答は変えない」と決めていたのと同じ線で
あり、応答の中で概念の**配列**を何と呼ぶかは、単位を何と呼ぶかとは
別の問いである。動かすとすればワイヤの破壊的変更として、それ自身の
理由で動かす。

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
- **散文(§3.6)は何も壊さない。** 変わるのは読む語だけで、呼べるもの・
  送る形・返る形はどれも 1 バイトも動かない。ヘルプの文言や
  エラー文の部分一致に依存しているスクリプトだけが影響を受ける。
