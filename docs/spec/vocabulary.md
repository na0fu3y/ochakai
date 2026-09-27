# 型の語彙と呼び名

利用者が書き、覚え、フィルタに綴る**語**。語は表面であり、
[surface.md](../surface.md) の VOCAB が全部を数えている。

## 型は自由な文字列で、推奨は 9 つ

`type` は閉じた集合ではない。**推奨に無い型も第一級**で、書き込みも検索も
一覧も同じに扱う(SPEC §4.1 は型を中央登録せず、§11 は未知の型を理由に
拒むことを禁じる)。

- 照合は大小文字を区別しない(`type=metric` は `Metric` に当たる)。保存は
  書かれたまま、NFC に正規化する。
- 1 行・128 バイト以内。`/` も使える(`acme/Table` は正当な OKF である)。
- 型は「何であるか」で、住所ではない。パスから型を推測しない
  ([addressing.md](addressing.md))。
- 書き込みが型を見て検証するのは一つだけ — **Attested Computation は
  `runtime` が要る**(SPEC §10.2)。型ごとのスキーマは持たない。

**製品が教える語彙**(`--type` のヘルプ、MCP のスキーマ、Web UI の選択肢に
出るもの)は次の 9 つで、並びは「定義 → 実行 → 解釈と規範 → カタログ →
外部の写し」である。

| 型 | 何を持つか |
|---|---|
| `Metric` | 数字の定義と、同義語 |
| `Attested Computation` | 人が認めた計算と、その実行を確かめる手段(golden query はこれ) |
| `Skill` | 手順そのもの。このベースでの書き方もここに書く |
| `Insight` | 数字の読み方: 基準線、季節性、注意点、閾値 |
| `Policy` | 数字の決め方を定める規範。`sources` の典拠になる |
| `Glossary Term` | 用語 |
| `BigQuery Dataset` | データセットのカタログ |
| `BigQuery Table` | テーブルのカタログ |
| `Reference` | 外部資料の写し |

推奨に入れる基準は、**綴りだけで意味が立つか**、**OKF が実際にその綴りを
使ったか**(SPEC の例、OKF 公式のバンドル)、**誰かがその綴りで実際に
書いたか**である。`Semantic Model`・`Golden Query`・`Playbook`・
`API Endpoint` は推奨から外したが、自由な型として書ける。

`Skill` は二つの意味で読まれる。デプロイ自身のエージェントは「棚卸し」の
ような手順を `Skill` から読み、MCP の instructions は書く前に `Skill` を
探すよう求める — どちらも **`human-reviewed` の `Skill` にだけ従う**。

## 知識の単位は concept と呼ぶ

OKF SPEC §2 が知識の単位を **concept** と呼ぶので、読む人が出会う語は
concept に揃える。

- MCP のツールは `search_concepts`・`list_concepts`・`get_concept`・
  `put_concept`(検索と一覧だけ複数形)、JSON の欄は `concepts`、CLI の
  出力は `ochakai://<id>`。
- Go の型名(`domain.Knowledge`)や内部の表名は、読む人に出ないので
  揃えていない。
- Web UI の日本語は「ナレッジ」(一件も全体も)と「ドキュメント」(保存
  された OKF の文)を使い、`concept` という語は画面に出さない。書き方の
  規則は CONTRIBUTING.md の「The web UI's Japanese」にあり、テストが読む。

## ほかの語

VOCAB が数える語のうち、書き手と読み手が綴るもの:

- **status**(内容の段階。書き手が書く): `draft` / `stable` / `deprecated`。
  書かれていなければ `stable` と読む(MCP から書いた文書は `draft` として
  書かれる)。
- **trust**(台帳から導く。書けない): `unverified` / `machine-confirmed` /
  `human-reviewed`([loop.md](loop.md))。
- **sort**(一覧の並び): `usage` / `verified_at` / `failed` / `stale_after`。
- **キュー**(`stats` の名): `drafts` / `failed` / `stale_after` / `edited`。
- **裁定の ruling**(`POST /api/v1/review/{id}`): `verified`。却下は
  `note` 付きの削除で、ruling の値ではない。
- **結果報告**: `worked` / `failed`。
- **書き込みの plan**: `created` / `updated` / `unchanged`。
- **リビジョンの change**: `create` / `update` / `delete` / `move` /
  `verify` / `reject` / `add_file` / `remove_file`。
- **エラーの code**: `invalid` / `not_found` / `already_exists` /
  `precondition_failed` / `forbidden` / `read_only` / `not_deleted` /
  `too_large` / `unsupported` / `method_not_allowed` / `internal`。
  状態コードが同じでも条件を分けられるように、封筒は `code` を運ぶ。

## やらないこと

- 型ごとの振る舞いやスキーマの登録簿、型の閉じた列挙。
- 推奨語彙に無い型を二級に扱うこと。
- 語を画面ごと・面ごとに言い換えること(一つの語は、どの面でも同じ綴り)。

## 経緯

設計記録 [0071](../design/0071-the-recommended-type-vocabulary.md)・
[0057](../design/0057-concept-is-the-word-a-reader-meets.md)・
[0064](../design/0064-rest-stops-at-api-v1.md) §7・§18・
[0083](../design/0083-an-error-carries-a-code.md)。
