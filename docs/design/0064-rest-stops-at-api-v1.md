# ochakai 設計ドキュメント 0064: REST は /api/v1 で止まる

Status: Accepted(2026-08-02)。[0046](0046-bundle-address-space.md) §3.5 の
代表表現の表を改訂する(概念の既定表現は「エクスポート形」ではなく
「JSON の View」— §4)。同ドキュメントの §2.1・§3.3、
[0058](0058-filters-nobody-arrived-through.md)、
[0061](0061-a-dry-run-is-the-write-withheld.md)、
[0030](0030-optimistic-locking.md) は引用するのみで改訂しない。
[0057](0057-concept-is-the-word-a-reader-meets.md) §3.2 は改訂する —
「`entries` は配列の名前であって単位の名前ではない」として wire から
外した除外を撤回し、`entries` を `concepts` に改名する(§7、issue
[#411](https://github.com/na0fu3y/ochakai/issues/411))。実装は同 PR
(issue [#379](https://github.com/na0fu3y/ochakai/issues/379))。
[0027](0027-delegated-provenance.md) と
[0052](0052-producer-beside-the-actor.md) が決めたヘッダ名を改訂する(§3)。
[0074](0074-the-document-and-the-vocabulary-that-asks-it.md) は §1 の
「title は任意」を wire に届かせるために引用するのみだが、§3・§7 が
置いた「型の文字種の緩和はしない」という除外のうち **`/` の一点は改訂
する**(§18)—— 理由は id の規則ではなく OKF SPEC §4.1・§11 である。
同じ一点について [0071](0071-the-recommended-type-vocabulary.md) §1 の
「`/` 不可」も改訂する。
Date: 2026-08-02

## 1. 目的

[docs/compatibility.md](../compatibility.md) は「メジャーバージョンが 0
の間、すべての面が不安定」と宣言し、「1.0 は未定、基準も無い」と続けて
いた。REST だけを止める最後の一括変更がこれで、この PR が同じ
compatibility.md を書き換えて「REST は止まった」と言う。MCP・CLI・
Web UI・保存形は不安定なまま — 動かしてよいものにはまだ動いてもらう。

## 2. 決定: 不明なクエリパラメータは 400 で名指す

REST の 11 操作すべてで、宣言していないクエリキーは 400 になる
(`fm.` だけは例外 — OKF frontmatter のキーとして中身で検査される。
0047)。これが今回の一番重い決定である: **凍結後に何かを安全に足せるか
どうかを決める**。以前は面ごとに違った — 未知の `fm.` キーは既に 400
だったが(概念自身の語彙と照合されるため)、それ以外の未知キーは黙って
無視されていた。この非対称が `dry_run` の false green を可能にした
根本原因で(0.16.1 のサーバは `dry_run` を無視してそのまま書き込む —
`internal/apiclient/apiclient.go` がレスポンスヘッダの有無から対応を
推測しているのはその名残)、凍結後に同じ形の事故が起きないための土台が
これである。

この決定はクエリパラメータでは閉じておらず、同じ形の穴が二つ残って
いた(issue [#470](https://github.com/na0fu3y/ochakai/issues/470))。

**リクエストボディの未知キー。** `readJSON`(`internal/restapi/restapi.go`)
は `DisallowUnknownFields` を呼んでおらず、本文を読む三操作
(`POST /api/v1/move`・`POST /api/v1/review/{id}`・
`POST /api/v1/usage/{id}`)は宣言していないキーを黙って捨てて 200 を
返していた。`{"rulling": "verified"}` は何もしない 200、`note` のつもり
の `notes` は静かに消える書き込み — 本文版の `dry_run` false green で
ある。**決定:** `dec.DisallowUnknownFields()` を呼び、JSON 値のあとに
続く内容(`dec.More()`)も 400 にする。未知キーはクエリパラメータと
同じ形で名指す。

**バンドル GET のモード別パラメータ。**
`GET /api/v1/bundle/{path}` は `history`・`limit`・`files` の三つを
住所全体に許可していたが、実際に読むのはそれぞれ一部のモードだけ
だった — `files` はアーカイブ分岐のみ、`limit` は `?history` と
`log.md` のみ、`history` はアーカイブ分岐より後で見るため
`Accept: application/gzip` と `?history` を同時に送ると `?history` が
黙って無視されていた。**決定:** モードの外で送られたキーは、そのモード
を名指す 400 にする。アーカイブと `?history` の重なりは、§4 が 6 通り
の表現に優先順位を与えた理由(同じものの表現違いだから)がここには
当てはまらない — `?history` はアーカイブとは別のオブジェクトなので、
優先順位ではなく 400 にする。

## 3. ヘッダ接頭辞の統一

`X-Ochakai-On-Behalf-Of` / `X-Ochakai-Producer`(リクエストヘッダ)を
`Ochakai-On-Behalf-Of` / `Ochakai-Producer` に改名し、5 本のレスポンス
ヘッダ(元から `X-` を持たない)と揃える。両綴りを受け付ける移行期間は
置かない — compatibility.md 自身の「非推奨ではなく撤去」という方針の
とおりであり、両綴りを永遠に受け付けることは 0046 §3.5 が一つの release
をかけて消した「第二の住所」と同じ形になる。RFC 6648 は新しいヘッダに
`X-` を付けないよう求めており、この対からは今回が最初で最後の機会になる
— 埋め込みホストのコードに深く入り込むリクエストヘッダ名は、この一覧の
中で最も後から直しにくい。

## 4. バンドルの表現交渉: 6通りに一つの優先順位

一つの住所が持つ 6 通りの表現(gzip アーカイブ・JSON・markdown・既定)に、
今まで存在しなかった一つの規則を与える:

1. `Accept: application/gzip` が最優先 — ただしオブジェクト自身の住所
   ではそこにサブツリーが無いので 409 で拒否される(既存の挙動)。
2. それ以外は種類ごとに明示 Accept 一つと既定一つ:
   - `index.md` / `log.md`: `application/json` → 構造化、それ以外 →
     生成された markdown。
   - 概念: `text/markdown` → エクスポート形の文書、それ以外 → JSON の
     View。
   - ファイル: `application/json` → メタデータ(名前・media_type・
     size・sha256・path・created_by・created_at)、それ以外 → バイト列。

ファイルの `application/json` 対応は新しいコードである — 今まで
ヘッダを無視してバイト列だけを返しており、ファイルの `sha256` を
バイト列を取らずに知る方法が無かった。0046 §3.5 の表は元からこの列を
約束していたが、コードが実装していなかった。

もう一つ: 0046 §3.5 の表は概念の**既定**(Accept 無し)を「エクスポート
形」としていたが、住所が着地して以来コードが実際に返してきたのは JSON
の View である — 「ブラウザや curl は `*/*` を送り、既存のクライアントが
読んでいる JSON を求めている」というコードの意図どおりの動作であり、
これは issue #379 が指摘した「0046 §3.5 と実装がひそかに食い違っている」
点の一つである。**ここではコードではなく表を直す** — 凍結の直前に
既定の表現を変えれば、既存のあらゆる REST クライアントを黙って壊す。
この訂正は 0046 §3.5 の表そのものへの改訂として記録する(本書の
`Status:` を参照)。

## 5. 住所の綴り: `.md` はバンドルパスの一部

`/api/v1/bundle/{path}` の `.md`、`/api/v1/review|usage/{id}`
の裸の id、`/api/v1/move` の body 内 id — 3 通りに見えるが、実装を見ると
矛盾していない。ただし `.md` の扱いは以前この書が言った形とは違う:
**`.md` は必須で、バンドルパスの一部である** — `.md` が外れないと
`internal/restapi/restapi.go` は概念に辿り着かない
(`strings.CutSuffix(path, ".md")`)。`Accept: text/markdown` のための
糖衣ではなく、`{path}` パラメータの例(`"metrics/revenue.md"`)がそもそも
示すとおり住所そのものの綴りである。

裸なのは `id`(`/review/{id}`・`/usage/{id}`)と `Summary.id` — 識別子で
あって住所ではない。`File.path` はファイル自身の拡張子を持つという意味
の裸で、`.md` が付かないという意味ではない。**`Change.path` は裸では
ない** — `BundleLog` 自身が「オブジェクトを住所で名指す」と書くとおり
バンドルパスであり、概念の行では `domain.ConceptPath(id) = id + ".md"`
がそのまま `knowledge_revision.path`(`internal/store/revision.go`)から
返ってくる。id と path は別の語で、裸なのは id だけだった。

単一対象(bundle・review・usage)はパスセグメント、二つの対象を持つ
`move` だけが body で名指す — 二対象という形の必然であって、もう一つの
非一貫性ではない。**この規則を一度だけ書き下す**: id は常に裸、バンドル
パスは概念なら `.md` を持つ、単一対象はパス・複数対象は body。コード
変更は無い — 今回直るのは記述だけである。

## 6. `attachments` / `Attachment` を wire から退役させる

0046 §2.1 は「添付(attachment)という概念は対象消滅する」とすでに
決めていたが、綴りだけが生き残っていた。wire 上のこの語をすべて
`files` / `File` に改める: `Attachment` スキーマと `View`・
`SearchHit`・`ReembedResult` の JSON キー、アーカイブ住所の
`?attachments=` クエリ(→ `?files=`)、MCP ツール `get_attachment`
(→ `get_file`)。`entries` は当初この改名の対象外とする案だったが、
issue #411 の指摘を受けて §7 で改めて決定し直す。

`ReembedResult.files` / `.missing` は**このパスの進捗**であって corpus
の全数調査ではない、と定義を明文化する。`attachment_embedding` は今も
`(concept id, ファイル名)` で鍵付けされており(0046 §3.3 が意図しない
第二の鍵空間だが wire からは見えないため本書の対象外)、将来これを
path 鍵に付け替えても、進捗という定義なら二度目の破壊的変更にならない。

意図して触れないもの: `internal/store` の旧来 id/name 鍵の内部メソッド
(`GetAttachmentMeta`・`ListAttachments`・`ListAttachmentsBatch`・
`PutAttachment`・`DeleteAttachment`・`GetAttachment`・`AttachmentBytes`)
と `attachment` / `attachment_embedding` の SQL テーブル。どのクライアント
からも見えない保存層の内部で、いつか行う付け替え作業と一緒に片付ける
(issue #379 のコメントがすでにこの境界を引いている)。

## 7. `entries` を `concepts` に改名する

issue [#411](https://github.com/na0fu3y/ochakai/issues/411) の指摘:
0057 §3.2 は `entries` を「配列の名前であって単位の名前ではない」として
wire から外したが、この理由は 3 箇所のうち 1 箇所にしか届かない。
`GET /api/v1/stats` の `entries` は `total`・`status`・`trust`・
`rejected`・`created` を持つ**構造体**(`domain.StatsEntries`)で配列
ではなく、そもそも当てはまらない。バンドル一覧 `{dirs, entries, files}`
では、隣の `files` を §6 が `attachments` から改名した理由(0046 §2.1 が
退役させた語)が `entries` にも同じ形で当てはまるのに、同じ応答の中で
片方だけ改名されていた。`GET /api/v1/context` は本当に concept の配列で
0057 §3.2 の理由が一番よく効く場所だが、`get_context` 自身の
`Description`(`internal/mcpserver/mcpserver.go`)はすでに「`"concepts"`
is the knowledge」と書いており、wire のキー名だけが追いついていなかった。

REST が本 PR で止まる以上、これが 3 箇所の綴りを動かせる最後の機会で
ある。`/stats` とバンドル一覧の 2 箇所は 0057 §3.2 の理由がそもそも
届いていない以上、「配列の名前だから恒久的に残す」とは書けない —
`/context` だけ残す案も、3 つのうち 1 つだけ違う語のまま残すことになり、
0057 が閉じようとした問題そのものになる。

**決定: 3 箇所とも `concepts` に改名する。** `Stats.entries`、バンドル
一覧の `entries`、`GET /api/v1/context` と MCP `get_context` の
`entries` を、すべて `concepts` にする。0057 §3.2 の除外は本書がここで
撤回する — 3 箇所のうち 2 箇所には最初から届いておらず、届く 1 箇所も
0057 §3.1 自身の決定(読む語はすべて concept にする)とすでに矛盾して
いたので、除外を保つ理由が消えている。ワイヤと `json` タグで直結する
Go の識別子も揃える(`domain.StatsEntries` → `StatsConcepts`、
`store.BrowseEntry` / `apiclient.BrowseEntry` → `BrowseConcept`)—
0054 §3.4 が対象外とした内部専用の識別子(`domain.Knowledge` 等)とは
違い、この 3 つは §6 の `Attachment` → `File` と同じ扱いになる。

## 8. `attach` / `detach` を `add_file` / `remove_file` に改める

issue [#470](https://github.com/na0fu3y/ochakai/issues/470) の指摘: §6 は
`attachments` / `Attachment` を wire から退役させたが、`Change.change` と
`Revision.change` の列挙(`domain.Changes`、
`internal/domain/knowledge.go`)には `attach` / `detach` がまだ残っている
— `internal/store/attachment.go` の `PutAttachment` / `DeleteAttachment`
が書き込む値そのものである。§7 が `entries` に対してやったのと同じ形の
取りこぼしが、隣の語彙にもう一つあった。

`log.md` 先頭の太字は 0046 §3.8 が「`change` の語彙をそのまま出す」と
決めた導出であり、そこに挙げた `create` / `update` / `move` / `delete`
は例示であって網羅ではない(現行の語彙は既に `verify` / `reject` /
`withdraw` を含み、0046 §3.8 はそのどれも列挙していない)。したがって
この改名は 0046 の改訂を要さず、本書だけで閉じる。

**決定: `attach` → `add_file`、`detach` → `remove_file` にする。**
`PutAttachment` / `DeleteAttachment` という内部の Go 識別子と
`attachment` / `attachment_embedding` の SQL テーブルは §6 が引いた
境界のとおり対象外のまま — 動くのは `touchAndRevise` に渡す文字列と、
`Change.change` / `Revision.change` の列挙だけである。保存済みの行は
マイグレーション `0035_a_file_is_added_or_removed.sql` が書き換える
— design doc 0056 §3.3 が `unreject` → `withdraw` で使った形と同じ
`UPDATE knowledge_revision SET change = ... WHERE change = ...` である。
[docs/surface.md](../surface.md) の VOCAB は `change.attach` /
`change.detach` を `change.add_file` / `change.remove_file` に差し替え、
アルファベット順で並べ直す(数は 34 のまま)。

CLI コマンド `ochakai attach` / `ochakai detach` は凍結の対象外のまま
— issue #470 も別 issue に切り出している。Web UI の同名ボタンも wire の
値ではなく CLI と同じ利用者向けの語であるため、対称に残す。

## 9. 残る三箇所を閉じる

issue #470 の指摘、続き。§7 の `entries` → `concepts` の隣で、同じ形の
不整合が三つ残っていた。

| 何 | 直った形 | なぜ |
|---|---|---|
| `GET /api/v1/context` の `truncated`(整数) | 撤去 | `service.ContextResult` は常に `len(outline)` を入れており、`outline` 自身の長さが同じ合図を運ぶ。バンドル一覧の `truncated`(真偽値、1000 件上限に当たったかを言う)とは別物で、そちらは変えない |
| バンドル一覧のファイル行の `updated_at` | `created_at` | `File` はコンテンツアドレスで不変だとすでに言っている(`domain.File.CreatedAt`)。同じ path への上書きでも `object.created_at` は動かない(`ON CONFLICT DO UPDATE` の `SET` に無い)ので、`updated_at` という綴りは同じ値を違う名前で呼んでいただけだった。隣の `concepts` 行の `updated_at` は本物の更新日時なので変えない |
| `POST /api/v1/reembed` の `embedded` | `concepts` | 数えているのは概念の数で、隣の `files`(対象の名前)と同じ形に揃える — §7 が `entries` に対してやったのと同じ理由 |

## 10. 小さな修正、まとめて

| 何 | 直った形 | なぜ |
|---|---|---|
| `If-None-Match: *` が id 既存で失敗 | 412(旧 409) | RFC 9110 のとおり precondition 失敗は 412。409 は precondition の無い衝突(move の id 衝突など)専用に残す |
| DELETE の `If-Match` | 概念 DELETE が対応(`purge=true` は対象外) | PUT が持つ楽観ロックを DELETE も持つ。purge は二段階目の不可逆な呼び出しで、そこにまで ETag を求めると 0030 §3.5 が避けた往復が増える |
| ファイル PUT の作成応答 | 201(概念 PUT と同じ) | 今までは常に 200 だった |
| `ruling: withdrawn` で取り消す物が無い | 409(旧 404) | purge-on-live と同じ「状態の衝突」であって「概念が無い」ではない。id が本当に無い場合は引き続き 404 |
| `limit` / `days` の範囲外 | 全面 400(旧: 大半は既定へ黙って丸め、`days` だけ 400) | 黙った丸めは `dry_run` が false green を生んだのと同じ形の沈黙。0 または省略は既定、それ以外の範囲外は 400 で範囲を名指す |

## 11. 決めて書くだけ、コードは動かさない

- **6 通りの表現の優先順位**は §4 に書いた規則がそれである。
- **`api/openapi.yaml` は `{path}` を本物のパスパラメータとして表現
  できない**(id が `/` を含み、ルートは空文字になる) — 既知の恒久的な
  欠落として受け入れる。契約テストはリクエストを書き換えて経路を通し、
  どんなジェネレータもこの一つの住所については動くクライアントを
  生成しない。**契約テストが見ないものがもう一つある**: 宣言されて
  いない `requestBody` に本文が来ても、バリデータは何も言わない
  (§16)。宣言の欠落そのものは、読む人が気付くしかない。
- **バージョンや capability の合図は無い**。凍結後に未対応のパラメータ
  を送ったクライアントは、その場で名指しの 400 を受け取る — 事前に
  読んで信じる capability 一覧より確実である。残る穴はリクエストヘッダ
  (中継が足すことがあり、厳格拒否できない)で、これは仕組みではなく
  規則で閉じる: **凍結後、新しいリクエストヘッダに安全性を左右する
  意味を持たせない**(`dry_run` がクエリパラメータである理由そのものを
  一般則に格上げする — 0061)。
- **CORS は意図して無い**。C6(「小さな埋め込み可能な REST API」)は
  「自分の Web サービスに埋め込む」であって「任意のブラウザ起源に直接
  応答する」ではない。`Access-Control-*` を出さないのは見落としではない。
- **凍結を破ってよい唯一の理由はセキュリティ上の欠陥**である。それ以外は
  メジャーバージョンを待つ。セキュリティ修正は [SECURITY.md](../../SECURITY.md)
  の「最新リリースのみサポート・バックポート無し」のとおり、猶予期間を
  置かず次のリリースで出す — 凍結前と同じ扱いである。
- **DELETE の再実行は 404 のままで、それが意図した挙動**である。ソフト
  削除済みの id も最初から無い id も `Store.Get` は区別せず
  `ErrNotFound` を返す(`internal/store/store.go` の `SoftDelete`)。
  盲目的な再試行は安全 — どちらも「その id に生きているものは無い」と
  いう終着点は同じだからである。

## 12. 死んだコード

`internal/restapi/restapi.go` の `queryFloat` を削除した — 0058 が
`min_score` を撤去して以来、呼び出し元が無かった。

## 13. 壊れるもの

**BREAKING**。REST を組み込んでいるクライアントが直す必要があるもの:

- `X-Ochakai-On-Behalf-Of` / `X-Ochakai-Producer` を送っているなら
  `X-` を外す。`On-Behalf-Of` は古い綴りのままだと 400 でなく黙って誤
  身元に書き込む。段階アップグレード中に限らない([Upgrades](../guides/operating.md#upgrades))。
- 未知のクエリパラメータを送っていたら 400 になる。
- `POST /api/v1/move`・`POST /api/v1/review/{id}`・
  `POST /api/v1/usage/{id}` の本文に宣言外のキーを送っていたら 400 になる。
- `GET /api/v1/bundle/{path}` で `history`・`limit`・`files` を、それを
  読まないモードに送っていたら(`limit` を index.md や概念・ファイルの
  住所に、`files` をアーカイブ以外に、`history` をアーカイブ
  (`Accept: application/gzip`)と同時に、など)400 になる。
- `attachments` という JSON キー・クエリパラメータ・MCP ツール名を
  読み書きしていたら `files` に読み替える。
- `stats`・バンドル一覧・`context`(MCP `get_context` も)の JSON キー
  `entries` を読んでいたら `concepts` に読み替える。
- If-None-Match `*` の衝突を 409 として扱っていたら 412 に。
- ファイル PUT の成功判定を 200 のみで書いていたら 201 も見る。
- `ruling: withdrawn` の失敗を 404 として扱っていたら、取り消す物が無い場合は 409 になる。
- 範囲外の `limit` / `days` が黙って既定値に丸まることを当てにして
  いたら、400 として扱う。
- `change` の値・保存済みの `knowledge_revision.change` 行を `attach` /
  `detach` として読んでいたら、`add_file` / `remove_file` に読み替える。
- `GET /api/v1/context`(MCP `get_context` も)の `truncated` を読んで
  いたら、`outline` の長さで代える。
- バンドル一覧のファイル行の `updated_at` を読んでいたら、`created_at`
  に読み替える(値そのものは動かない)。
- `POST /api/v1/reembed` の `embedded` を読んでいたら、`concepts` に
  読み替える。
- `title` を必ずあるものとして読んでいたら、**不在を扱う**(§15)。
  `Summary`(検索の全ヒットと `View` を含む)・`ContextRank`・
  `ContextOutline` の `title` はキーごと無くなり得る。表示名が要るなら
  id の末尾セグメントで補う — OKF SPEC §4.1 がそう書いている。
- `observed.generated.at` を「行が最後に書かれた時刻」として読んで
  いたら、**内容が最後に変わった時刻**に読み替える(§17)。整形だけの
  書き込みでは動かなくなる。行の時刻が要るなら `summary.updated_at`
  である。
- 型を「`/` を含まない文字列」として検証していたら、**`/` を許す**
  (§18)。`acme/Table` は正当な OKF の型であり、これからは 400 に
  ならず concept として保存される。
- `If-Match: *` を送っていたら**外す**(§20.1)。今までは前提条件なしと
  読まれて書き込みが通っていた。これからは 400 で、作成を防ぐつもりで
  送っていたなら**今までが黙って作成していた**ほうが問題である。
- PUT に `If-None-Match: <etag>` を、GET に `If-None-Match: *` を、
  ファイルの PUT / DELETE に両ヘッダのどれかを送っていたら**外す**
  (§20.4)。いずれも今までは黙って捨てられており、**前提条件を付けた
  つもりの書き込みが無条件に通っていた**。
- 同じクエリキーを二度送っていたら、**一度にする**(§20.2)。配列として
  宣言された五つ(`type`・`status`・`trust`・`tag`・`prefix`)はそのまま
  反復してよい。それ以外は 400 になる。
- `change` の値を閉じた集合として検証していたら、**未知の値を通す**
  (§20.3)。今日の九語は変わらないが、契約はもう網羅を約束しない。
- 生成クライアントを使っていて、メソッド名が `searchKnowledge` や
  `getBundleFile` などであれば、`searchConcepts` /
  `getBundlePath` などに変わる(§21)。HTTP は一バイトも動かない。
- `Accept` に `q` を付けて表現を選び分けていたら、**名前だけで選ぶ**
  (§22)。`q=0` は除外にならず、`application/gzip;q=0` はアーカイブを
  返す。大小は区別されなくなり、トークンを部分文字列として含むだけの型
  (`text/markdown-x`)は既定に落ちる。

保存形とワイヤの識別子(id・path)は 1 バイトも動かない。

## 14. 契約と実装の食い違いを閉じる(issue #470 C)

§4 は「表は『エクスポート形』、コードは JSON の View」という食い違いを
一件、手で直した。残りは issue #470 の監査が見つけた 3 件で、いずれも
スペックとコードのどちらが正しいかを決め、もう一方をそれに合わせる。

### 14.1 `?history` の `limit`: 二つの上限を書き分ける

`api/openapi.yaml` は「log.md と `?history` のみ――既定・上限とも
1000」と一枚岩に書いていたが、実装は二通りある: 概念自身の
`?history` を JSON で読むと `checkedLimit(limit, 50, 200)`
(`internal/service/usage.go` の `Revisions`)、log.md および
`?history` のあらゆる markdown レンダリング(ファイルの履歴も含む)は
`checkedLimit(limit, MaxLogLines, MaxLogLines)` = 1000/1000
(`internal/service/browse.go`)。同じ `?history&limit=500` が、
`Accept: application/json` で概念に対しては 400 に、markdown として
は 200 になる――スペックの「1000」は後者にしか当てはまっていない。

**決定:** コードは変えない。概念の JSON 履歴は改訂 1 件ごとに文書
全体を運ぶので、1000 件は知識ベースの複数コピー分になる――`Change`
自身の説明がドキュメントを埋め込まない理由に挙げるのと同じ論拠
(§11)。log.md やファイルの履歴はドキュメントを運ばないので、そこまで
の重さがない。**スペックを直す**: `limit` の説明に、対象によって上限
が違うと書き、両方の数字(50/200 と 1000/1000)を名指す。

### 14.2 概念の GET に ETag はあるが 304 が無かった

`api/openapi.yaml` は `If-None-Match` をこの GET 全体のパラメータと
して宣言していたが、304 の説明は「ファイル」としか書いていなかった。
実装も同じ形の欠落を持っていた: 概念の分岐
(`internal/restapi/restapi.go`)は ETag を計算してヘッダに立てる
だけで `If-None-Match` と比較しておらず、両方のファイル分岐はすでに
比較して 304 を返している。概念に `If-None-Match` を送るクライアント
は、今まで一致していても常に 200 を受け取っていた。

**決定:** 実装する。ETag はすでに計算済みで、比較を一つ足すだけ――
ファイル分岐と同じ `strings.Contains` の形。スペックの 304 応答の
説明と `If-None-Match` パラメータの説明の双方を、概念にも 304 が
返ることを言うよう直す。

### 14.3 ファイル PUT は宣言した ETag を送っていなかった

`api/openapi.yaml` はファイル PUT の 200・201 双方に `ETag` レスポンス
ヘッダを宣言していた(概念 PUT と同じ形)が、実装は JSON を書くだけで
ヘッダを送っていなかった――「宣言してあるのに実装が無い」は §4 が
閉じようとした食い違いと同じ形で、凍結後に既知の嘘を契約に残すことに
なる。

**決定:** 実装する。GET が返すのと同じ形(`"` + sha256 + `"`)で、
書き込んだファイルの ETag を PUT の応答にも立てる。ダウンロードは
今までどおり無し(§11 の一覧のとおり、そこには precondition を掛ける
状態がない)。

これら 3 件はいずれも破壊的変更ではない――§13 の一覧に加わるものは
無い。14.2 は観測できる挙動が変わる(概念に `If-None-Match` を送って
一致したクライアントは、200 の代わりに 304 を受け取るようになる)が、
このヘッダはこの GET 全体のパラメータとしてスペックがすでに宣言して
いたので、これは契約を破る変更ではなく、実装が最初からの契約に追いつ
く完成である。

## 15. `title` は wire でも任意になる

OKF SPEC §4.1 は `title` に既定を与えていない — RECOMMENDED であり、
「省略されていれば consumer はファイル名から題を導いてよい(MAY)」と
書く。[0074](0074-the-document-and-the-vocabulary-that-asks-it.md) §1 は
これを保存形で受け取り、「title は任意、無ければ id の末尾セグメント」
と決めていた。**wire だけが追いついていなかった** — `Summary.title` は
`required` かつ解決済みで、文書が一度も宣言していない値を「これがこの
concept の題である」と言い切っていた。

同じキーが 5 箇所に、**二つの相反する契約**で綴られていた。解決して
必須の `Summary.title`(`SearchHit` が `allOf` で継承するので、検索の
全ヒットと `View` に届く)・`ContextRank.title`・`ContextOutline.title`
に対して、生のまま `omitempty` のバンドル一覧 `concepts[].title` と
`Change.title`。同じ名前が、片方では「文書の題またはファイル名」、
もう片方では「文書の題、無ければキーごと不在」を意味していた。

**決定: 5 箇所とも任意にし、生の値を運ぶ。** `Summary` の `required`
から `title` を外し、`SummaryOf` は `DisplayTitle()` ではなく `k.Title`
を入れる。表示名が要る読み手は id の末尾セグメントから導く — SPEC が
名指しでそう書いている作業であり、`domain.DisplayTitle` は残る。

**なぜ `status` は解決したままなのか。** SPEC §5.4 は `status` に
**規範的な既定**を与えている(「`status` が無ければ `stable`」)。
解決した status は「文書が何を意味するとスペックが言っているか」を
報告しているのであって、書かれていない値を主張してはいない。title に
既定は無い。この非対称はここに書いておく — でなければ次の読み手が
「なぜ status はそのままなのか」を最初から調べ直すことになる。

反対向き(全部を解決する)は成立しない。`Change.title` はファイルの行
(題を持たない)にも purge 済み concept の行にも解決しようがなく、
**「どこでも解決する」という綴りは存在し得ない**。分裂を閉じる綴りは
「どこでも任意」の一つだけである。

契約の例のうち、id の末尾セグメントと同一の `title` を書いていたもの
(`queries/monthly-revenue` に `title: monthly-revenue` など)は落とす。
不在を示すのが例の仕事であって、不在を否定するのはその逆である。

半端に直しても何も落ちない — `TestOpenAPISchemasMatchGoTypes` は
プロパティ名の集合しか見ず `omitempty` を見ないので、`title:` を持たない
文書がヒット・`View`・context pack のそれぞれでキーごと不在になることを
`internal/restapi` の統合テストが直接読む。

## 16. `PUT /api/v1/bundle/{path}` に `requestBody` が無かった

スペックの PUT は description → parameters → responses と続き、
`requestBody` が無い。実装は必須の本文を読み、**そのバイト列が concept
かファイルかを決める**(§4)。**どんなジェネレータも、この契約からは
動く書き込みクライアントを生成できない。**

生き延びた理由は契約テストの側にあった。`carriesOpaqueBytes`
(`internal/restapi/openapi_test.go`)が `/api/v1/bundle/` 配下の PUT を
**すべて**バリデータから外していた — ファイルのバイト列(宣言できる
media type が `*/*` しかない)のために書かれた除外が、**concept の PUT
まで巻き込んでいた**。書き込み面の 200/201 の本体・`ETag`・
`Ochakai-Note`・`Ochakai-Plan`・`Ochakai-Unchanged`・412/413/415 が、
どれも何にも検査されていなかった。§11 が「契約テストはリクエストを
書き換えて経路を通す」と書いた既知の欠落の隣に、**書かれていない欠落**
がもう一つあった。

**決定: `requestBody` を宣言し(`required: true`、concept は
`text/markdown`、ファイルは `application/octet-stream` の binary)、
除外をファイルだけに絞る。** 絞り方はハンドラと同じ規則にする —
markdown のパスで、本文が `type` を持てば concept、それ以外はファイル。
ヘッダの綴りではなく本文で決めるので、除外に穴が残らない。

絞ったことで出たもの、二つ。**(1)** `Content-Type` を送らずに concept を
書く呼び出しがあった。サーバは受け取る(`.md` パスで JSON 以外なら
concept 候補)が、OpenAPI には「Content-Type 不在」を綴る文法が無いので、
宣言した media type を名乗らないリクエストは契約の外である。テストを
`text/markdown` を送る形に直した — サーバは寛容なままでよく、契約は
`text/markdown` か `application/octet-stream` を求める。**(2)** バリデータ
自身は「`requestBody` を宣言していない操作に本文が来たこと」を報告
しない。**つまりこの欠落は、契約テストには最初から見えなかった** —
宣言を消して走らせても緑のままである。§11 の一覧に、契約テストが
見ないもののもう一つとして書き足した。

## 17. `observed.generated.at` が別の瞬間を報告していた

SPEC §5.2: 「`generated.at`: 内容が**最後に意味のある変更**を受けた
ISO 8601 の日時」。

二つのレンダリングが食い違っていた。**文書**は `ContentChangedAt` を
書く(`internal/okf`)—— 正しい。**JSON の wire** は `UpdatedAt` を書く
(`domain.ObservedOf`)—— 誤り。二つは本当にずれる:
`internal/service` は、**言っていることを変えずに整形だけした書き込み**
では `ContentChangedAt` を動かさないと決めており(0046 §3.4)、store は
`UpdatedAt` を必ず動かす。同じ concept の同じ応答の中で、埋め込まれた
文書の `generated.at` と `observed.generated.at` が違う値になっていた。

`api/openapi.yaml` は正しい意味論を**二度**約束していた(`Generated`
スキーマの「when it last changed」、`View` スキーマの「撤去した
`content_changed_at` は冗長だった、四つ目は `observed.generated.at` で
ある」)。`domain.Knowledge.ContentChangedAt` 自身のコメントも「これが
OKF の generated.at の意味である(SPEC §5.2)」と書いている。**スペックと
意図は互いに一致しており、JSON だけが違っていた。**

**決定: `ObservedOf` は `At: k.ContentChangedAt` にする。** 凍結される
フィールドの値が変わる変更であり、**今しかない**。整形だけの PUT が
`observed.generated.at` を動かさず、それがエクスポート形の文書の
`generated.at` と同じ瞬間であることを統合テストが読む。

## 18. 型の文字種: `/` の禁止をやめる

SPEC §4.1: 「型の値は中央登録されない…… consumer は未知の型を
**寛容に扱わなければならない(MUST)**」。§11: consumer は
「未知の `type` の値」を理由にバンドルを拒んではならない。

`api/openapi.yaml` の `Type` は `pattern: "^[^/\\r\\n]{1,128}$"` を持ち、
`domain.ValidType` は `/` を禁じていた。`type: acme/Table` は正当な OKF
なのに ochakai は 400 を返し、取り込みでは**黙って loose なバンドル
ファイルに降格**していた — バイト列は残るが concept ではなくなり、note
も出ず `skipped` にも載らない。SPEC が MUST で求めている寛容さの、
ちょうど反対である。

`/` の禁止は「型が住所として読めないように」という **ochakai 自身の
規則**で、SPEC の裏付けは無い。型はフィルタ・frontmatter・ツール引数で
綴られるものであってパスではないので([0071](0071-the-recommended-type-vocabulary.md))、
`/` が入っても住所にはならない。**長さの上限は残す** — ある種の上限は
普通の防御であり、現実のバンドルは踏まない。ただし「SPEC 由来」と
書くのはやめる。

**決定: 型に `/` を許す。** 契約の `pattern` を `^[^\r\n]{1,128}$` に
広げ、`domain.ValidType` も揃える。応答のパターンは検証するクライアント
が保持するものなので、**これも凍結後には広げられない**。

禁止に依存していたものを調べた結果: `FuzzParse` は `ValidType` に対して
主張するだけなので広がりに追随する、store の `type` 列は制約の無い
`text`、型を path や URL に組み立てているコードは無い(検索の `type=` は
クエリ値、MCP はツール引数、frontmatter は文字列)。**依存は無かった。**
[0074](0074-the-document-and-the-vocabulary-that-asks-it.md) §3・§7 が
「型の文字種の緩和はしない」として置いた除外は、id の緩和を型に及ぼさ
ないという意味では今も生きているが、`/` の一点についてはここで撤回する
— 撤回の理由は id の規則ではなく SPEC §4.1・§11 である。

非文字列スカラの `title:` や `tags` 要素の扱い(SPEC の寛容な適合性が
coercion と note を求めるところで、パーサが文書ごと拒む)は別 PR の
保存形の話であり、ここでは扱わない。

## 19. サーバがすでにそう振る舞っているのに、契約が黙っていたもの

いずれも追加であって、クライアントは壊れない。

- **`Ochakai-Read-Only` を宣言していない応答が三つ**あった:
  `PUT /api/v1/bundle/{path}` の 412、`DELETE /api/v1/bundle/{path}` の
  412、`POST /api/v1/review/{id}` の 409。`announceReadOnly` は
  **すべての**応答に立てる。
- **返しているのに書いていない状態コード**: `GET /api/v1/usage/{id}` の
  400(§2 は 11 操作すべてに未知クエリキーの 400 を与えたが、応答一覧が
  更新されなかった唯一の操作である)、`POST /api/v1/move` ・
  `POST /api/v1/review/{id}` ・ `POST /api/v1/usage/{id}` の 413
  (三つとも 4 MiB の `MaxBytesReader` 越しに本文を読む)。
- **どこにも宣言の無いレスポンスヘッダが三本**: `Cache-Control`
  (ファイル読み出しの両分岐)、`X-Content-Type-Options: nosniff`、
  `Content-Security-Policy: sandbox`。後ろの二本は散文では説明されて
  いたのにヘッダとしては宣言が無かった。`ETag` は 304 分岐の手前で
  concept にもファイルにも立つので、304 にも宣言する。
- `Trust` スキーマの散文に、**tier はこのインスタンスの検証台帳から
  導かれる**こと、文書自身の `verified` キーは `received:` の下に保たれ
  一度も採点されないことを書く(0009 の決定)。SPEC §5.3 だけを読んだ
  consumer は、文書自身のキーが読まれると期待する。
- コードとスペックに残っていた `0064 §12.x` という参照を、実際の節
  番号 §14.x に直す。凍結される契約が存在しない節を指しているのは、
  §14 が閉じようとした食い違いと同じ形である。

三本のヘッダは [docs/surface.md](../surface.md) の HEADER を 10 → 13 に
動かす。同ファイルの surface_test はヘッダを**コードではなくスペックから**
数えるので、サーバが立てて契約が宣言していないヘッダは「誰も数えて
いない表面」だった。**数え方そのものの死角**であり、上げるのは敗北では
なく仕組みが働いた跡である。

## 20. まだ黙っていた三箇所

§2 は沈黙を三つ閉じた(未知のクエリキー・未知の本文キー・モード外の
キー)。凍結の直前に、同じ形が三つ残っていた。どれも**今のうちに名指せば
後で意味を与えられる**という、§2 自身の論法で閉じる。

### 20.1 `If-Match: *`

§10 は `If-None-Match: *` を RFC 9110 のとおり 412 に直した。その鏡写しが
残っていた。RFC 9110 §13.1.1 の `If-Match: *` は「表現が存在するときだけ
実行する」— 作成専用の `If-None-Match: *` に対する**更新専用**の片割れで
ある。ochakai はこれを「前提条件なし」と読んでいたので、**作成を防ぐため
に `*` を送った呼び出し元は黙って作成していた。** `dry_run` の false green
より悪い: あちらは未知のキーを無視していたが、こちらは既知のキーを
**逆向きに実行していた。**

**決定: 400 で名指す。** RFC の意味を実装するのではなく、実装していないと
言う。「存在するときだけ更新する」で詰まった人はおらず、能力を足す理由が
無い(`If-None-Match: *` が載っているのは `--only-if-new` と MCP の
propose という実在の必要があるからで、こちらには対応物が無い)。そして
決め手は、**三択のうち凍結後も動かせるのが 400 だけ**であることである —
常に失敗してきた要求に依存する呼び出し元は存在し得ないので、400 は後の
リリースで RFC の意味に変えられる。「前提条件なし」を凍結すれば道は
永久に閉じ、RFC の意味をいま実装すれば取り消せない。

### 20.2 単値クエリキーの重複

`?limit=10&limit=50` に「求められたとおり」の答えは無い。それでもサーバは
答えていた — **同じリクエストの中で二つの向きに**。`q.Get` を使う大半の
キーは最初の値を、`frontmatterFilter` は最後の値を採っていた。`fm.` の
コメント自身が「同じキー二回は問いではなく矛盾である」と書きながら、
黙って解決していた。

**決定: 配列として宣言されていないクエリキーの重複は 400 で名指す。**
配列は五つ(`type`・`status`・`trust`・`tag`・`prefix`)で、そこでの反復は
集合の綴りである — `?type=Metric&type=Policy` は「どちらか」であって矛盾
ではない。`fm.{key}` は一つずつが単値なので重複は 400 になり、「最後の値が
勝つ」という規則は書き下せなくなって消える(§8 と同じ形 — 規則を消したの
ではなく、主語が無くなった)。

### 20.3 `change` の閉じた列挙

§8 は `attach` / `detach` を、0056 §3.3 は `unreject` を改めた。**どちらも
保存済みの行を書き換えるマイグレーションを伴っている** — この語彙は三か月
で二度動いた。それが `Change.change` と `Revision.change` の**応答スキーマ
に閉じた `enum` として**書かれていた。§18 は「応答のパターンは検証する
クライアントが保持するものなので、凍結後には広げられない」と書いており、
`enum` はそこで言う `pattern` と同じものである。**十語目は永久に足せない。**

**決定: `enum` を外し、開集合だと散文で宣言する。** 九語は例示として残し、
「知らない値は、特別に描くもののないイベントとして扱い、それを理由に
失敗しないこと」と書く。SPEC §4.1 が未知の型について consumer に求める
寛容さを、ochakai 自身の語彙にも当てる。数えるのは引き続き
[docs/surface.md](../surface.md) の VOCAB で、変わるのは**語を足すことが
破壊的変更でなくなる**ことだけである。

**リクエスト側の列挙は触らない。** `ruling`・`outcome`・`sort` は
呼び出し元が**送る**値なので、後から広げても古いクライアントは壊れない。
危ないのは応答に出る列挙だけで、`change` がその中で唯一、実際に動いてきた
語彙だった。残る応答側(`Status`・`Trust`・`Actor.kind`・`Ochakai-Plan`)は
外部の錨を持つか ochakai の決定そのもので、閉じたままにする。

### 20.4 実装していない前提条件は、あと三通りあった

§20.1 が `If-Match: *` を閉じたあと、**同じ形が三つ残っていた**。どれも
「受け取って、黙って捨てる」で、捨てられるのは**前提条件そのもの** —
呼び出し元が防ごうとした無条件の書き込み・読み出しが、まさにそれによって
起きる。

| 綴り | RFC の意味 | ochakai がしていたこと |
|---|---|---|
| PUT の `If-None-Match: <etag>` | このバージョン**でない**ときだけ書く | 黙って無視、無条件に書く |
| GET の `If-None-Match: *` | 何も無いときだけ本文を返す | 黙って無視、200 で全文 |
| ファイルの PUT / DELETE の両ヘッダ | — | **一度も読んでいない** |

**決定: 三つとも 400 で名指す。** 前二つは知識ストアに用途が無い(概念の
バージョンを条件にする綴りは既に一つずつある — 書き込みには `If-Match`、
読み出しには `If-None-Match: <etag>`)。三つめは用途が**ありうる**が実装が
無く、契約だけが宣言していた: この操作は一つの住所で概念とファイルの両方を
書くので両ヘッダを宣言しているが、そこで名指されるバージョンは概念のもの
である(0030)。

**CLI は既にこれを拒んでいた。** `putFile`(`cmd/ochakai/client.go`)は
「`--only-if-new` と `--if-match` は概念のバージョンへの前提条件であり、
ファイルはコンテンツアドレスなので持たない」と言って落ちる。そのコメントは
**「黙って無視される前提条件は、それが防ぐはずだった書き込みそのもので
ある」**と書いており、本節はサーバをその文に追いつかせただけである — 0067
§2 が「CLI は REST の薄いクライアント」と決めている以上、サーバが強制して
いない規則を CLI が知っているのは逆向きだった。

**条件付きファイル書き込みは、後から実装できる。** 400 は §20.1 と同じ理由
で選択肢を開けたまま残す。残る狭い隙間を正直に書いておく: `type` を持たない
`.md` は概念のアドレスに座るファイルなので、そこへの DELETE は概念の経路を
通ってから DeleteFile に落ちる。この一点だけは前提条件が読まれないままだが、
概念の居ない住所への前提条件付き DELETE は既に 404 である(§20.5)。

### 20.5 `If-Match` + 対象不在は 404 で、それは意図した RFC 逸脱である

`If-Match` を付けた PUT が空いている id に届くと `svc.Put` は
`ErrNotFound` を返し、404 になる — **契約はこの 404 を宣言していなかった**
(§19 が閉じた「サーバがそう振る舞っているのに契約が黙っている」型の、
最後の一件)。

RFC 9110 §13.2.2 はここを 412 と定める。**それを採らない。** 412 は
「変わった」と「消えた」を同じ答えに潰すが、呼び出し元にとってこの二つは
別の行動につながる — 前者は読み直して再試行、後者は作り直すか諦めるか
である。`If-Match` 無しの PUT は作成するので、この応答は前提条件を付けた
ときにしか現れない。

**決定: 404 のまま契約に宣言し、RFC からの意図的な逸脱としてここに書く。**
§20.1 が RFC 準拠を選ばなかったのと矛盾しないことを言っておく — あちらの
欠陥は**沈黙**(前提条件が適用されないこと)であって応答コードではなく、
こちらはどちらを選んでも誤った書き込みは起きない。

## 21. 凍結を検査するものが、検査していなかったもの

`api/openapi.frozen.txt` は「クライアントが観測できるものを一行ずつ」と
宣言していたが、**スキーマの制約ファセットを一つも読んでいなかった** —
`pattern`・`format`・`default`・`maximum`・`maxLength`・`readOnly`。
§18 が「凍結後には広げられない」と名指しで書いた `pattern` が、その §18 と
同じ PR で**検査の外に置かれたまま凍結されようとしていた。**

**決定: `schemaFacets` の十六キーと `operationId` を指紋に入れる。**
operationId を入れるのは、§11 が「どんなジェネレータも」と書いてコード
ジェネレータをこの文書の消費者に数えている以上、**生成されるメソッド名は
クライアントが握るもの**だからである。同じ理由で、0057 が退役させた語を
名乗ったままだった五本(`searchKnowledge`・`moveKnowledge`・
`reviewKnowledge`・`getKnowledgeUsage`・`reembedKnowledge`)を
`searchConcepts`・`moveConcept`・`reviewConcept`・`getConceptUsage`・
`reembedConcepts` に改める — HTTP のバイト列は一つも動かない。

**バンドルの三本は住所を名乗る。** `getBundleFile` / `putBundleFile` /
`deleteBundleFile` を `getBundlePath` / `putBundlePath` /
`deleteBundlePath` にする。`File` はこのワイヤでは **concept でない**
バンドルファイルのスキーマ名なので(§6)、主に concept を返す操作が
それを名乗るのは自分の語彙との衝突だった。

`Object` にしないのは、**この操作の定義域がそれより広い**からである。
`GET` はディレクトリ一覧とサブツリーのアーカイブも返し、ディレクトリは
オブジェクトではない — §4 のエラーメッセージ自身が「object であって
ディレクトリではない」と言っている。住所を名乗れば全モードを覆い、union
の名詞を要求しない。同じ理由で GET の summary も「バンドルのオブジェクト
一つを読む」から「バンドルパスにあるものを読む」に直す(`PUT` と
`DELETE` はオブジェクトしか書けず消せないので、そちらは変えない)。

**OKF に揃える先は無い。** SPEC §2 が定義する語は Knowledge Bundle・
Concept・Concept ID の三つだけで、`file` は定義語ではなく地の文にしか
出ない(§3「バンドルは markdown ファイルのディレクトリツリー」)。そして
**非 markdown のファイルは SPEC の範囲外**である — 許可も禁止もされず、
名前もメタデータのフィールドも consumer への規則も無く、§11 の適合条件は
すべて `.md` についてのものである。したがって `File` スキーマを OKF の
語に改める道は存在せず、狭めた定義が規範と衝突してもいない(空いている
場所での定義である)。`resource`(§4.1)は concept が**記述する外部の**
アセットを指すので、ここへの流用は誤りになる。**改めるのではなく、
OKF の外だと契約に書く** — `File` スキーマの説明がそれを言う。

**残る欠落を二つ書いておく。** `GET /api/v1/search` と `?history` の
`limit` はモードによって(既定, 上限)が二組ずつある(検索 10/50 と一覧
100/1000、概念の JSON 履歴 50/200 と markdown 履歴 1000/1000)。**OpenAPI
には「別のパラメータの値で既定が変わる」を綴る文法が無い**ので、この四組は
§14.1 のとおり散文にしかなく、散文は指紋の外である。上限側は挙動テストが
押さえている(`limit=51` が 400)が、既定側は押さえていない。`{path}` の
欠落(§11)と同じ、既知の恒久的な欠落として受け入れる。

二つめは `Accept` である。**OpenAPI は `Accept`・`Content-Type`・
`Authorization` をヘッダパラメータとして宣言することを許さない**(宣言
しても「無視される(SHALL be ignored)」)ので、この三つは指紋にも
[docs/surface.md](../surface.md) の HEADER にも載らない。**表現の集合**
そのものは載っている — 応答の `content` の媒体型と `requestBody` の媒体型
は指紋が読む — が、**どの `Accept` がどれを選ぶかという対応規則**は §22 の
散文にしかない。`limit` の既定と同じ形の、OpenAPI の表現力の欠落である。

この二つを並べて見ると、**凍結が守れるのは「何が在るか」であって「どれを
選ぶと何が返るか」ではない**と分かる。前者は指紋が押さえ、後者は散文と
挙動テストが押さえる。§22 が規則を書き下し、`TestAcceptIsReadAsASetOfNames`
がそれを外から読むのは、そのためである。

## 22. `Accept` は順位ではなく名前の集合として読む

§4 は 6 通りの表現に優先順位を与えたが、**その順位を何から読むかは書いて
いなかった**。コードは `strings.Contains` で、結果として二つの綴りが規則と
食い違っていた: `text/markdown-x` は markdown として答えられ、大文字の
`TEXT/MARKDOWN` は答えられなかった(RFC 9110 では媒体型は大小を区別しない)。
**規則とコードが一致していたのは偶然だった。**

そしてもう一つ、決めなければ凍る綴りがあった。`Accept: application/gzip;q=0`
は RFC 9110 では「gzip **以外**をくれ」だが、ochakai は gzip を返す。

**決定: `Accept` は名前の集合として読み、`q` は他のパラメータと一緒に
落とす。`q=0` も実装しない。** 実装したうえで、そう書き下す。

**選択肢は二つあり、`Accept` をやめるほうは採らない。** 6 通りの表現を
クエリパラメータ(`?as=json` のような)で選ぶ案は、表面を減らさず**移す**
だけである — PARAM が 19 から 20 になり、標準のヘッダで足りているものに
第二の綴りを作ることになる。C6(「小さな埋め込み可能な REST API」)に効く
のは HTTP を設計どおり使うほうで、`curl -H 'Accept: application/gzip'` は
そのまま export である。詰まった人もいない([docs/surface.md](../surface.md)
の一つめの問い)。

**`q=0` を実装しないほうを選んだ理由は、ここで交渉していないからである。**
一つの住所は既定の表現を一つ持ち、選択トークンは最大二つで、**順位を付ける
対象が存在しない**。そして**既定は「名指さない」ことで必ず得られる**ので、
「gzip ではないもの」を表現する必要が原理的に生じない — 除外が言えることを、
省略が既に言っている。

**§20.1 が `If-Match: *` を 400 にしたのと違う答えになる理由**も書いておく。
あちらの欠陥は**黙った書き込み**で、防ごうとした当のものが起きた。こちらは
読み出しで、呼び出し元は受け取った gzip を見ればそれと分かる。加えて
`Accept` は §11 が「中継が足すことがあるので厳格拒否できない」と書いた側の
ヘッダである。**同じ形に見えて重大さが違うので、同じ答えにしない。**

現実に `q=0` を送るクライアントは実質存在しない — ブラウザが出すのは
`q=0.8` / `q=0.9` で、`q=0` は明示的な除外なので既定設定からは出ない。
だからこの決定が変えるのは挙動ではなく**約束**であり、約束は書いておかないと
凍らない。
