# ochakai 設計ドキュメント 0036: OKF のスキーマを真とする — 互換規則の全面置き換え

Status: Accepted(2026-07-27)。[0005](0005-okf-compatibility.md) と
[0016](0016-knowledge-catalog-alignment.md) を Superseded にし、
[0009](0009-provenance-portability.md) §4 と
[0023](0023-okf-type-vocabulary.md) §3.1 を改訂する。
**OKF 互換領域の現行ドキュメント**。実装は同 PR、次のリリース(0.14.0)で出る。
§3.6 の 9 型は [0038](0038-type-vocabulary-realignment.md) が 11 型に組み替えた
(`Attested Computation` を語彙に足した決定そのものは有効)
Date: 2026-07-27

## 1. 目的

OKF が **v0.2** に更新され、これまで各プロデューサが拡張キーで自作していた
provenance・trust・lifecycle に標準の綴りができた(SPEC §5、§7、§10)。
v0.2 は additive な minor バンプだが、**2 つだけ破壊的なリネーム**を持つ
(§13.1): `timestamp` は `generated.at` に、本文の `# Citations` は
`sources` に置き換わる。

ochakai の OKF 互換は 0005 で決めたあと、0016(カタログ準拠)・0017
(パスが住所)・0019(ID 文字種)・0022(ファイル名が名前)・0023(型の
語彙)・0024(リンクは本文から)で 5 回改訂され、さらに 0028 で compile 面が
消えた。**現在の姿を読むのに複数本を Status ヘッダ順にたどる必要がある。**
v0.2 対応は frontmatter の中核に触るので、部分改訂を積まず
(CONTRIBUTING「amendment chains を避ける」)、本ドキュメントを
**OKF 互換領域の全面置き換え**として書く。

[0009](0009-provenance-portability.md) §4 は「SPEC 側に provenance の標準
キーが定義される動きをウォッチし、標準化されたら写像を再検討する」と明記して
いた。その条件が満たされた。

0017(パスが住所)/ 0019(ID 文字種)/ 0022(ファイル名が名前)/
0023(型の語彙)/ 0024(リンクは本文から)/ 0013(添付)はそれぞれ単一主題の
現行ドキュメントとして有効であり、本ドキュメントはそれらを前提として参照する。

### 1.1 撤回された 0034 について

v0.2 対応は一度 **0034「OKF v0.2 準拠」**として書かれ、main にマージされた
(#176)。本ドキュメントはその内容を吸収したうえで置き換えており、**0034 は
文書として撤回しファイルごと削除した**(#180)。番号 0034 は欠番であり、
当時の文面は PR #176 の履歴に残る。

**撤回したのは文書であって、実装ではない。** 0034 の実装は #176 でマージされ
**v0.13.0 で出荷済み**である(タグ 2026-07-26 16:58 JST、#180 のマージは
翌 01:29 JST)。エクスポートが `generated` / `verified` を書き
`okf_version: "0.2"` を宣言するのは 0.13.0 からで、利用者はすでにその挙動を
持っている。本ドキュメントの破壊的変更(§4)は **0.13.0 からの差分**として
読むこと。

撤回の理由は、0034 が封筒の基準として置いた次の一文が誤っていたことである
(§2.3 で詳述):

> 封筒に上げるのは、ochakai 自身が観測・照合・表示するものだけ。

この基準に従って 0034 は SPEC §5.1(`sources` / `usage_window`)と §10.2
(Attested Computation の契約)を attrs 素通しに留め、そのうえで
「検索・フィルタ・**UI** のいずれかが `sources` の中身を読む必要が実際に
出たとき、そのとき封筒に上げる」という再検討条件を書いていた。Web UI が
それを必要とし、条件を検討する過程で基準そのものが誤りだと分かった。
基準を差し替えると 0034 の §2.3 / §3.6 / §3.7 / §5 と往復規則の表が
連動して変わる — 部分改訂を積むより、未リリースのうちに 1 本に畳むほうが
読み手にとって正しい。

0034 のうち §3.2(trust 写像)・§3.3(`updated_by`)・§3.5(`stale_after`)・
§3.8(actor 規約)・§3.9(往復表)は本ドキュメントが引き継いでいる。
節番号も揃えてあるので、`0034 §3.4` を引いていたコードコメントは
`0036 §3.4` を読めばよい(§3.8 → §3.11、§3.9 → §3.12 の 2 つだけ動いた)。

## 2. 世界観

### 2.1 推奨はするが、強制しない(0005 §2 を継承)

推奨タイプは**推奨**であって閉集合ではない。利用者が自分のドキュメント
種別(runbook、data-contract、ADR……)を持ち込むことを妨げず、自由な
タイプは一級市民である — 検索・CRUD・検証ステータス・provenance・
リビジョンはすべて同じように機能し、振る舞いが付かないだけである。
SPEC §4.1 も「型は中央登録されない」と定めており、語彙は**振る舞いの
レジストリではない**(0028 §3)。

### 2.2 バンドルは知識を運び、provenance はインスタンス固有(0009 §3.1 を継承)

v0.2 で「誰が生成し、誰が検証したか」に標準の書き方ができたが、
**インポートでそれを読み戻さない方針は変えない**。`generated` /
`verified` は「このインスタンスでサーバーが観測した事実」であって、
ドキュメントの自己申告ではない。ochakai には認可機構がない(0002)以上、
ペイロードから provenance を受けた瞬間に、誰でも任意の provenance を
名乗れることになる。

エクスポートされた `generated` / `verified` は**参考情報(歴史的記録)**
である。標準化されたことで**外部の consumer が読める**ようになったのが
v0.2 の利得であり、ochakai 自身が読み戻す理由にはならない。

ただし v0.2 は 1 か所だけ、読み**取る**価値のある情報を足した:
`verified` キーの**存在**である。これは trust tier(SPEC §5.3)の唯一の根拠で
あり、`status` の写像に使う(§3.4)。存在は読むが、`by` / `at` の値は
採らない。

### 2.3 SPEC が定義するキーは封筒に持つ

**OKF v0.2 のスキーマを真とする。SPEC が定義するキーは
`domain.Knowledge` の封筒フィールドとして持ち、`attrs` に残るのは
プロデューサ独自の拡張キー(SPEC §4.1)だけにする。**

撤回された 0034(§1.1)は「封筒に上げるのは、ochakai 自身が観測・照合・
表示するものだけ」という**サーバー中心**の基準を置き、「サーバーが読まないものを封筒に上げると、
検証面・OpenAPI・MCP スキーマ・Web UI が増えるのに、増えた分を誰も使わない」
と述べていた。この前提は 2 か所で誤っていた。

- **「誰も使わない」が誤り。** 使うのは ochakai のサーバーではなく、
  **バンドルを読む外部の consumer と、それを編集する人間**である。ochakai は
  知識ストアであって、格納物の意味を解釈しないことが最初から設計の中核に
  ある(0001)。「サーバーが読むかどうか」を封筒の基準にすると、
  **解釈しないという設計が、記録しないという結論を導いてしまう。**
  ochakai が引き受けるのは記録であり、記録の形は SPEC が決める。
- **素通しは「4 サーフェスが同じ形を語れる」を満たさない。** 型のないマップは
  OpenAPI にもツールスキーマにも書けず、UI のフォームにも起こせない。
  0015 が 4 サーフェス一様を求める以上、スキーマのあるものはスキーマとして
  持つべきである。

したがって基準は:

| | 置き場所 |
|---|---|
| SPEC v0.2 が定義するキー | **封筒フィールド** |
| プロデューサ独自の拡張キー(SPEC §4.1) | `attrs` を素通し(0005 以来のまま) |

**封筒に持つことは、読むことでも実行することでもない。** `executor` /
`attester` を封筒に持っても ochakai はそれを取得も検証も実行もしない(§5)。
0001 の中核制約(LLM ゼロ・SQL 非実行)には触れない。

プロデューサ拡張キーの規則(SPEC §4.1: 未知のキーは保存し、往復させ、
拒否しない)は 0005 以来実装済みであり、値は元の位置・元の綴りで往復する。

### 2.4 過去の ochakai との互換より OKF との互換を優先する

同じ理由で、後方互換のために SPEC から外れた形を残すことをやめる。
**ochakai が過去に書いた綴りのための互換シムは作らない**(§3.5)。

ただし **OKF 自身が定める移行経路は「互換」ではなく仕様なので残す**:
v0.1 綴り(`timestamp` / `verified_by` / `verified_at`)の読み取り
フォールバック(SPEC §13.1)と、`verified` が bare mapping で来たときに
1 要素リストとして読むこと(SPEC §11)である。

## 3. 決定

### 3.1 宣言バージョンを 0.2 にする

バンドルルートの `index.md` は `okf_version: "0.2"` を宣言する
(SPEC §12。frontmatter が許される唯一の index)。`internal/okf.Version`
が `"0.2"` である。

### 3.2 trust: `timestamp` / `verified_*` ではなく `generated` / `verified`

SPEC §13.1 の破壊的リネームに正面から従い、v0.1 の綴りは**出さない**。

```yaml
generated: { by: agent:analyst@example.iam.gserviceaccount.com, at: 2026-07-26T04:12:00Z }
verified:
  - { by: human:tanaka@example.co.jp, at: 2026-07-26T09:30:00Z }
```

| v0.2 キー | ochakai の出所 |
|---|---|
| `generated.by` | **最後に内容を変えた actor**(§3.3 の `updated_by`) |
| `generated.at` | `updated_at` |
| `verified[].by` / `verified[].at` | `verified_by` / `verified_at` |

- `generated.at` は厳密には「サーバーが最後にこの行を書いた時刻」である。
  `SameContent` が無変更の書き込みを弾くので通常は SPEC §5.2 の "last
  meaningful change" と一致するが、**verify(0025 §6.2)と添付の
  attach/detach も `updated_at` を進める** — `updated_at` は ETag の版
  でもあるため、進めないと楽観ロックが壊れる。どちらも concept
  ドキュメント自体を書き換えてはいないので `generated.by` は動かさない
  (検証は内容の生成ではなく、添付はドキュメントの外のファイルである)。
  専用の列を足してまで `at` を分ける価値はないと判断した。
- `verified` は **1 要素のリスト形式**で出す。SPEC §5.2 は bare mapping も
  許すが、ochakai が将来複数の検証を保つようになってもワイヤ形が変わらない
  リスト形を選ぶ。インポート側は SPEC §11 の要求どおり bare mapping を
  1 要素リストとして扱う。
- **status が verified なら `verified` キーを必ず出す**(actor を持たない
  エントリでも)。キーの不在が unverified を意味する(SPEC §5.3)以上、これは
  §3.4 の写像が往復するための不変条件でもある — `stable` だけを書いて
  戻すと draft に落ちる。逆に、verified でないエントリは `verified` キーごと
  出さない。
- **`timestamp` / `verified_by` / `verified_at` は出力に無い。**
  インポートは v0.1 の綴りも読める(SPEC §13.1 のフォールバック、§2.4)が、
  どちらも 0009 のとおり値は採らない。

**委譲(0027)は mapping 内の拡張キー `via` で表す。**

```yaml
generated: { by: human:tanaka@example.co.jp, via: agent:insightflow@example.iam.gserviceaccount.com, at: ... }
```

`Actor.String()` の `"A via B"` 文字列を `by` に押し込むと SPEC §7 の actor
形式から外れる。`by` は actor 単体に保ち、委譲は兄弟キーにする — SPEC §4.1 の
「未知のキーは保存し、拒否しない」に収まり、`human:` 前置が保たれるので
trust tier(SPEC §5.3)も正しく出る。REST / MCP のワイヤ形式(`Actor` の
JSON)は変えない。

**ochakai 固有の provenance は拡張キーとして残す。** `created_by`
(作成者。OKF は「最後に変えた人」しか持たない)、`rejected_by` /
`rejected_at`(§3.4)。いずれも歴史的記録であり、インポートは値を採らない。

### 3.3 `updated_by` を封筒に持つ(マイグレーション 0019)

`generated.by` は「現在の内容を生成したのは誰か」であり、ochakai は
`created_by` と、リビジョンの `changed_by` しか持っていなかった。
`created_by` で代用すると、他人が更新した後に「生成者」が嘘になる。

- `domain.Knowledge` の `UpdatedBy Actor`、`knowledge` の
  `updated_by_kind` / `updated_by_name` / `updated_by_via` 列(既存の
  `created_by_*`(0001)と `*_by_via`(0017)と同じ形)。
- create / update / move が内容を書いたときに呼び出し元でスタンプする。
  move では、動いたエントリ自身と、本文を書き換えられた参照元の両方に
  付く(どちらも `update` / `move` リビジョンとして記録される変更である)。
  **verify(0025 §6.2)と attach/detach ではスタンプしない** — 検証は
  内容の変更ではなく `verified` に積む行為であり、添付は concept
  ドキュメントの外のファイルである。
- 内容が変わらない update(`SameContent`)もスタンプしない。何も書かない
  書き込みが生成者を付け替えては意味が反転する。
- バックフィル: 最新の create / update / move リビジョンの `changed_by`、
  無ければ `created_by`。
- REST / MCP のワイヤ形式に `updated_by` が載る(0015: 4 サーフェスに
  一様に出す)。

### 3.4 `status` は OKF 語彙で出し、`verified` の有無で読み戻す

SPEC §5.4 の語彙は `draft | stable | deprecated` の 3 値で、ochakai の 4 値
(draft / verified / deprecated / rejected)とは重ならない。v0.2 では
**「検証済み」は status ではなく `verified` の有無**で表す(SPEC §5.3)。

**エクスポート:**

| ochakai | `status` | 併記 |
|---|---|---|
| `draft` | `draft` | — |
| `verified` | `stable` | `verified: [{by, at}]` |
| `deprecated` | `deprecated` | `status_note` |
| `rejected` | `deprecated` | `status_note`、拡張キー `rejected_by` / `rejected_at` |

**インポート:**

| frontmatter | `verified` キー | ochakai |
|---|---|---|
| `draft` | 有無を問わず | `draft` |
| `stable` | あり | `verified` |
| `stable` | なし | `draft` |
| `deprecated` | 有無を問わず | `deprecated` |
| `status` 省略 | あり | `verified` |
| `status` 省略 | なし | **未設定のまま**(書き込み経路の既定に委ねる) |
| 上記以外 | あり | `verified`。レポートに note を 1 行残す |
| 上記以外 | なし | `draft`。エントリは落とさず、レポートに note を 1 行残す |

`verified` キーは**存在だけ**を読む。v0.1 文書では `verified_at` の存在が
同じことを言うので、そちらも見る。`by` / `at` の値は採らない(§2.2)。

- **ochakai → ochakai の往復は保たれる**(verified → `stable` +
  `verified:` → verified)。ochakai は status=verified のとき必ず
  `verified` を出す(§3.2)ので、この判定は曖昧にならない。
- **外来バンドルの `stable` は verified にしない。** OKF の `stable` は
  「消費してよい」であって「人が確認した」ではなく、trust tier は
  `verified` からしか読めない(SPEC §5.3)。
- **`status` 省略は「未設定」として通す。** SPEC §5.4 の既定は `stable`
  だが、ochakai には既に「status を名乗らない書き込み」の規則がある —
  create なら draft、update なら現状維持。ここで値を作ると、status を
  持たない文書を再インポートするたびに**既存エントリを黙って draft に
  降格させる**。既定は書き込み経路に委ねるのが正しく、結果として新規は
  draft に着地するので §5.4 との実質的な差もない。
- **未知の status でドキュメントを落とさない**(SPEC §11)。
- **未知の status でも `verified` キーは効く。** lifecycle 値と trust の
  信号は SPEC §5.3 / §5.4 で独立しており、前者が読めないことは後者を
  捨てる理由にならない。これは §3.5 でレガシー status の別名を廃止しても
  0.12 以前のバンドルの verified が verified のまま入る仕組みでもある —
  それらの文書は `verified_at` を持っている。
- **再解釈は黙って行わない。** 未知の status、日付でない `stale_after` の
  ようにドキュメントを受けつつ値を読み替えた場合、パーサは note を返し、
  `ochakai import` が `note:` 行として出す。**skip とは別勘定**である —
  エントリは取り込まれているのだから、スキップ件数に混ぜてはいけない。

### 3.5 ochakai 側の互換を切る(§2.4 の具体化)

- **`attrs` に封筒キーと同名のキーを書いた payload は 400 で拒否する。**
  従来はキーが attrs に残り、エクスポートで `reservedKeys` により黙って
  落ちていた。書き手にその場で知らせるほうがよい。封筒キーの一覧
  (`domain.EnvelopeKeys`)が検証と `reservedKeys` の唯一の出所である。
- **`attrs.sources` を封筒に持ち上げる移行シムは書かない。**
  マイグレーション 0020 が既存データを 1 度移し、以後は封筒フィールドだけが
  正である。
- **ochakai ≤0.12 が書いたレガシー status 値(`verified` / `rejected`)の
  読み戻しをやめる。** OKF の status 語彙は 3 値であり、それ以外は未知の値
  として扱う(§3.4)。0.12 以前のバンドルの verified は `verified_at` の
  存在によって verified のまま入る — **ochakai の別名ではなく SPEC §5.3 の
  仕組みで**入るのが本ドキュメントの姿勢である。`rejected` は draft になる
  (そもそも §3.4 のとおり `rejected` はエクスポートで `deprecated` になり、
  往復は非可逆である)。
- **per-source / per-parameter のプロデューサ拡張キーは落とす。**
  SPEC のキーを網羅し、それ以外は note を出して捨てる(§3.4 の
  「再解釈は黙って行わない」)。封筒に上げた目的が「4 サーフェスが 1 つの形を
  語れること」である以上、`additionalProperties` は UI の行エディタと MCP の
  スキーマの両方を無力化する。将来必要になったら `Extra` を足すのは
  純粋な追加である。

トップレベルのプロデューサ拡張キーは従来どおり `attrs` を素通しする(§2.3)。
落とすのは**スペックが構造を定義したオブジェクトの内側**だけである。

### 3.6 型の語彙(0023 §3.1 の改訂)

SPEC §10 の concept 型 `Attested Computation` を推奨語彙に加え、9 型にする。
表示順は `Semantic Model` / `Metric` / `Golden Query` /
**`Attested Computation`** / `Insight` / `Glossary Term` /
`BigQuery Dataset` / `BigQuery Table` / `Reference`。

- 語彙に足す意味は 0023 §3.1 と同じ: 型の綴りがそのまま意味であり、
  推奨語彙に載っていれば `get_context` や検索フィルタで「実行してよい
  計算の定義」を一語で指せる。振る舞いの無い型はすでに複数ある。
- **サーバーの振る舞いは付かない。** 契約(§3.8)は記録するだけで、
  実行も検証も取得もしない。0001 の中核制約にも、0028 が「決定的な生成も
  クライアント側」と引き直した線にも触れない。
- 0028 §4 の「compile 再実装の条件」との関係: OKF が sanctioned
  computation の記録形式を定めたことで、**ochakai がコンパイラを持たない
  まま、同じ需要をバンドルの語彙で満たせる**ようになった。0028 の判断は
  補強されており、覆らない。

### 3.7 provenance(SPEC §5.1)を封筒に持つ

```yaml
sources:
  - id: ga4-schema
    resource: https://developers.google.com/analytics/bigquery/export-schema
    title: GA4 BigQuery Export schema
    author: team:ga4-docs
    usage_count: 5000
    last_modified: "2026-05-30"
usage_window: { from: "2026-06-01", to: "2026-06-30" }
```

| キー | 型 | SPEC の必須性 |
|---|---|---|
| `sources` | list of object | OPTIONAL |
| `sources[].resource` | string(URI / path) | **REQUIRED**(エントリ内) |
| `sources[].id` / `.title` / `.author` | string | OPTIONAL |
| `sources[].usage_count` | integer | OPTIONAL |
| `sources[].last_modified` | `YYYY-MM-DD` | OPTIONAL |
| `sources[].usage_window` | `{from, to}` | OPTIONAL(共有 window の上書き) |
| `usage_window` | `{from, to}` | OPTIONAL |

- **`usage_count` はポインタ(`*int`)で持つ。** SPEC は `usage_count` を
  liveness 信号と位置づけており、「未計測」と「0 回使われた」は意味が違う。
  ゼロ値と不在を同一視すると、後者だけが持つ「使われていない」という情報が
  消える。
- **日付は文字列で持つ。** `stale_after`(§3.9)と同じ理由 — タイムゾーン
  意味論を持ち込まない。**YAML は引用符の無い日付を型付きの時刻として読む**
  ため、そのまま JSON に落とすと `2026-06-01` が `2026-06-01T00:00:00Z` に
  なって戻ってくる。時刻成分を持たない値は日付として読み書きし、書き手が
  書いていないものを書かないようにする(`yamlScalar`)。同じ理由で、
  日付を値に持つ**拡張キー**も同じ扱いをする。
- **`sources[].usage_window` はトップレベルの上書きである**(SPEC §5.1)。
  ochakai は両者を記録するだけで、どちらが優先かの解決はしない —
  解決するのは信号を読む consumer である。
- **信号を採点しない。** SPEC §5.1 は raw signal を出し、重み付けは
  consumer に委ねると定める。ochakai は `resource` を取りに行かず、
  到達可能性も検証せず、credibility を数値化しない。
- 本文の footnote による per-claim 帰属(SPEC §5.1)は本文の慣習であり、
  コード変更を要さない。`# Citations` 本文リストのパースはしない(§5)。

`usage_count` と ochakai 自身の利用集計(`search_hits` / `fetches` /
`worked` / `failed`、0001 §9)は**別物である**。前者は*引用する側*が
*引用される素材*に付ける信号、後者は*このインスタンスでの*観測であり、
エクスポート時に相互変換はしない。

### 3.8 Attested Computation の契約(SPEC §10.2)を封筒に持つ

```yaml
type: Attested Computation
runtime: bigquery
parameters:
  - { name: year, type: integer, required: true }
computation: queries/revenue.sql     # 省略時は本文の "# Computation" fence
executor:
  resource: references/skills/run-on-bq.md
  receipt: [job_id, executed_sql, result]
attester:
  resource: references/attesters/revenue.py
```

| キー | 型 | SPEC の必須性 |
|---|---|---|
| `runtime` | string | **REQUIRED**(type が Attested Computation のとき) |
| `parameters` | list of `{name, type, required}` | OPTIONAL(`name`/`type` はエントリ内で REQUIRED) |
| `computation` | string(path) | OPTIONAL(不在なら本文の `# Computation` fence) |
| `executor.resource` / `executor.receipt` | string / list of string | REQUIRED(`executor` 内) |
| `attester.resource` | string(path) | REQUIRED(`attester` 内) |

- **`parameters[].required` は素の `bool`。** SPEC 上「不在」と `false` は
  同義(必須ではない)なので、ポインタにする理由がない。`usage_count`
  (§3.7)との扱いの違いは意図的である。
- **`receipt` は `executor` の中にある。** トップレベルの `receipt` ではない。
- **型による条件分岐はしない。** これらのフィールドは type によらず封筒に
  ある。Go の構造体は型ごとに形を変えられず、「型ごとのスキーマ強制を
  しない」(§5)も維持する。型に応じて変わるのは**検証(§3.10)と UI の
  出し分け**だけである。
- **実行しない。** §5 のとおり、`executor` を走らせず、receipt を検査せず、
  `attester` を実行せず、`computation` を取りに行かない。これは consumer の
  手順である(SPEC §10.5)。

### 3.9 lifecycle: `status` と `stale_after`

`status` は §3.4。`stale_after` は SPEC §5.5 の宣言:

```yaml
stale_after: "2026-12-31"
```

- 形は `YYYY-MM-DD` の絶対日付。タイムゾーン意味論を持ち込まないため
  `date` 列とし、比較は素の日付比較(`today >= stale_after`)。日付として
  成立しない値は書き込み時に 400、インポートでは note を残して落とす(§3.10)。
- ペイロードから受ける(`status_note` と同じく書き手のもの)。
  `SameContent` の比較対象に含める。省略は「クリア」を意味する — PUT は
  全置換であり、他の内容フィールドと同じ扱いである。
- **フィードは足さない。** 0025 の 2 フィード(`sort=verified_at` /
  `sort=failed`)は*サーバーが観測した*証拠で並ぶ。`stale_after` は
  書き手の宣言であり別軸である。実際に使われ始めてから 0025 の改訂として
  検討する。当面は各サーフェスで読み書き・表示できれば足りる。

### 3.10 検証は書き込み経路で効かせ、インポートでは効かせない

| 規則 | 書き込み(REST / MCP / Web UI) | インポート |
|---|---|---|
| `sources[].resource` が空 | 400 | その source を落として note |
| 日付が `YYYY-MM-DD` でない | 400 | 値を落として note |
| `parameters[].name` / `.type` が空 | 400 | その parameter を落として note |
| `executor` があって `resource` か `receipt` が空 | 400 | 落として note |
| `attester` があって `resource` が空 | 400 | 落として note |
| type が Attested Computation で `runtime` が空 | 400 | **通す**(note のみ) |
| `attrs` に封筒キーと同名のキー | 400(キー名を挙げる) | — |

- **インポートは決してドキュメントを落とさない。** SPEC の適合性節が
  「consumers must accept missing optional fields and unknown `type` values
  without rejection」と定めており、§3.12 の「拒否の粒度」も同じ。値を
  読み替えたり落としたりしたときは note を返す(§3.4)。
- **`runtime` の REQUIRED を書き込み経路で強制するのは、§5「型ごとの
  スキーマ強制をしない」からの意図的な逸脱である。** SPEC §10.2 は
  `runtime` を REQUIRED と定め、しかも `runtime` が `parameters` の意味論を
  決める — これを欠いた Attested Computation は、consumer にとって解釈不能な
  契約になる。逸脱をここ 1 点に限り、他の型には何の要求も足さない。
- **件数・サイズの上限は設けない。** attrs は今も無制限で、これらの値は
  そこに居た。表現の変更に新しいポリシーを紛れ込ませない(本文 4 MiB
  制限は依然効く)。

### 3.11 actor 規約(SPEC §7)との関係

ochakai は `human:<id>` / `agent:<id>` を出す。SPEC §7 は `human:<id>` /
`process:<id>` / `<producer>/<version>` を挙げる。

- **`human:` はそのまま一致する。** trust tier(SPEC §5.3)は `human:` 前置
  だけを見るので、tier の判定は正しく出る。§7 が「人手・人による確認には
  MUST で `human:` を使え」と定める要求も満たしている。
- **`agent:` は維持する。** ochakai の agent はほとんどが IAM
  サービスアカウント名でバージョンを持たない。`<producer>/<version>` に
  合わせるために偽のバージョンを足したり、意味の違う `process:` に
  読み替えたりはしない。SPEC §5.3 が human 以外を一括で machine-confirmed と
  するので、区別の必要も無い。
- `sources[].author` も actor 規約の位置だが、**その source の作者**であって
  ochakai が観測した actor ではない。文字列としてそのまま往復させる。

### 3.12 往復(round-trip)の規則 — 現在の全体像

| 事象 | 規則 |
|---|---|
| パスと ID | バンドルパスは `<id>.md`。ID がアドレスであり、パスは型を主張しない(0017)。セグメントは非空・128 バイト以内・制御文字なし・`.` 始まりでない(0019 §2)。NFC 正規化して照合する(0022) |
| タイプ | frontmatter の `type` が唯一の出所で、値は OKF の綴りそのもの(0023)。照合は大小文字非依存、保存は原表記。`type` を持たない .md は concept ではないのでスキップしてレポート |
| `title` | 任意。空なら ID の最終セグメントが名前(0022)。空の title は出力しない |
| `resource` | 封筒キー。`type` の直後に出す(カタログのリファレンスバンドルのキー順) |
| provenance | `sources` / `usage_window`(§3.7) |
| trust | `generated` / `verified`(§3.2) |
| lifecycle | `status` / `status_note` / `stale_after`(§3.4、§3.9) |
| computation | `runtime` / `parameters` / `computation` / `executor` / `attester`(§3.8) |
| frontmatter のキー順 | SPEC の族順: base(`type` / `resource` / `title` / `description` / `tags`)→ provenance → trust → lifecycle → computation → ochakai 拡張(`created_by` / `rejected_*`)→ プロデューサ拡張キー |
| 未知の frontmatter キー | プロデューサ拡張キー(SPEC §4.1)として値ごと `attrs` に保存し、エクスポートでトップレベルに書き戻す。封筒キーと同名の attr はエクスポートせず封筒が勝つ(書き込みでは 400、§3.5)。YAML が型付けした日付は、時刻成分が無ければ日付として書き戻す(§3.7) |
| スペックが構造を定義したオブジェクトの内側 | 網羅し、未知のキーは note を出して落とす(§3.5) |
| サーバー所有キー | `generated` / `verified` / `created_by` / `rejected_*`、および v0.1 の `timestamp` / `verified_by` / `verified_at` は**値を採らない**(0009)。`attrs` にも入れない。例外は `verified` の**存在**で、§3.4 の status 写像にだけ使う |
| リンク | 構造化フィールドを持たず、本文の markdown リンクから導出する(0024)。`# Links` セクションは出力しない |
| 添付 | 参照駆動で帰属し、正準レイアウトは `<id>/<name>`。外来の位置は `okf_path` で保存する(0008 / 0013) |
| `index.md` / `log.md` | OKF の予約ファイル。インポートは黙ってスキップ。`index.md` はエクスポートで全階層に再生成する(frontmatter なし、ルートのみ `okf_version`) |
| 隠しファイル | セグメントが `.` で始まるパスは黙ってスキップ |
| 非 Markdown ファイル | 添付にならないものはスキップしてレポート |
| アーカイブの包み | アンラップしない。包みディレクトリは名前空間である(0017 §4.3) |
| 改行コード | CRLF は LF に正規化して受ける |
| 拒否の粒度 | ドキュメント単位でスキップし、バンドル全体は失敗させない(0019、SPEC §11) |
| 再解釈の報告 | 取り込んだうえで値を読み替えたものは note として返し、skip とは別に報告する(§3.4) |

### 3.13 保存形(マイグレーション 0020)

`knowledge` に §3.7 / §3.8 の 7 列を足す:

| 列 | 型 |
|---|---|
| `sources` | `jsonb NOT NULL DEFAULT '[]'` |
| `usage_window` | `jsonb` |
| `runtime` | `text NOT NULL DEFAULT ''` |
| `parameters` | `jsonb NOT NULL DEFAULT '[]'` |
| `computation` | `text NOT NULL DEFAULT ''` |
| `executor` | `jsonb` |
| `attester` | `jsonb` |

- 1 家族 1 列。構造を持つものは `jsonb`(`links` / `attrs` の前例)、スカラは
  `text`。1 つの blob にまとめる案は、封筒に上げた意味(個別に問い合わせ
  可能であること)を失うので採らない。
- nullable な 3 列は「宣言が無い」と「空の宣言」を区別する。
- **移行は `attrs` から値を移し、同時に `attrs` からキーを消す。** 残すと
  ワイヤに二重に出る — `reservedKeys` が抑えるのは**エクスポートだけ**で
  あり、REST / MCP の応答と Web UI の Attributes タブには両方が現れ、次の
  全置換 PUT で重複が固定化する。
- 形が合わない値(配列でない `sources`、object でない要素、receipt を欠く
  `executor` など)は**その家族だけ丸ごと触らない**(attr も残す)。半端に
  変換するより、attr のまま残して人間が見つけられるほうがよい。
- **`updated_at` は進めない。** ETag の版(0030)であり `generated.at`
  (§3.2)でもある。表現の変更は内容の変更ではない。
- **リビジョンのスナップショットは書き換えない。** 履歴は当時の形を保つ。
- **index は張らない。** 現時点で sources を条件に引くものは無い。逆引き
  (§5)が実装されるときに GIN index を足す。

### 3.14 インポートの入口

`ochakai import <dir | file.tar.gz | ->`(REST 経由)のみ。DB 直結の
`import-okf` は 0007 で廃止済みで、復活させない。新しい API 面は作らず、
既存の POST / PUT の繰り返しとしてクライアント側で実装する(0004)。

### 3.15 各サーフェス(0015 §4)

- **REST = yes。** 唯一の契約。`domain.Knowledge` を直接デコードするので
  ハンドラの変更は無く、`openapi.yaml` にスキーマが増える。
- **CLI = yes。** `ochakai get` が OKF ドキュメントを出し `update` が読むので、
  okf の変更だけで編集が通る。新しいフラグは足さない。`renderEntry` に
  引用と契約の要約を 1 行ずつ足す。
- **MCP = yes。** 原則 no(0015 §2)だが、これは新しい能力ではない —
  `create_knowledge` の説明は**既に**エージェントへ `attrs.sources` を
  書けと指示しており、散文を型に置き換えるだけである。むしろ
  `update_knowledge` の全置換フィールド列挙に新フィールドを足さないと、
  列挙を信じたエージェントが引用と契約を消す。
- **Web UI = yes。** 本ドキュメントの動機そのもの。行エディタで sources と
  parameters を編集し、契約は type に応じて出し分ける。

## 4. 壊れるもの(0.14.0)

**基準線は 0.13.0 である。** v0.2 対応そのもの — `timestamp` /
`verified_by` / `verified_at` が `generated` / `verified` になり、
`status: verified` が `stable` + `verified` になり、`rejected` の往復が
非可逆になり、`status: stable` を受け付けるようになり、`okf_version` が
`"0.2"` になり、ワイヤに `updated_by` / `stale_after` が載ったこと —
は撤回された 0034 の実装として **0.13.0 で出荷済み**である(§1.1)。
それらは本ドキュメントの破壊的変更ではない。0.12 系から上げる場合は
0.13.0 のリリースノートも併せて読むこと。

0.13.0 からの差分は以下である。

**封筒に移ったことによるもの**

- **ワイヤ形式に 7 フィールドが増える**(`sources` / `usage_window` /
  `runtime` / `parameters` / `computation` / `executor` / `attester`。
  追加のみ)。
- **既存エントリの `attrs` から同名キーが消える。** `attrs.sources` などを
  読んでいたクライアントは封筒のフィールドを読むよう直す。マイグレーション
  0020 が値を移し、attrs からキーを削除する(§3.13)。
- **`attrs` に封筒キーと同名のキーを書く payload が 400 になる。**
  0.13.0 では黙って通り、エクスポートで消えていた(§3.5)。
- **エクスポートのキー順が SPEC の族順に変わる**(§3.12)。0.13.0 は
  `status` 系を trust の前に置いていた。値は変わらないが差分を取ると動く。
- **per-source / per-parameter のプロデューサ拡張キーが失われる**
  (note 付き、§3.5)。トップレベルの拡張キーは従来どおり素通しする。

**検証が増えたことによるもの**

- **`runtime` の無い Attested Computation が書き込み経路で 400 になる**
  (§3.10)。インポートは従来どおり通し、note を出す。
- `sources[].resource` の欠落、日付でない `last_modified` / `usage_window`、
  `name` か `type` を欠く `parameters`、`receipt` を欠く `executor` も
  同様に書き込みで 400 になる(§3.10)。

**status の読み戻しが変わるもの**

- **ochakai ≤0.12 が書いたレガシー status 値の読み戻しをやめる**(§3.5)。
  `status: verified` を持つバンドルは `verified_at` があるので verified の
  まま入るが、`status: rejected` は draft になる。
- **未知の status でも `verified` キーがあれば verified になる**(§3.4)。
  0.13.0 は未知の status を無条件に draft にしていた。lifecycle 値と trust の
  信号は独立という SPEC §5.3 / §5.4 の整理に合わせた変更であり、上のレガシー
  値の扱いもこの規則から導かれる。

**アップグレード手順**: マイグレーション 0020 が起動時に自動適用される。
`attrs` から封筒へ値を移す UPDATE を含むが、`updated_at` は進めないので
ETag も `generated.at` も動かない(§3.13)。`embeddingText`(0001)は
id / title / description / tags / `attrs["question"]` / body しか読まないので、
**再 embed は不要**である。

実装中に見つけた既存のバグも 1 つ直した(本改訂の帰結ではない)。
Web UI の詳細画面から status を変えると全置換 PUT のペイロードに
`resource` が含まれておらず消えていた件は 0.13.0 で直っているが、
**同じバグが編集フォームの経路には残っていた** — フォームに `resource` の
入力欄が無く、Edit → Save で消えていた。片方だけ直したから残ったのであり、
封筒の書き込み可能フィールドを 1 箇所(`WRITABLE`)に集め、両経路が
そこから payload を作るようにした。

## 5. やらないこと

- **`executor` / `attester` / `computation` を実行も検証も取得もしない。**
  封筒に持つことは実行することではない。ochakai が引き受けるのは記録であり、
  実行と receipt の検査は consumer の手順である(SPEC §10.5)。0001 の
  中核制約(LLM ゼロ・SQL 非実行)にも、0028 が引き直した線にも触れない。
- **`resource`(エントリのものも source のものも)を dereference しない。**
  URL を取りに行かず、到達可能性も検証せず、credibility を採点しない(§3.7)。
- **`sources` での検索・フィルタ・並べ替え・フィードを作らない。**
  逆引き(「この外部資料に由来するエントリ」)は未実装であり、実装するときに
  GIN index と併せて設計する。`stale_after` のフィードも作らない(§3.9)。
- **バンドルからの provenance の読み戻し**(0009、§2.2)。`verified` の
  存在だけを status 写像に使う。
- **型ごとのスキーマ強制**は §3.10 の `runtime` 1 点に限り、他へ広げない。
  `Semantic Model` の `attrs.spec` は 0028 で書き込み時検証を撤去済みで、
  戻さない。
- **`agent:` の `process:` への読み替え**(§3.11)。
- **`# Citations` 本文リストのパース**。SPEC §13.1 は v0.1 文書について
  MAY と定めるが、ochakai は本文を解釈しない(リンク導出を除く、0024)。
  `# Citations` はこれまでどおり本文として往復する。
- **`usage_count` と ochakai の利用集計の相互変換**(§3.7)。
