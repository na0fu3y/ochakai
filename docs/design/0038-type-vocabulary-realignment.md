# ochakai 設計ドキュメント 0038: 推奨型の語彙を OKF の証拠に合わせ直す

Status: Accepted(2026-07-27)。[0023](0023-okf-type-vocabulary.md) を
Superseded にし、[0028](0028-retire-compile-sql.md) §3 の
「`Semantic Model` は推奨語彙に残す」と
[0036](0036-okf-schema-first.md) §3.6 の 9 型を改訂する。
**型の語彙領域の現行ドキュメント**。実装は同 PR。
[0046](0046-bundle-address-space.md) §3.9 が書き込み時の case 正規化
(`CanonicalType`)を撤去する — 語彙そのものは不変で、照合が case 非依存で
あることも変わらない
Date: 2026-07-27

## 1. 目的

推奨型の語彙を、ochakai が独自に育てた 9 型から、**OKF が実際に示している
語彙**に合わせ直す。`Semantic Model` と `Golden Query` を外し、
`Skill` / `Playbook` / `Policy` / `API Endpoint` を足して 11 型にする。

これは 0023 の続きである。0023 は「型の綴りがそのまま意味である」
(SPEC §4.1 の "the spelling is the meaning")を確立し、内部スラグとの
二重語彙を消した。本ドキュメントは同じ規準を**語彙の中身**に適用する —
綴りがそのまま意味になっていない型が 2 つ残っており、逆に **OKF の側が
綴りを与えている概念を 4 つ取りこぼしていた**。

本ドキュメントは 0023 の部分改訂ではなく置き換えである。0023 §3.1 の
一覧はすでに 0036 §3.6 が 1 度改訂しており、ここに 2 つ目の部分改訂を
重ねると「現在の語彙は何か」が 3 つのドキュメントの差分を合成しないと
分からなくなる(CONTRIBUTING.md の改訂チェーン回避)。§4 が語彙の
現在の姿を単独で述べる。

## 2. 前提の確認: SPEC は語彙を推奨していない

先に誤解を一つ潰しておく。SPEC §4.1 の
`BigQuery Table` / `BigQuery Dataset` / `API Endpoint` / `Metric` /
`Playbook` / `Reference` / `Attested Computation` という並びは
**"Example values" であって推奨語彙ではない**。同じ節がこう続く:

> Type values are **not** registered centrally. Producers SHOULD pick
> values that are descriptive and self-explanatory; consumers MUST
> tolerate unknown types gracefully, typically by treating them as
> generic concepts.

したがって「SPEC の例示に無いから外す」も「例示にあるから足す」も、
それ単独では論法にならない。判断の材料は 2 つある:

1. SPEC が producer に課している唯一の要請 —
   **"descriptive and self-explanatory"**。
2. **OKF が実際に綴りを与えた証拠**。SPEC の例示、SPEC 本文が worked
   example として書き下した型、そして SPEC と同じリポジトリの
   [リファレンスバンドル](https://github.com/GoogleCloudPlatform/knowledge-catalog/tree/main/okf/bundles)
   が実際に使っている型である。

この 2 つで測ると、現在の語彙は**両方向にずれている**。

## 3. 現状のずれ

### 3.1 綴りが意味になっていない 2 つ

**`Semantic Model`。** 0028 が compile_sql と書き込み時検証を撤去した
時点で、この型に残ったのは「`attrs.spec` に Apache Ossie のモデル
オブジェクトを入れる」という**サーバーが誰も読まない規約**だけになった。
0028 §3 は「振る舞いの無い型はすでに 3 つある — 語彙は振る舞いの
レジストリではない」として残す判断をしたが、その理由づけは**振る舞いの
有無しか見ていなかった**。

見るべきは self-explanatory かどうかである。`Insight` や `Reference` は
振る舞いが無くても綴りだけで意味が立つ。`Semantic Model` は立たない —
Apache Ossie という外部仕様を知って初めて `attrs.spec` に何を入れるかが
決まる。**綴りの外に意味がある型**であり、0023 が消したのはまさにこの構造
である。0028 の判断は 0023 の規準に照らすと一貫していなかった。

セマンティックモデルを ochakai に置くこと自体は 0018 のまま有効である。
外すのは推奨語彙からだけで、モデルは自由型として引き続き第一級に扱われる。

### 3.2 SPEC が綴りを与えたのに ochakai が持っていない 4 つ

| 型 | OKF 側の根拠 |
|---|---|
| `API Endpoint` | SPEC §4.1 の例示 |
| `Playbook` | SPEC §4.1 の例示、**§4.4 が worked example として全文を書いている**(「resource に紐づかない concept」の代表例、インシデント対応手順)、§9 のログ例にも登場 |
| `Policy` | リファレンスバンドル `acme_retail/policies/`(2 件) |
| `Skill` | リファレンスバンドル `acme_retail/skills/`、**SPEC §10.2 が `resource` の背後にあるものとして名指ししている**(「What sits behind a `resource` (a Skill, a script, a container) は packaging の選択」) |

`Policy` と `Skill` は例示に無いが、**ochakai がすでに封筒フィールドとして
持っている構造の行き先**である点が重い(§3.3)。

### 3.3 `Golden Query` — 概念は残り、綴りだけが余分になった

「質問 + 検証済み SQL」という概念は消えない。**それを表す型が SPEC に
できた**ので、ochakai 独自の綴りを持ち続ける理由が無くなった。

SPEC §10.2 で REQUIRED なのは `runtime` だけで、`parameters` /
`executor` / `attester` は OPTIONAL である。つまりゴールデンクエリは

```yaml
type: Attested Computation
runtime: bigquery
question: 今年の月次売上は?      # プロデューサ拡張キー
```

に本文の `# Computation` フェンスを添えれば、**過不足なく Attested
Computation として表現できる**。しかも得るものがある — `runtime` が
明示され、必要なら `attester` を足して「この SQL が書き換えられていない
こと」を下流に検証させられる。0036 §3.6 が「コンパイラを持たないまま
同じ需要をバンドルの語彙で満たせる」と書いた線の先である。

**`attrs.question` は型から独立している。** 埋め込みテキスト
(`embeddingText`、`internal/service/service.go`)と CLI の `context`
レンダリング(`cmd/ochakai/client.go`)はどちらも**属性**を読んでおり
型では分岐しない。したがって型を外しても、質問文が検索に効くことも
`question` が整形表示されることも変わらない。

### 3.4 封筒フィールドの行き先に名前が無かった

0036 が `sources` と Attested Computation の契約を封筒に載せたことで、
ochakai は**他のエントリを指すフィールドを 3 本**持っている。その指し先に
推奨語彙の名前が無かった:

- `sources[].resource` → 典拠。リファレンスバンドルでは
  `policies/revenue-recognition.md`(type `Policy`)を指している。
- `executor.resource` → 実行手順。バンドルでは
  `skills/run-on-bq.md`(type `Skill`)を指している。SPEC §10.2 も
  `resource` の背後を「a Skill, a script, a container」と説明する。
- `attester.resource` → 検証コード。バンドルでは `.py` ファイルであり、
  concept ではなく添付である。**ここに型は要らない。**

`Policy` と `Skill` を語彙に入れることは、**ochakai がすでに持っている
グラフの辺に、両端の名前を与える**ことに等しい。0038 で足す 4 つのうち、
この 2 つがもっとも実効的である。

## 4. 決定: 推奨語彙は 11 型

| 型 | 何を持つか |
|---|---|
| `Metric` | メトリクス定義、同義語 |
| `Attested Computation` | 認可された計算と、その実行を検証する手段(ゴールデンクエリはこれ) |
| `Skill` | 手順そのもの — `executor.resource` が指す実行手順 |
| `Playbook` | 人・エージェントが従う運用手順(インシデント対応など) |
| `Insight` | メトリクスの読み方: ベースライン、季節性、注意点、閾値 |
| `Policy` | 数字の決め方を定める規範。`sources[].resource` の典拠になる |
| `Glossary Term` | 用語 |
| `BigQuery Dataset` | データセットのカタログエントリ |
| `BigQuery Table` | テーブルのカタログエントリ |
| `API Endpoint` | BigQuery 以外の資産のカタログエントリ |
| `Reference` | 外部資料の写し |

表示順は「定義 → 実行 → 解釈と規範 → カタログ資産 → 外部の写し」で、
既存 7 型の相対順序は変えていない。

0023 のすべての規則はそのまま生き続ける — 型は自由文字列、照合は
大小文字非依存、保存は原表記、NFC 正規化、`/` 不可、128 バイト以内。
**変わるのは推奨語彙の中身だけである。**

### 4.1 `Skill` と `Playbook` は別の型である

どちらも「手順」だが、**誰が従うかではなく、何に指されるかが違う**。
`Skill` は `executor.resource` の指し先、すなわち Attested Computation の
契約の一部として機械的に参照される。`Playbook` は何にも指されない —
SPEC §4.4 がこれを「resource に紐づかない concept」の例として選んでいる
とおりである。畳んで 1 つにすると、契約の指し先を検索で絞れなくなる。

### 4.2 サーバーの振る舞いは 1 つも増えない

足す 4 型に検証も特別扱いも付けない。0028 が引いた線
(語彙は振る舞いのレジストリではない)はそのままで、型に schema を課す
例外は 0036 §3.10 が定めた `Attested Computation` の `runtime` ただ 1 つに
留まる。`Skill` に「手順が本文にあること」を課したくなるが、それは
0036 §5 が閉じた話である。

### 4.3 保存済みエントリは触らない

**マイグレーションは書かない。** 0023 §3.5 が旧綴りについて定めた
とおり、語彙から外れた型は**自由型としてそのまま生き続ける** —
検索も export も import もリビジョンも更新も、これまでどおり動く。
0005「自由型は第一級」がそのまま働くので、ここに特別扱いは要らない。

型を機械的に `Attested Computation` へ改名する案は**採らない**。
`runtime` は SPEC §10.2 で REQUIRED であり、書き込み経路の検証
(`validateOKF`、0036 §3.9)は create と update の両方で空の `runtime` を
400 にする。改名だけしたエントリは読めるが**次の更新で弾かれる**状態に
なり、かといって ochakai が `bigquery` と埋めるのは「知らない事実を
書き込む」ことで、0036 §3.4 の「再解釈は黙って行わない」に反する。
runtime を知っているのは著者だけである。

移行したい利用者は、通常の update で `type` と `runtime` を書き換えれば
よい。それは知識の編集であって、サーバーの都合で走らせるものではない
(0009「Your knowledge is yours」、0028 §5 と同じ線)。

### 4.4 語彙を述べる場所を 1 つにする

現状、語彙は**11 箇所にハードコードされ、長さが 3 通りに割れている** —
`domain.Types` と補完 4 箇所と OpenAPI と Web UI の `TYPE_ICONS` と
README が 9 型、MCP の `writeIn.Type` と Web UI のフォームヒントが 8 型
(`Semantic Model` が欠落)、MCP の `search_knowledge` 説明と
`searchIn` / `contextIn` が 7 型(`Attested Computation` も欠落)である。
`domain.TypesHint()` はエラーメッセージ 2 箇所でしか使われていない。

これは 0015(サーフェス整合)違反であり、本改訂で語彙を 2 方向に動かす
以上、同じ穴をもう一度掘らないために直す:

- **Go から出る文字列はすべて `domain.TypesHint()` 由来にする** —
  MCP のツール説明、シェル補完 4 種。補完はクォートが要るので
  `domain.TypesQuoted()` を足す。
- `api/openapi.yaml`・`internal/webui/index.html`・MCP の
  jsonschema タグは静的資産(構造体タグはコンパイル時定数)なので手で
  揃え、**0035 の流儀で外から検査する** — 退役した綴りが推奨として
  残っていないこと、現行 11 型が欠けていないことをテストで固定する。

## 5. やらないこと

- **`Policy` に「規範だから編集を制限する」ような振る舞い**(§4.2)。
- **`attester.resource` 用の型を足すこと。** 指し先は `.py` などの
  コードであり、concept ではなく添付である(§3.4)。
- **`Skill` / `Playbook` を 1 つに畳むこと**(§4.1)。
- **保存済みエントリの改名・削除・マイグレーション**(§4.3)。
- **`Metric` を外すこと。** 0028 §5 のまま。コンパイルされなくても
  メトリクスの定義は散文の知識として意味がある。
- **型を閉じた集合にすること。** 0023 のまま、自由型は第一級。
  語彙が 11 になっても、12 個目を利用者が書けることは変わらない。
- **段階的な deprecation。** 消えるのはヒントだけで、旧綴りの型は
  エラーにならず自由型として通る。0.15.0 で一度に落とす。

## 6. 壊れるもの

**保存済みデータと wire 形式は変わらない。** 型は自由文字列のままなので、
`type: Golden Query` を送る既存クライアントは今までどおり通る。

- 補完・ヒント・スキーマ説明から 2 つの綴りが消え、4 つ増える。
  `--type 'Golden Query'` は**エラーにならず、その型のエントリを返す** —
  自由型として照合されるだけである。
- `domain.TypeModels` と `domain.TypeQueries` の定数が消える
  (Go API を直接使う組み込みホスト向けの破壊的変更)。
- **推奨語彙が 11 に増えることで、エージェントに渡るツール説明が長くなる。**
  0015 が言うとおりツールスキーマはエージェントのコンテキスト予算を食う。
  それでも列挙する価値があるのは、型が `get_context` と検索フィルタの
  一語の絞り込み手段だからである(0036 §3.6 と同じ理由)。
- `docs/guides/golden-query-canary.md` のカナリア手順が
  `Attested Computation` を軸に書き直される。既存のゴールデンクエリで
  回している利用者は `--type` の値だけ変えれば従来どおり動く。
