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

## 8. 小さな修正、まとめて

| 何 | 直った形 | なぜ |
|---|---|---|
| `If-None-Match: *` が id 既存で失敗 | 412(旧 409) | RFC 9110 のとおり precondition 失敗は 412。409 は precondition の無い衝突(move の id 衝突など)専用に残す |
| DELETE の `If-Match` | 概念 DELETE が対応(`purge=true` は対象外) | PUT が持つ楽観ロックを DELETE も持つ。purge は二段階目の不可逆な呼び出しで、そこにまで ETag を求めると 0030 §3.5 が避けた往復が増える |
| ファイル PUT の作成応答 | 201(概念 PUT と同じ) | 今までは常に 200 だった |
| `ruling: withdrawn` で取り消す物が無い | 409(旧 404) | purge-on-live と同じ「状態の衝突」であって「概念が無い」ではない。id が本当に無い場合は引き続き 404 |
| `limit` / `days` の範囲外 | 全面 400(旧: 大半は既定へ黙って丸め、`days` だけ 400) | 黙った丸めは `dry_run` が false green を生んだのと同じ形の沈黙。0 または省略は既定、それ以外の範囲外は 400 で範囲を名指す |

## 9. 決めて書くだけ、コードは動かさない

- **6 通りの表現の優先順位**は §4 に書いた規則がそれである。
- **`api/openapi.yaml` は `{path}` を本物のパスパラメータとして表現
  できない**(id が `/` を含み、ルートは空文字になる) — 既知の恒久的な
  欠落として受け入れる。契約テストはリクエストを書き換えて経路を通し、
  どんなジェネレータもこの一つの住所については動くクライアントを
  生成しない。
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

## 10. 死んだコード

`internal/restapi/restapi.go` の `queryFloat` を削除した — 0058 が
`min_score` を撤去して以来、呼び出し元が無かった。

## 11. 壊れるもの

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

保存形とワイヤの識別子(id・path)は 1 バイトも動かない。

## 12. 契約と実装の食い違いを閉じる(issue #470 C)

§4 は「表は『エクスポート形』、コードは JSON の View」という食い違いを
一件、手で直した。残りは issue #470 の監査が見つけた 3 件で、いずれも
スペックとコードのどちらが正しいかを決め、もう一方をそれに合わせる。

### 12.1 `?history` の `limit`: 二つの上限を書き分ける

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
(§9)。log.md やファイルの履歴はドキュメントを運ばないので、そこまで
の重さがない。**スペックを直す**: `limit` の説明に、対象によって上限
が違うと書き、両方の数字(50/200 と 1000/1000)を名指す。

### 12.2 概念の GET に ETag はあるが 304 が無かった

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

### 12.3 ファイル PUT は宣言した ETag を送っていなかった

`api/openapi.yaml` はファイル PUT の 200・201 双方に `ETag` レスポンス
ヘッダを宣言していた(概念 PUT と同じ形)が、実装は JSON を書くだけで
ヘッダを送っていなかった――「宣言してあるのに実装が無い」は §4 が
閉じようとした食い違いと同じ形で、凍結後に既知の嘘を契約に残すことに
なる。

**決定:** 実装する。GET が返すのと同じ形(`"` + sha256 + `"`)で、
書き込んだファイルの ETag を PUT の応答にも立てる。ダウンロードは
今までどおり無し(§9 の一覧のとおり、そこには precondition を掛ける
状態がない)。

これら 3 件はいずれも破壊的変更ではない――§11 の一覧に加わるものは
無い。12.2 は観測できる挙動が変わる(概念に `If-None-Match` を送って
一致したクライアントは、200 の代わりに 304 を受け取るようになる)が、
このヘッダはこの GET 全体のパラメータとしてスペックがすでに宣言して
いたので、これは契約を破る変更ではなく、実装が最初からの契約に追いつ
く完成である。
