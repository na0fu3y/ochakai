# ochakai 設計ドキュメント 0016: knowledge-catalog リファレンスバンドルへの準拠

Status: Accepted(2026-07-19 の議論で決定)
Date: 2026-07-19

## 1. 目的

設計ドキュメント 0005 で ochakai は OKF SPEC(v0.1)と双方向互換になったが、
SPEC はタクソノミーを規定しない。一方、SPEC と同じリポジトリの
[knowledge-catalog のリファレンスバンドル](https://github.com/GoogleCloudPlatform/knowledge-catalog/tree/main/okf/bundles)
(stackoverflow / ga4 / crypto_bitcoin)は、SPEC の外側に**事実上の標準**を確立している:

1. タイプディレクトリは**複数形**(`tables/`、`datasets/`、`references/`)。
2. **`references` タイプ**: 外部資料(enum 定義、ライセンス、スキーマ文書)を
   一級の concept として鏡写しにする(SPEC §8 が `references/` サブディレクトリに言及)。
3. **`datasets` タイプ**: テーブル群を束ねるコンテナ concept。
4. **`resource`** は frontmatter の中核キーで、`type` の直後に置かれる。
5. 本文セクションの規約: `# Schema`、`# Common query patterns`、`# Citations`。

ochakai の現状はこのいずれにも合っていない: 推奨タイプは単数形
(stackoverflow バンドルをインポートすると `tables/` は自由タイプ `tables` になり、
推奨タイプ `table` に結びつかない)、reference / dataset に相当するタイプがなく、
`resource` は attrs に埋もれている。本ドキュメントは破壊的変更を許容して
これを解消する。

## 2. 決定

### 2.1 推奨タイプのスラグを複数形にする

| 旧 | 新 | 表示名(frontmatter `type`) | サーバーの振る舞い |
|---|---|---|---|
| `metric` | `metrics` | `Metric` | compile_sql の入力 |
| `query` | `queries` | `Golden Query` | ゴールデンクエリカナリア |
| `insight` | `insights` | `Insight` | — |
| `term` | `terms` | `Glossary Term` | — |
| `table` | `tables` | `BigQuery Table` | — |
| (新規) | `datasets` | `BigQuery Dataset` | — |
| (新規) | `references` | `Reference` | — |

表示名 `BigQuery Table` / `BigQuery Dataset` は §2.5(BigQuery 限定)の帰結で、
knowledge-catalog の全バンドルの表記と一致する。`Table` / `Dataset` は受け入れ
エイリアス(§2.5)。

- バンドルパスは `metrics/<id>.md` のようになり、knowledge-catalog の
  バンドルと同じ見た目になる。**stackoverflow バンドルはそのまま
  インポートすると `tables` / `datasets` / `references` の推奨タイプに直接着地する**
  (frontmatter の `BigQuery Table` 等の表記は従来どおり `attrs.okf_type` で往復)。
- ルート index の表示順は metrics, queries, insights, terms, datasets,
  tables, references(dataset は table の容れ物なので先)。
- 旧単数形スラグは frontmatter の `type` からのマッピングでのみ新タイプに
  写像する(表示名 `Metric` 等は従来どおり写像される)。バンドルの
  ディレクトリ名には適用しない — 「パスが勝つ」規則(0005 §3.3)は
  例外を持たない。0016 以前のエクスポートを取り込むにはディレクトリを
  複数形にリネームするか、移行済みサーバーから再エクスポートする。

### 2.2 `datasets` と `references` を推奨タイプに加える

- `references` は「外部資料の鏡」: 外部 URL を `resource` に持ち、本文が
  その内容(enum 表、ライセンス条文、スキーマ文書)を再掲する。他エントリは
  本文中の相対リンクまたは `# Citations` から参照する。
- `datasets` は「テーブルの容れ物」: `resource` にデータセット URI を持ち、
  本文の `# Schema` セクションで配下のテーブルへリンクする。
- どちらも当面サーバーの振る舞いは付かない(insight / term と同格)。
  自由タイプと推奨タイプの関係は設計ドキュメント 0005 §2 のまま。

### 2.3 `resource` を封筒フィールドに昇格する

- `domain.Knowledge` に `Resource string` を追加し、DB に `resource` 列を持つ。
  REST / MCP のワイヤ形式にも `resource` が載る。
- エクスポートは `type` の直後に `resource` を出す(リファレンスバンドルの
  キー順に一致)。インポートは `resource` を封筒キーとして受け、attrs には
  置かない。
- ochakai 自身の table エントリの「`attrs.source` から導出」規則(0005 §3.3)は
  廃止。import-ossie は `Resource` を直接設定する(`attrs.model` は従来どおり)。

### 2.4 本文セクションの規約を推奨する

`# Schema` / `# Common query patterns` / `# Citations` を tables / datasets /
references の本文の推奨構成として文書と MCP ツール説明に記す。強制はしない
(SPEC §4.2 と同じく規約であって検証対象ではない)。

### 2.5 BigQuery 限定(dialect の廃止)

設計ドキュメント 0003(GCP 前提化)の簡略化を完結させる: ochakai の語彙と
コンパイルは **BigQuery を前提**とし、他ウェアハウスへの分岐を持たない。
互換性は保たない(2026-07-19 の議論で決定。レガシー最小化)。

- **compile の `dialect` オプションを廃止**。出力は常に BigQuery SQL
  (バッククォート引用、`DATE_TRUNC(DATE(x), MONTH)` 形式)。REST / MCP /
  CLI(`--dialect`)/ 補完から消える。旧クライアントが送る `dialect` キーは
  未知フィールドとして無視される。
- **入力側の Ossie 式辞書は従来どおり**: `BIGQUERY` キーを優先し、なければ
  `ANSI_SQL` にフォールバック(`ForBigQuery`)。これは「移植可能に書かれた
  semantic model を受ける」ためで、他方言に出力するためではない。
- **表示名は BigQuery 修飾**: tables → `BigQuery Table`、datasets →
  `BigQuery Dataset`。`Table` / `Dataset`(および 0016 以前の単数形スラグ)は
  受け入れエイリアスで、**正規表記に正規化される**(元表記は保存しない —
  自分の語彙のリネームであって外来表記の翻訳ではない)。
- **`resource` はカタログ流の正準 URL**: import-ossie は完全修飾ソース
  `project.dataset.table` を
  `https://bigquery.googleapis.com/v2/projects/P/datasets/D/tables/T` に
  正規化する。project を欠くソースは正準 URL を持たないため原文のまま。
- BigQuery 以外のテーブルを扱いたい利用者は自由タイプで持てる(§2.2 の
  世界観のとおり)。tables / datasets の `resource` が BigQuery URL である
  ことの検証はしない。

## 3. マイグレーション(0010)

単一トランザクションで:

1. `knowledge` に `resource text NOT NULL DEFAULT ''` を追加し、
   `attrs->>'resource'`(全タイプ)と `attrs->>'source'`(旧 `table` のみ、
   resource が空のとき)を移して当該キーを attrs から除去する。旧 `table`
   行の完全修飾ソースは §2.5 の正準 URL に正規化する。
2. 旧 5 スラグを複数形に UPDATE する: `knowledge`、`knowledge_revision`
   (`type` 列とスナップショット JSON の `type`)、`knowledge_event`、
   `knowledge_usage`、`knowledge_embedding`、`attachment`。
3. `links` の target(`"<type>/<id>"` および `ochakai://` 形式)を
   `knowledge.links` とリビジョンスナップショット内で書き換える。

自由タイプとして既に `metrics` 等の複数形スラグを使っていた場合、旧
`metric` との間で主キーが衝突し得る。その場合マイグレーションは失敗して
ロールバックする(黙って混ぜるより安全)。衝突を解消してから再適用する。

リビジョンスナップショットの書き換えは「歴史の改竄」ではなくリネームの
一貫適用である: スナップショットを読み戻す唯一の用途(復元・監査)が
新スラグの世界で機能することを優先する。

## 4. やらないこと

- REST パス形状の変更。タイプはパスパラメータであり、スラグが変わるだけ。
- 旧単数形スラグの API レベルの別名。DB を移行した後は新スラグが唯一の
  綴りである(frontmatter からのインポート写像だけが旧綴りを受ける)。
- `# Schema` 等の本文セクションの構文検証。
- 表示名の接尾辞推測(「`〜 Table` で終わる表記は全部 tables」のような規則)。
  エイリアスは knowledge-catalog に実在する綴りに限定した明示的な表であり、
  それ以外はパスが勝つ規則(0005 §3.3)と自由タイプで受ける。
