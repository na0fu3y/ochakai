# ochakai 設計ドキュメント 0054: MCP はオブジェクトを concept と file と呼ぶ

Status: Accepted(2026-07-29)。**BREAKING。** MCP の面が持つ二重語彙を
畳む — ツール名 6 本、応答のキー 2 つ、引数 1 つを、OKF SPEC §2 の
`concept` と [0046](0046-bundle-address-space.md) §2.1 の `file` に
改める(`search_concepts` / `get_concept` / `put_concept` /
`delete_concept` / `get_concept_usage` / `get_file`)。あわせて
`ochakai://` を**バンドルのオブジェクトの住所**にする — `get_file` が
渡す URI をこのサーバ自身が解決できなかったのを直す(§3.4)。
リソーステンプレートの表示名も同じ語に揃える。**ツールは 8 本のまま**で、
能力は増えも減りもしない。ツール名を設定に書いているクライアントと、
応答のキーを読んでいるクライアントが壊れる。
0046 §3.14 のツール表を言い直す(名前と URI の解決だけで、載る面と
載らない面は動かない)
Date: 2026-07-29

本ドキュメントは 2026-07-29 に採択した同番の記録を**差し替えたもの**で
ある([0048](0048-decision-records-for-wire-contracts.md) §2.3: リリース
に乗っていない決定は番号を足さずに置き換える)。元の記録は「知識の単位の
呼び名」に射程を限り、§3.2 で `get_attachment` を「別の問い」として
明示的に見送っていた。その問いに答えるのが本ドキュメントであり、答えは
同じ判断の続きだったので、二つの記録に分けない。

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

### 1.1 名前だけを見ていたので、半分残った

ツール名を 5 本改めても、**同じ二重語彙が同じ面の別の場所に残る**。
数え直すと 4 か所ある。

| どこ | いま | 何と食い違うか |
|---|---|---|
| ツール `get_attachment` | attachment | 0046 §2.1 が「添付」というオブジェクト種を溶かし、REST は 1 つの住所に畳んだ。説明文自身が "one file of the bundle" と書いている |
| `get_concept` / `put_concept` の応答 | `{"knowledge": …}` | ツール名は concept、封筒のキーは knowledge |
| `get_file` の応答 | `{"attachment": …}` | 同上 |
| `report_outcome` の引数 | `target` | 隣の `get_concept_usage` は同じ concept を同じ住所で `id` と呼ぶ |

**名前は名前の集合であって、ツール名の集合ではない。** エージェントが
払うのは「覚える語の数」であり、ツール名で 1 つ、応答のキーで 1 つ、
引数で 1 つ、どれも同じ通貨で払う。0023 と 0038 で二度した判断
(「綴りが割れたら OKF を採る」)を、面の**全体**に当てる。

### 1.2 住所が片道しか通っていない

`get_attachment` はファイルを埋め込みリソースとして返し、その URI を
`ochakai://<バンドルパス>` と名乗る。ところが `resources/read` は
`ochakai://{+id}` を**concept の id としてだけ**解決していた。

> **サーバが自分で渡した URI を、自分で読み返せない。**

`ValidID` はパスを受け取り、その後の引きが必ず外れるので、
`resources/read` は ResourceNotFound を返す。埋め込みリソースに URI は
必須なので、**渡さないという選択肢はない** — 選べるのは、渡したものが
働くかどうかだけである。

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

ファイルの側も同じ形である。OKF はファイルをファイルと呼び、0046 §2.1 は
オブジェクトを **概念 / ファイル** の 2 種と定め、REST は両方を
`/api/v1/bundle/{path}` に畳んだ。`attachment` は「エントリが*持つ*もの」
という**畳む前のモデルの語**であり、そのモデルはもう無い。

**`attachments` は残る。** concept の投影が持つ `attachments` は、本文
から**導出された帰属**であり(0046 §2.1: 「帰属の導出規則として残る」)、
「このバンドルにあるファイル」とは別のことを言っている。同じ語が二つの
ことを指していたのが問題なのだから、片方だけを改めるのが答えである。

## 3. 決定

### 3.1 改名する 6 本

| 旧 | 新 |
|---|---|
| `search_knowledge` | `search_concepts` |
| `get_knowledge` | `get_concept` |
| `put_knowledge` | `put_concept` |
| `delete_knowledge` | `delete_concept` |
| `get_knowledge_usage` | `get_concept_usage` |
| `get_attachment` | `get_file` |

**検索だけ複数形**である。`knowledge` は不可算名詞なので単複を問われ
なかったが、`concept` は可算で、検索は複数を返し残りは一つを扱う。
名前が数を偽らないほうがよい。

`get_file` の引数は `path` のままである(0046 §3.3 でバンドルパスに
なっている)。変わるのは名前だけで、読めるものは変わらない。

### 3.2 応答のキーと引数

| ツール | 旧 | 新 |
|---|---|---|
| `get_concept` / `put_concept` | `{"knowledge": View}` | `{"concept": View}` |
| `get_file` | `{"attachment": File}` | `{"file": File}` |
| `report_outcome` | 引数 `target` | 引数 `id` |

`target` を `id` にするのは、**隣のツールが同じものを id と呼んでいる**
からである。`get_concept_usage` と `report_outcome` は同じ concept の
同じ住所(`POST|GET /api/v1/usage/{id}`)を扱い、片方が `id`、片方が
`target` と訊く。`ochakai://` 接頭辞を許すのは従来どおりで、これは
§3.4 の住所をそのまま貼れるという意味である。

### 3.3 リソーステンプレートは「バンドルのオブジェクト」

`Name: "knowledge"` / `Title: "Knowledge entry"` は `object` /
`Bundle object` になる。エージェントが `@` で参照するときに見える名前で
あり、ツール名と別の語であってよい理由がない。URI 変数も
`{+id}` から `{+address}` になる — 指せるものが 2 種類あるからである。

### 3.4 `ochakai://` はバンドルのオブジェクトを指す

**URI の後ろはバンドルのオブジェクトの住所である** — concept はその id、
それ以外のオブジェクトはそのバンドルパス。`resources/read` は
**concept を先に引き、外れたらファイルを引く**。

これは REST が同じ住所で解決している順序であり(0046 §3.5、
`GET /api/v1/bundle/{path}`: `.md` なら concept、無ければファイル)、
**二つの面が同じ規則で同じ住所を読む**ことになる。衝突するのは、
producer が concept の id とまったく同じ名前のファイルを置いたときだけ
で、そのとき concept が勝つのは両方の面で同じである。

`index.md` と `log.md` は指せない。バンドルから**生成される**もので
あって保存されているものではなく(0046 §§3.7-3.8)、`ValidBundlePath`
が既にそう決めている。0015 §3.1 の「browse は MCP に載せない」も同じ側
にある。

**これはツールの追加ではない。** `get_file` が既に返しているバイト列を、
そのファイルが既に名乗っている住所で読めるようにするだけである。ツール
本数は 8 のまま、`tools/list` の JSON も動かない。逆向きの案 —
「ファイルには URI を名乗らせない」 — は取れない: 埋め込みリソースに
URI は必須で、ファイルに別の名前は無い。

### 3.5 変えないもの

- **`get_context` / `report_outcome` のツール名** — どちらも concept を
  名指していない。前者は問いに対する読み、後者は結果の報告である。
- **description の「entry」と「knowledge base」。** 直すのは
  「knowledge entry」という**複合語だけ**である — 一つのものが二語で
  綴られていた場所だからで、`get_concept` の説明が
  「one knowledge entry」で始まるのは、ツール名と説明が違う語で同じ
  ものを呼んでいるのと変わらない。単独の「entry」は REST の契約も CLI の
  help も使う面をまたいだ語であり、ここだけ改めると面の間に新しい割れ目が
  できる。「knowledge base」は場所の名前で、単位の名前ではない。
- **concept の投影の `attachments`** — §2 のとおり、導出された帰属で
  あって「バンドルのファイル」ではない。REST の投影と同じキーであり、
  ここだけ改めると面の間に新しい割れ目ができる。
- **ツールの本数 8**、`get_file` の引数、スキーマの形、能力。
- **`domain.Knowledge`(Go の型名)。** 281 箇所にあるが、**利用者は
  内部パッケージを覚えなくてよい**([docs/surface.md](../surface.md) の
  「数えていないもの」)。外形を 1 バイトも変えない機械的差分は、この
  決定の一部ではない。ただし **MCP の面を書いている型**
  (`knowledgeOut` → `conceptOut`、`attachmentIn/Out` → `fileIn/Out`、
  `parseKnowledgeURI` → `parseObjectURI`)は、キーと名前をその場で
  綴っているので一緒に動く。
- **REST・CLI・Web UI**。§1 のとおり、どれも `knowledge` を持たない。

### 3.6 各サーフェス(0015)

**MCP のみ。** REST と CLI は既に `knowledge` を持たず、Web UI は画面の
語である。1 面だけの変更になるのは、他の面が先に畳まれたからである。
`/api/v1/bundle/{path}` は既にファイルを 1 つの住所で返しており、
§3.4 が MCP に持ち込むのはその規則であって、新しい能力ではない。

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

- **両方の名前を出す(別名)。** ツールが 8 本から 14 本になる。
  スキーマはエージェントのコンテキストから支払われるので本数は予算で
  ある(0015 §3.1)。互換のために予算を 7 割増やすのは、この
  プロジェクトが最も避けてきた形の支払いである。
- **`search_okf_concepts` のように出自を名前に書く。** 冗長で、
  ochakai のツールが OKF のものであることはツール名の仕事ではない。
- **`get_file` を `get_concept` に畳む。** 数の上では 8 が 7 になる。
  採らない理由は 0055 §2 と同じで、**返すものが違う** — 一方は JSON の
  投影、他方は媒体に応じた content ブロックであり、畳めば「どちらが
  返るか」を引数で訊く一つのツールになる。0015 §3.1 が
  「添付は重いので意図して取れ」と書いているのは、その分岐を**呼ぶ側の
  判断**として残すためである。
- **応答のキーを変えない。** ツール名だけ concept にして封筒は
  `knowledge` のまま、が最初の記録の状態だった。§1.1 のとおり、これは
  二重語彙を消したのではなく、見えにくい場所へ移しただけである。
- **URI をバンドルパスに統一する**(concept も `ochakai://<id>.md`)。
  住所が 1 綴りになるのは魅力だが、**MCP の面の中に id を取るツールと
  パスを取る URI が割れる** — 0046 §3.14 が CLI で拒んだ形そのもので
  ある。MCP の語彙は id であり(全ツールが id を取る)、concept ID は
  OKF が定義する住所でもある(SPEC §2)。REST が 2 綴りを持つのは
  `bundle/{path}` が**バイト列の面**だからで、そちらの理由は MCP には
  無い。
- **何もしない。** §1 の二重語彙が残る。0023 と 0038 で二度同じ判断を
  している以上、ここだけ残す理由がない。

## 5. 壊れるもの

- **ツール名を設定に書いているクライアント** — allow-list、フック、
  プロンプトの中の名指し。MCP はツール一覧を接続時に配るので、名前を
  書いていないクライアントは何もしなくてよい。
- **応答のキーを読んでいるクライアント** — `result.knowledge` は
  `result.concept` に、`result.attachment` は `result.file` になる。
  構造化応答を使わずに content ブロックだけ読んでいるクライアントは
  影響を受けない。
- **`report_outcome` に `target` を渡しているクライアント** — 引数名は
  `id` になる。値の形は変わらない(`ochakai://` 接頭辞も従来どおり)。
- 0.x では互換を保証しない([docs/compatibility.md](../compatibility.md))。
  changelog に旧名と新名の対応表を置く。
- **サーバの能力は変わらない。** 同じ 8 本、同じ引数の形、同じ応答の
  中身。改名と、既に渡していた住所が解決するようになったことだけである。
