# OKF 互換・バンドル・保存形・往復と provenance

ochakai が何を保存し、どう受け取り、どう返すか。**形式は Open Knowledge
Format で、その横に第二の形式を発明しない**(C3)。資産は丸ごと出て、
丸ごと戻る(C1)。

## どの OKF か

「v0.2」は一つの文書を指さない — v0.2 は公開後に normative な規則を版番号を
動かさずに変えている。ochakai が合わせているのは**コミット**
`open-knowledge-format@0b87c52` で、正典は [compatibility.md](../compatibility.md)
である。

## バンドルとオブジェクト

ochakai が持つのは**一つのバンドル — パスからオブジェクトへの写像**である。
一デプロイ = 一バンドルで、名前空間はディレクトリである。オブジェクトは
二種類しかない。

| | 何か | 置き場所 |
|---|---|---|
| **concept** | `type` を持つ frontmatter のある `.md` | 文書のバイト列(DB) |
| **ファイル** | それ以外のすべて(`.md` 以外の拡張子) | markdown は DB、それ以外は GCS |

- **バンドルに入ったものは出てくる。** これが中心の不変条件である。取り込みが
  `skipped` に落としてよいのは、バンドルパスとして不正なもの(文字種違反、
  `..` の脱出、隠しパス)と、導出面の `index.md` / `log.md` と、書き込み経路が
  concept として拒んだ文書だけで、どれも理由を添えて報告する。
- **`.md` の住所に座れるのは concept と予約名だけ**である(SPEC §3.1・§11)。
  `type` の無い `.md` の書き込みは 400 で、ファイルは `.md` を名乗れない。
  export が非適合のバンドルを出さないためである。
- 上限は一オブジェクト 5 MiB、concept 本文 4 MiB。**GCS が無ければ
  非 markdown の書き込みは 501** で、デプロイはそれを言う。
- メディアタイプはバイト列の sniff で決め、クライアントの申告を信じない。
  **拒否はしない**(拒否するとバンドルが往復しない)。危険な形式は配信側で
  扱う — 画像・PDF・プレーンテキスト以外は `Content-Disposition:
  attachment`・`nosniff`・`sandbox` を付けて返し、HTML と SVG はインラインで
  配信しない。**受けるが描画しない。**
- 「このファイルはどの concept のものか」は**本文からの導出**である — その
  concept の本文が指すバンドル内のファイルと、`<id>/` 名前空間の直下にある
  concept でないオブジェクト。添付という操作は無く、置くのは `PUT`、外すのは
  `DELETE` である。

## 文書の形

- **concept の id はパスである**(SPEC §2)。`.md` を除いたバンドル・ルート
  からのフルパスで、frontmatter の `id:` は読まない — 届いた `id:` は
  producer の拡張キーとして保たれ、書かれたとおり返る。
- **必須キーは `type` だけ。** `title` は任意で、無ければ id の末尾
  セグメントが名前になる。export は title が空のときだけ省く。
- **型の書かれていないキー**(`title`・`description`・`resource`・`status`・
  `status_note`・`stale_after`・`runtime`・`computation`)が文字列以外の
  スカラーなら、書かれたテキストとして読み、note を出す。スカラーの位置に
  マッピングやリストが来たら落とし、note を出す。**ハードエラーは SPEC §11 が
  非適合とするもの(パースできない frontmatter と空の `type`)と、
  `runtime` の無い Attested Computation だけ**である。
- Attested Computation の `executor` は `resource` があれば有効である。
  `receipt` は要求しない。
- `status_note` は ochakai が書き、読み戻す唯一の独自キーである。
- **リンクは本文から導出する。** 本文中の markdown リンク(バンドル絶対
  `/path.md` と相対 `./x.md`)が関係である。`http(s)://` とコードの中は
  抽出しない。関係の型は持たない(SPEC がリンクテキストを関係名と定めて
  いない)。存在しない concept へのリンクは許し、後で作られれば繋がる。
- **鍵として比較される文字列は NFC に正規化する** — id、リンクの target、
  ファイルのパス、取り込みのパス、検索クエリ。macOS で再 tar した NFD の
  パスが別の concept として二重化しないためである。本文・title・description
  は正規化しない。

## 保存形: 受け取ったバイト列

**書き込みで保存するのは受け取ったバイト列そのもの**である。正準形は
保存形ではなく導出値で、内容が変わったかの判定と索引にだけ使う。書き手の
`type` の大小を直さず、書き手が書かなかった `status` を書かない — SPEC §5.4 の
既定(`stable`)を適用するのは読み手である。保存前の正規化は **CRLF → LF**
と**末尾の改行一つ**だけである。

- **版(ETag)は保存したバイト列の SHA-256**、`generated.at`(内部の
  `content_changed_at`)を進めるかは**正準形の一致**で判定する。整形だけの
  編集は ETag を動かし、`generated.at` は動かさない。
- 裁定とファイルの追加・削除は concept のバイト列を動かさないので ETag も
  動かさない。
- CLI は渡された文書をそのまま送る。

### 主張と観測

サーバーが所有するキー(`generated`・`verified`・`created_by`)は、この
インスタンスの**観測**である。文書が自分について書いた値は同じ名前の位置に
置いておけない(export で観測を書き戻すのに、YAML は重複キーを許さない)。

- 書き込みはそれらの行を**外科的に取り除き**、他のキー・コメント・順序は
  動かさない。
- **取り除くことは捨てることではない。** 文書が書いた trust family は
  `received:` の下に**主張**として残し、取り込みの note に出す。主張は台帳にも
  trust tier にも `trust=` にも入らない。
- **自分の export 形は主張ではない。** 添えられた trust family がこの
  インスタンスの観測と一致すれば戻ってきた自分の観測として取り除く。だから
  get → 編集 → PUT と export → レビュー → import は前と同じバイト列を保存
  する(`unchanged`)。

### 瞬間の綴り

`stale_after`・`sources[].last_modified`・`usage_window` の `from`/`to` は、
**RFC 3339 の datetime か `YYYY-MM-DD` の日付**を取る。日付はそれが開く
UTC の真夜中である。offset の無い datetime は取らない。二つの状態の v0.2 で
書かれたバンドルが両方ある以上、両方を読むことが SPEC §11 の寛容である。

**出口では、ochakai は綴りを選ばない。** 手元にテキストがあればそのテキストを
返す(frontmatter はタイムスタンプのノードを decode 前に文字列として保つ)。
瞬間でしか持っていない `stale_after` の列だけは RFC 3339 で綴り、真夜中を
日付に畳まない。ochakai が書き手として出す文書(`examples/` など)は
RFC 3339 で書く。

## 予約ファイルは導出面である

`index.md`(SPEC §8)と `log.md`(SPEC §9)は、ochakai が既に持っている
情報の OKF が定めた表現で、住所を読むと生成される。

- `index.md` の節は「サブディレクトリ」「概念」「ファイル」。frontmatter を
  持つのはルートの `okf_version` だけである。
- `log.md` は新しい順に日付でまとめ、一行一リビジョン。リンクはその
  `log.md` のディレクトリからの相対である。**却下の理由はここで運ぶ**
  (`**Rejection**` の行と一言)— OKF の信号は単調で、否定をどのキーに
  乗せても観測が主張として旅することになるからである。
- **取り込みは予約ファイルを読み戻さない。** 他所の履歴を自分の台帳に
  入れることは、provenance を読み戻すのと同じ誤りである。手書きの予約
  ファイルが取り込みで消えるときは note を出す(そのため自分の export を
  `--strict` で戻すと、ディレクトリの数だけ note が出て落ちる)。
- 予約名の住所を「そこにあるオブジェクト」以外として読む表現
  (`application/gzip`・`?history`)は 409 である。

## 取り込みと往復

`ochakai import` は、既存の書き込みを一件ずつ叩いて合成する(サーバーに
一括取り込みの口は無い)。

- tar に入れた形がそのまま構造である。アーカイブを自動で剥がさない。
- **一文書の 400 はその文書のスキップ**として報告して続け、その文書が指して
  いたファイルは書く(帰属は導出なので、拒否を伝染させない)。ファイルを
  持たないデプロイ(GCS 無し)の 501 も、そのファイルのスキップとして報告して
  続ける — concept は全部入る。認証・ネットワーク・それ以外の 5xx は中断する。
- `--dry-run` はサーバーの `dry_run` に聞く — 同じ検証・同じ読み・同じ規則を
  通して書かず、`Ochakai-Plan: created | updated | unchanged` と本文の
  `plan` で答える(常に 200、ETag 無し、DELETE では 400、read-only では
  「書けない」)。`--strict` は note とスキップで落ちるので、CI の関門に
  なる。

### provenance はインスタンスのものである

**provenance は、このインスタンスが観測した記録であって、可搬な属性では
ない。** バンドルが運ぶのは知識で、台帳は出ない。

- **同じインスタンスへの往復では provenance は一つも動かない。** 更新は
  `created_by` を引き継ぎ、台帳に触れない。
- 別のインスタンスへ移すとき(移行・共有・新しい concept)は、**取り込んだ
  者が作成者になる**。それは「レビューの関門を通したのは誰か」という正確な
  記録である。移行専用のサービスアカウントで取り込めば、そう読める。
- **Git はレビュー経路である。** export した木を Git に置き、merge 後に
  import する運用を推奨する([git-review](../guides/git-review.md))。
  **merge は verify ではない** — 取り込みは trust tier を動かさず、検証は
  `POST /api/v1/review/{id}` から下る。
- provenance を読み戻すオプション(`--preserve-provenance` の類)は持たない。
  認可の無いデプロイでは誰でも任意の provenance を名乗れることになる。

## 問いの語彙

frontmatter は索引され、`fm.<key>=<value>` で引ける(完全一致と包含だけ)。

- **列を持つキー**(`type`・`status`・`tags`・`sources`・`stale_after`)を
  `fm.` で名指すと 400 で、使うべきフィルタを名指す。同じキーが綴りによって
  別の問いになる(`status=stable` は書かなかった文書にも当たり、
  `fm.status=stable` は当たらない)からである。
- **OKF が定義しないキー**(producer の拡張キー、`id` を含む)は引けず、
  400 で引けるキーの一覧を返す。保存と往復には一切触れない。
- 引ける集合は `domain.EnvelopeKeys` から導出する — OKF がキーを足し、
  この一覧が覚えた日に、マイグレーション無しで引ける。
- `fm.` は REST と CLI にあり、MCP と Web UI には載せない。

## やらないこと

- Git や GCS を保存の本体にすること、型付き JSON の書き込み面、部分更新。
- provenance の読み戻し・実行・dereference・採点。主張を索引・trust tier・
  ワイヤの一級市民にすること。
- ファイルの中身を解釈すること。
- `fm.` に演算子(範囲・否定・論理式)を持たせること、キーごとの named
  フィルタ。
- export に `id:` を書くこと(住所の横に第二の綴りを置く)。
- 他人の文書の日付を正規化すること。
- 第二の形式(Apache Ossie など)の読み書き。戻す条件は
  [positioning](../positioning.md) にある。

## 経緯

設計記録 [0009](../design/0009-provenance-portability.md)・
[0075](../design/0075-the-bundle-is-the-address-space.md)・
[0079](../design/0079-taking-the-document.md)・
[0100](../design/0100-md-is-how-a-concept-is-spelled.md)・
[0135](../design/0135-a-rejection-is-a-deletion.md)・
[0136](../design/0136-a-concept-is-addressed-not-labelled.md)・
[0139](../design/0139-ochakai-does-not-choose-a-spelling.md)。
