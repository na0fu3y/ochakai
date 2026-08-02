# ochakai 設計ドキュメント 0064: REST は /api/v1 で止まる

Status: Accepted(2026-08-02)。既存のドキュメントを Superseded にも
改訂にもしない — [0046](0046-bundle-address-space.md) §§2.1・3.3・3.5、
[0057](0057-concept-is-the-word-a-reader-meets.md) §3.2、
[0058](0058-filters-nobody-arrived-through.md)、
[0061](0061-a-dry-run-is-the-write-withheld.md)、
[0030](0030-optimistic-locking.md) を引用するのみ。実装は同 PR
(issue [#379](https://github.com/na0fu3y/ochakai/issues/379))。
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

## 5. 住所の綴り: 宣言だけで足りる

`/api/v1/bundle/{path}` の `.md`、`/api/v1/review|usage/{id}`
の裸の id、`/api/v1/move` の body 内 id — 3 通りに見えるが、コードを
見ると実は矛盾していない。`.md` は既に `Accept: text/markdown` の糖衣
であって識別子の一部ではなく(`Summary.id`・`File.path`・`Change.path`
は元から裸である)、単一対象の操作(bundle・review・usage)はパス
セグメントで、二つの対象を持つ `move` だけが body で名指す — 後者は
二対象という形の必然であって、もう一つの非一貫性ではない。**この規則を
一度だけ書き下す**: 識別子は常に裸の id / path。`.md` は表現選択子。
単一対象はパス、複数対象は body。コード変更は無い。

## 6. `attachments` / `Attachment` を wire から退役させる

0046 §2.1 は「添付(attachment)という概念は対象消滅する」とすでに
決めていたが、綴りだけが生き残っていた。wire 上のこの語をすべて
`files` / `File` に改める: `Attachment` スキーマと `View`・
`SearchHit`・`ReembedResult` の JSON キー、アーカイブ住所の
`?attachments=` クエリ(→ `?files=`)、MCP ツール `get_attachment`
(→ `get_file`)。`entries` は対象外のまま — [0057](0057-concept-is-the-word-a-reader-meets.md)
§3.2 が「配列の名前であって単位の名前ではない」として除外した決定は
変わらない。

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

## 7. 小さな修正、まとめて

| 何 | 直った形 | なぜ |
|---|---|---|
| `If-None-Match: *` が id 既存で失敗 | 412(旧 409) | RFC 9110 のとおり precondition 失敗は 412。409 は precondition の無い衝突(move の id 衝突など)専用に残す |
| DELETE の `If-Match` | 概念 DELETE が対応(`purge=true` は対象外) | PUT が持つ楽観ロックを DELETE も持つ。purge は二段階目の不可逆な呼び出しで、そこにまで ETag を求めると 0030 §3.5 が避けた往復が増える |
| ファイル PUT の作成応答 | 201(概念 PUT と同じ) | 今までは常に 200 だった |
| `ruling: withdrawn` で取り消す物が無い | 409(旧 404) | purge-on-live と同じ「状態の衝突」であって「概念が無い」ではない。id が本当に無い場合は引き続き 404 |
| `limit` / `days` の範囲外 | 全面 400(旧: 大半は既定へ黙って丸め、`days` だけ 400) | 黙った丸めは `dry_run` が false green を生んだのと同じ形の沈黙。0 または省略は既定、それ以外の範囲外は 400 で範囲を名指す |

## 8. 決めて書くだけ、コードは動かさない

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

## 9. 死んだコード

`internal/restapi/restapi.go` の `queryFloat` を削除した — 0058 が
`min_score` を撤去して以来、呼び出し元が無かった。

## 10. 壊れるもの

**BREAKING**。REST を組み込んでいるクライアントが直す必要があるもの:

- `X-Ochakai-On-Behalf-Of` / `X-Ochakai-Producer` を送っているなら
  `X-` を外す。
- 未知のクエリパラメータを送っていたら 400 になる。
- `attachments` という JSON キー・クエリパラメータ・MCP ツール名を
  読み書きしていたら `files` に読み替える。
- If-None-Match `*` の衝突を 409 として扱っていたら 412 に。
- ファイル PUT の成功判定を 200 のみで書いていたら 201 も見る。
- `ruling: withdrawn` の失敗を 404 として扱っていたら、取り消す物が
  無い場合は 409 になる。
- 範囲外の `limit` / `days` が黙って既定値に丸まることを当てにして
  いたら、400 として扱う。

保存形とワイヤの識別子(id・path)は 1 バイトも動かない。
