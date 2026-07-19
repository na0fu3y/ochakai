# ochakai 設計ドキュメント 0018: import-ossie の廃止 — セマンティックモデルを通常ナレッジにする

Status: Accepted(2026-07-19)
Date: 2026-07-19

## 1. 目的

`import-ossie`(CLI コマンド、`POST /api/v1/import/ossie`、`internal/importer`)を
**廃止**する。セマンティックレイヤーからの決定的な SQL 生成(compile_sql)は
**継続**する。

セマンティックモデルは専用テーブルと専用インポート経路をやめ、
**通常のナレッジエントリ**として、他のすべての知識と同じ
create / update / search / export / revisions / provenance の下に置く。
モデルの登録と、検索用の metrics / tables エントリの整備は、
LLM を備えたクライアント(agent-web / agent-adk)の仕事とする。
サーバーに残る仕事は 2 つだけ — **書き込み時のモデル検証**と
**決定的なコンパイル**である。

## 2. 現状と問題

import-ossie は 2 つの仕事をしている: (1) Ossie YAML のスペック全文を専用の
`semantic_model` テーブルに upsert する、(2) `metrics/<name>`・`tables/<name>`
のナレッジエントリを派生生成する。この構造には 4 つのひずみがある。

1. **真実が二重。** compile_sql は `semantic_model` テーブルのスペックを読む。
   エージェントが `metrics/<name>` の `attrs.expression` を通常の update で
   直しても、生成される SQL は変わらない。ナレッジ側は「検索用の写し」で
   あって、編集がコンパイルに反映されない片方向の導出物である。
2. **エクスポートに乗らない。** OKF バンドル(`GET /api/v1/export`)は
   ナレッジだけを出す。モデル本体は export → import の往復で失われ、
   移った先で compile_sql が動かない。「Your knowledge is yours」
   (設計ドキュメント 0009)にとってモデルだけが例外になっている。
3. **リビジョンも provenance も付かない。** `semantic_model` テーブルは
   `updated_at` 1 列だけで、誰がいつ何を変えたかがナレッジと非対称。
   検証ワークフロー(draft → verified)も適用されない。
4. **専用サーフェスの維持費。** REST 専用エンドポイント、CLI コマンド、
   apiclient のメソッド、importer パッケージ — 「エージェントツールに
   馴染まない」として REST に残る面(restapi.go 冒頭)は export と
   import/ossie の 2 つだけであり、片方はこの専用性のためだけに存在する。

さらに、派生生成(2)の中身 — メトリクス・テーブルに検索可能な説明文を
付ける — は、機械的な写しよりも LLM クライアントのほうが上手にやれる
仕事である。サーバーが規約でこれを固定する理由は薄い。

## 3. 世界観: 埋めるのはクライアント、守るのはサーバー

設計ドキュメント 0015 の役割分担 — 解釈と生成はクライアント、決定性と
記録はサーバー — をセマンティックモデルにも適用する。

- **モデルを書くのは知識の編集である。** ゴールデンクエリや用語集と同様、
  モデルも draft で入り、人が確認して verified になる。特別な「インポート」
  という儀式は要らない。YAML ファイルを持っている利用者は、それを
  エージェントに渡して登録させるか、汎用の create / update API を
  スクリプトから叩けばよい。
- **サーバーが守る線は 1 本だけ。** compile_sql の約束は「LLM 非介在の
  決定的なコンパイル」であり、それは入力のモデルが**一つの整合した
  ドキュメントとして検証済み**であることに依存する。だからモデルは
  メトリクス単位に分散させず 1 エントリ 1 モデルの原子に保ち、壊れた
  モデルは**書き込み時に**拒否する。コンパイル時(= 利用者がクエリを
  求めた時)まで検証を遅延させない。
- **タイプは振る舞いの結び目**(0016・0017)。配置は利用者のものなので
  「モデルはこのパスに置け」とは強制しない。振る舞い(検証・コンパイル)は
  type に付く。

## 4. 決定

### 4.1 推奨タイプ `models` を追加する

0016 §2.1 の表に 1 行足す:

| スラグ | 表示名(frontmatter `type`) | サーバーの振る舞い |
|---|---|---|
| `models` | `Semantic Model` | 書き込み時検証(§4.2)、compile_sql の入力(§4.3) |

- Ossie モデルオブジェクト(単一)を **`attrs.spec` に verbatim 保持**する。
  compiler が読まないフィールドも保存され、Ossie 仕様の進化と往復忠実性が
  保たれる(従来の `semantic_model.spec` と同じ方針)。
- `title` はモデル名、`description` はモデルの説明を推奨(検索に載る)。
  `body` は自由 — モデルの背景・注意点など人間向けのノートの場所であり、
  検証対象ではない。
- 慣例のパスは `models/<name>` とするが、0017 のとおり配置は強制しない。
  解決はパスではなく id 参照で行う(§4.3)。
- 複数モデルの Ossie ドキュメント(`{semantic_model: [...]}`)は
  **1 エントリ 1 モデル**に分けて登録する。分割はクライアントの仕事。
  compiler の document 形の受理(`ModelsFromSpec`)は不要になり削除する。

### 4.2 type=models は書き込み時に検証する

create / update で type が `models` のエントリは、汎用 validate に加えて:

- `attrs.spec` が存在し、Ossie モデルオブジェクトとしてパースできること
  (`compiler.Model` への JSON round-trip)。
- `spec.name` が非空であること。
- 構造の整合: dataset / metric / field 名の重複がない、relationship の
  from / to が spec 内の dataset を指し、結合列の数が両側で一致する
  (結合列・primary_key は物理列であり field ではないので、存在までは
  検証しない — コンパイラも物理列としてそのまま出力する)。

違反は `Invalid`(400)で拒否する。式(expression)の方言選択や
サポート範囲(単一ファクト + スター結合)はリクエスト依存なので
従来どおりコンパイル時のエラーに残す — 書き込み時検証は「ドキュメントとして
壊れていない」ことの保証であり、「すべてのリクエストがコンパイルできる」
ことの保証ではない。

この検証はタイプの振る舞いなので、エントリがどのパスにあっても適用される。

### 4.3 compile_sql はモデルエントリを id で読む

- `CompileRequest.Model` は**モデルエントリの id**(例 `models/sales`)に
  なる(従来はテーブル上のモデル名)。省略時は従来どおり第 1 メトリクスの
  エントリ `metrics/<metric>` を引き、その **`attrs.model`(これも id)**で
  解決する。
- 解決したエントリの type が `models` でない、または `attrs.spec` が無い
  場合は明確なエラー。`semantic_model` テーブルと
  `Store.{Upsert,Get}SemanticModel` は削除する。
- `CompileResult` に解決した `model`(id)と `model_status` を載せる。
  status によるゲートはしない(0002: 信頼は provenance から判断する) —
  draft のモデルからもコンパイルはできるが、エージェントがそれを見て
  判断できるようにする。
- エントリはリビジョンを持つので、「どの版のモデルでこの SQL が出たか」が
  初めて追跡可能になる(従来の semantic_model は上書きのみ)。

### 4.4 派生エントリはクライアントの規約にする

`internal/importer` は削除し、metrics / tables エントリの生成・更新は
クライアント LLM の仕事とする。MCP ツール説明(create_knowledge /
compile_sql)に規約として 1 行ずつ記す:

- メトリクスは `metrics/<name>` に、`attrs.model` = モデルエントリの id、
  `attrs.expression` = 式、説明文つきで作る(compile_sql のモデル解決と
  検索の両方がこれに乗る)。
- テーブルは `resource` に正規 URI、`links` に
  `{rel: "defined_in", target: <モデルエントリ id>}` を付ける
  (backlinks でモデル → テーブルが辿れる。従来 importer が書いていた
  `model/<name>` という target は実在しないエントリを指す宙ぶらりんの
  参照だった — 本改訂で初めて実体を持つ)。

モデルとメトリクスエントリのずれ(改名・削除)は、compile_sql が
モデル側を正としてエラーを返すことで表面化する。サーバーが同期を
保証しない代わりに、不整合が黙って通ることもない。

### 4.5 削除するもの

- `internal/importer` 一式(派生ロジック、Report 型)
- `POST /api/v1/import/ossie`(restapi 冒頭の存在理由コメントも更新 —
  REST 専用面は export だけになる)
- CLI `import-ossie`(completion 含む)と `apiclient.ImportOssie` / `ImportReport`
- `semantic_model` テーブル(§5 の移行後に drop)
- `compiler.ModelsFromSpec` と document 形(`{semantic_model: [...]}`)の受理
- README / CONTRIBUTING / deploy ガイドの import-ossie 手順
  (`examples/semantic-model.yaml` は models エントリの OKF ドキュメント
  `examples/semantic-model.md` に置き換える — `ochakai create -f` でも
  エージェント経由でもそのまま登録できる)

## 5. 移行

マイグレーション 0012(0017 の 0011 の後):

1. `semantic_model` の各行を knowledge エントリに変換する:
   id = `models/<name>`、type = `models`、title = name、
   description = spec の description、`attrs.spec` = spec、
   status = draft(移行は人の検証を代行しない)、
   created_by = kind `system` / name `migration-0012`、rev 1 の
   create リビジョンを添える。id 衝突(既存エントリが `models/<name>` に
   ある)は主キー衝突でマイグレーション全体が中断・ロールバックする
   (0010 と同じ前例) — 衝突を解消して再適用する。
2. 既存 metrics エントリの `attrs.model` を `<name>` から `models/<name>` に
   書き換える(id 参照化、§4.3)。
3. 既存 tables エントリの `defined_in` リンク target を `model/<name>` から
   `models/<name>` に書き換える(宙ぶらりん参照の実体化、§4.4)。
4. `semantic_model` テーブルを drop する。

0012 の起動拒否ガードのような措置は不要 — 消えるのはエンドポイントと
コマンドであり、叩けば 404 / usage エラーで即座に分かる。

## 6. やらないこと

- **サーバー側の派生フック。** models エントリの書き込み時に metrics /
  tables エントリを自動生成することはしない。importer の同期ロジックが
  場所を変えて生き残るだけで、§2-1 の片方向導出の問題が戻ってくる。
  埋めるのはクライアントである(§3)。
- **YAML 専用の投入経路の復活。** 汎用 create / update で足りる。CI から
  モデルを流し込みたい場合も、スペックを `attrs.spec` に載せた 1 リクエスト
  で済む。
- **compile の status ゲート。** draft モデルのコンパイル拒否はしない。
  表示して判断を委ねる(§4.3)。
- **スキーマの全面強制。** `attrs.spec` は verbatim 保持であり、検証は
  compiler が読む部分の整合に限る(§4.2)。Ossie 仕様の未対応フィールドを
  落とさない。
- **Ossie YAML 形式のエクスポート。** export は従来どおり OKF バンドル
  のみ。モデルは models エントリとして(frontmatter の attrs に spec を
  抱えて)出て行き、そのまま戻れる。Ossie YAML への逆変換は必要になったら
  クライアントの仕事。

## 7. 検討した代替案

- **案 B: importer を write-フックに移す**(models エントリの書き込み時に
  サーバーが派生エントリを upsert)。原子性と検索性は保てるが、派生
  ロジックの保守面積がそのまま残り、人が編集した派生エントリを再生成が
  クロバーしない規則(現 importer の no-clobber upsert)も持ち越しになる。
  クライアントに LLM がある前提では、写しの品質もクライアント側が上。却下。
- **案 C: モデルを解体してメトリクス単位に分散**(モデルエントリを作らず、
  metrics / tables エントリの attrs からコンパイル時に組み立てる)。
  relationships・primary_key の置き場が不自然になり、整合性検証が
  コンパイル時まで遅延し、verbatim 保持も失われる。決定的コンパイルの
  基盤を「たまたま整合していること」に載せることになる。却下。
- **案 D: spec を body に YAML で持つ。** body は人間の自由領域であり
  検証対象にしない、という一線(0016 §2.4)が壊れる。機械が所有する
  構造の場所は attrs である。却下。
- **案 E: 現状維持。** §2 の 4 つのひずみが残る。却下。

## 8. 壊れるもの(許容済みの棚卸し)

- **CLI**: `ochakai import-ossie` が消える。ドキュメント化された代替は
  「エージェントに YAML を渡す」または汎用 create / update。
- **REST**: `POST /api/v1/import/ossie` が消える(404)。
- **compile_sql のワイヤ**: `model` 引数と metrics エントリの `attrs.model`
  の意味がモデル名からエントリ id に変わる(§5-2 で既存データは移行)。
  結果に `model` / `model_status` が増える。
- **エージェント設定**: import-ossie に言及する CLAUDE.md・プロンプトは
  更新が必要。compile_sql のエラーメッセージの誘導先も
  「import-ossie で取り込め」から「models エントリを作れ」に変わる。
- **DB**: `semantic_model` テーブルが消える(0.8.0 系からの直接
  アップグレードは従来どおりマイグレーション経由)。

リリースは 0.10.0 に一括で載せる(0016・0017 と同じ列車。いずれも
未リリースであり、compile_sql のワイヤ変更を跨ぎで刻む理由がない)。
