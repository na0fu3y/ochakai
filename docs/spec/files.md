# ファイル

concept でないバンドルのオブジェクト — 図、PDF、attester のコード、データの
写し。**知識は concept に書き、ファイルはその根拠や道具として置く。**

## オブジェクトとしてのファイル

- ファイルは**自分のパスに座るバンドルのオブジェクト**で、concept の属性では
  ない。`.md` 以外の拡張子を持つ(`.md` の住所には concept しか座れない)。
- **どの concept のものかは導出する**: その concept の本文がリンクで指す
  ファイルと、`<id>/` 名前空間の直下にあるファイル。添付という操作は無く、
  置くのは `PUT`、外すのは `DELETE` である。どの concept からも指されない
  ファイルも、置いたパスにそのまま残り、export に出る。
- 上限は一つ 5 MiB。空のファイルは受けない。
- **メディアタイプはバイト列から判定する**(申告は信じない)。**形式で
  拒まない** — 拒めばバンドルが往復しない。
- バイト列は SHA-256 で名指され、同じ内容は一度だけ保存される。置き場所は
  `OCHAKAI_GCS_BUCKET` があれば GCS、無ければ PostgreSQL で、どちらでも
  同じ約束を守る([okf.md](okf.md)・[concurrency.md](concurrency.md))。

## 配信

- `GET /api/v1/bundle/<パス>` がバイト列を返し、ETag は内容のハッシュ、
  `If-None-Match` で 304。`Accept: application/json` なら中身の無い
  メタデータ(パス・メディアタイプ・大きさ・ハッシュ)を返す。
- **受けるが、描画しない。** 画像・PDF・プレーンテキストだけをインラインで
  返し、それ以外は `Content-Disposition: attachment` と
  `Content-Security-Policy: sandbox` を付けて返す。`nosniff` は常に付く。
  HTML と SVG はインラインで配信しない。
- `?history` はそのファイルの変更の履歴(JSON)である。

## 検索

- **ファイル名は字句検索に入る**ので、どのデプロイでも名前で見つかる。
- 埋め込むデプロイでは、ファイルもベクトルを持ち(パスで引く)、検索では
  そのファイルを持つ concept のヒットとして数える。画像と PDF のバイト列を
  埋め込めるのは `gemini-embedding-2` 系で、テキストのファイルはどのモデル
  でも本文を埋め込む([search.md](search.md))。
- **ochakai はファイルを解釈しない** — OCR も要約も、PDF のテキスト抽出も
  しない。ファイルで分かったことは concept の本文に書く。そこが検索される。

## 面

- **REST**: `/api/v1/bundle/{path}` の GET・PUT・DELETE。
- **CLI**: `ochakai put`・`get`・`delete` がファイルも扱う(拡張子のある
  パスがファイルの住所)。`ochakai get <パス>` はバイト列を標準出力に出す。
  `ochakai get <id> --download <dir>` は concept が持つファイルを落とす。
  取り込みと export はファイルも運ぶ。
- **MCP**: `get_file` で一つ読むだけ。ファイルを書くツールは持たない
  (base64 をツールの引数に載せない)。
- **Web UI**: concept の詳細でファイルを見る・足す・外す。受け取る形式を
  絞らない。

## やらないこと

- ファイルの中身の解釈(OCR・抽出・キャプション)、どのファイルが当たったか
  の表示、中身の DB への複製。
- 形式の許可リスト。
- 一 concept あたりのファイル数の上限(帰属が導出になったので意味が無い)。
- MCP からファイルを書くこと。

## 経緯

設計記録 [0075](../design/0075-the-bundle-is-the-address-space.md) §1・§5・
[0091](../design/0091-a-file-vector-is-keyed-by-its-path.md)・
[0099](../design/0099-a-purge-reaches-the-bytes.md)・
[0131](../design/0131-a-deployment-says-what-it-cannot-do.md)・
[0140](../design/0140-one-address-reads-as-well-as-writes.md)、
決定 [0156](../decisions/0156-files-live-in-postgres-without-a-bucket.md)。
