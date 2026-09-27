# REST の安定性契約

REST の何が動かず、何が動くか。利用者に向けた方針の正は英語の
[compatibility.md](../compatibility.md) で、ここは**変える人が守る規則**を
日本語で持つ。

## 凍るのは OKF のコアだけ

- **凍結されているのは、バンドルの往復と検索** — `GET` / `PUT` / `DELETE
  /api/v1/bundle/{path}` と `GET /api/v1/search` の四つである。
  利用者が約束として受け取るのは OKF のバンドルと、そこへの入口だからで
  ある。
- 残りの `/api/v1`(`review`・`usage`・`stats`・`move`・`reembed`・`access`・
  `frontmatter`・`agent` など)は 0.x の不安定な面で、MCP・CLI と同じく
  minor で変わりうる。変えるときは CHANGELOG に **BREAKING** と書く。
  それぞれ、動かなくなった時点で、その判断を記録して凍る。
- **コアは広がることはあっても狭まらない。**

## 凍結は検査される

- `api/openapi.frozen.txt` がコアの指紋で、操作・`operationId`・
  パラメータ・ヘッダ・状態コード・スキーマの欄と制約を一行ずつ持つ。
  `cmd/ochakai/frozenwire_test.go` が契約と突き合わせて落ちる。
- 説明文は指紋に入らない — 契約の散文は凍結後も直せる。

## 凍結の外にある二つの追加

指紋に行が増えるが、壊さない追加なので許される。同じ PR で golden を
再生成し(`go test ./cmd/ochakai -run TestFrozenWireHasNotMoved -update`)、
説明に何のための追加かを書く。

1. **応答だけに出るスキーマへのプロパティの追加** — 知らない欄をクライアントは
   無視する(例: `dirs[].description`)。
2. **任意のクエリパラメータの追加** — 未知の鍵を 400 にしているので、後から
   足しても送らない呼び出し元の答えは変わらない(例: `cursor`)。

## 凍結を破ってよい理由

コアを壊してよいのは次のときだけで、記録と CHANGELOG の **BREAKING** を伴う。

1. **セキュリティ上の欠陥。**
2. **出力が OKF に適合しないこと。** `.md` に型の無い文書を置けなくした
   変更、concept の住所への素の GET に文書を返すようにした変更(決定
   [0153](../decisions/0153-serve-the-bundle-as-okf.md))がこれである。
3. **OKF が既に定めた綴りの二つ目を畳むこと**(`?history` の markdown を
   やめ、`log.md` に一本化した変更)。
4. **撤去した能力が残した欄を落とすこと**(却下が削除になったときの
   `rejected` など)。欄を整えるためには使わない。

**指紋が持てないものがある** — どの入力にどの答えが返るか。上の 2 と 3 は
指紋を一行も動かさずに振る舞いを変えたので、記録と CHANGELOG が唯一の
記載になる。

## 契約の書き方の規則

- **REST は唯一の契約**で、[api/openapi.yaml](../../api/openapi.yaml) 一枚が
  持つ。`internal/restapi`・`internal/mcpserver`・`internal/apiclient` を
  それに揃え、REST のテストの要求と応答は契約に照合される。
- **宣言していないクエリの鍵と本文の鍵は 400 で名指す。** 本文の鍵は
  完全一致で照合し、同じ鍵の重複も 400 である。
- **エラーは文と `code` を運ぶ。** 文は予告なく変わってよく、`code` は
  閉じた語彙(いま 11 語)で、分岐はこちらでする。語を足すのは凍結を動かす
  変更である。
- `Accept` は優先順位ではなく名前の集合として読む。名指さなければ既定の
  表現が返る。

## 経緯

設計記録 [0064](../design/0064-rest-stops-at-api-v1.md)・
[0082](../design/0082-what-the-freeze-holds-still.md)・
[0083](../design/0083-an-error-carries-a-code.md)・
[0100](../design/0100-md-is-how-a-concept-is-spelled.md)・
[0101](../design/0101-a-level-can-be-walked.md)・
[0102](../design/0102-one-history-in-one-spelling.md)・
[0107](../design/0107-the-freeze-holds-the-okf-core.md)・
[0125](../design/0125-a-body-names-each-field-once.md)・
[0135](../design/0135-a-rejection-is-a-deletion.md)。
