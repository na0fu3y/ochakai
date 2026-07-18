# ochakai 設計ドキュメント 0013: 添付ファイルの一般化と GCS 一本化

Status: Accepted(2026-07-18 の議論で決定)
Date: 2026-07-18

## 1. 目的

設計ドキュメント 0008 は添付を「画像のみ」と定めた。しかし OKF の
現実のバンドルは `.md` の横に `.txt` などの非 OKF データを平気で置く
(例: GoogleCloudPlatform/knowledge-catalog の crypto_bitcoin サンプル
の `seeds.txt`)。エントリの根拠となるデータファイル — スキーマの
シード、golden query の期待結果、6 ページの仕様 PDF — を、画像と同じ
仕組みでフラットに export/import できるべきである。

同時に、添付バイト列の置き場を GCS に一本化する。設計ドキュメント
0011 は bytea と GCS の二重化を「移行路」として敷いたが、移行は完了
した(2026-07-18)。二重化の維持コスト — 条件付き再インライン化、
読み出しのフォールバック、ロールバック注意書き — を払い続ける理由は
もうない。

## 2. 決定

### 2.1 許可リスト = Claude が読めて、Gemini が埋め込める形式

添付として受け付けるのは、次の**両方**を満たす形式である:

1. Claude にアップロードできる形式
   ([Upload files to Claude](https://support.claude.com/en/articles/8241126-upload-files-to-claude))
2. gemini-embedding-2 が入力に取れる形式
   ([Gemini Embedding 2](https://docs.cloud.google.com/gemini-enterprise-agent-platform/models/gemini/embedding-2))

読む側(エージェント)と探す側(将来のマルチモーダル埋め込み検索)の
双方が扱えない形式を貯めても、解釈も検索もできない死蔵になるからだ。
交わりは:

| media_type | 由来 |
|---|---|
| `image/png` `image/jpeg` `image/webp` | 両リスト |
| `image/gif` | 既存許可リストの祖父条項(下記) |
| `application/pdf` | 両リスト |
| `text/plain` | 両リスト |

- **`image/gif` は残す。** gemini-embedding-2 のリストには無いが、
  0008 以来の許可形式であり、既存データと OKF ラウンドトリップを壊して
  まで落とす利得がない。将来の埋め込み対象からは外れるだけである。
- **`text/plain` はスニッフィング判定**(従来どおりバイト列が決める。
  クライアント申告の media type は信用しない)。Go の
  `http.DetectContentType` は UTF-8/UTF-16 テキストを `text/plain` と
  判定するので、`.txt` `.csv` `.json` `seeds.txt` はみなプレーン
  テキストとして通る — Claude 側リストの CSV/JSON を個別に列挙する
  必要はない。HTML は `text/html` と判定されて弾かれる(次項)。
- **明示的に受けないもの**: `text/html` と `image/svg+xml`(スクリプト
  を運べる。API から配信すれば知識作者全員に XSS ベクタを配ることに
  なる — 0008 の判断を維持)、DOCX/XLSX 等の ZIP 系(スニッフィングで
  `application/zip`、embedding-2 も取れない)、音声・動画・
  BMP/HEIC/HEIF/AVIF(交わりにない)。XML 宣言なしの SVG など HTML
  シグネチャに当たらないマークアップは `text/plain` として通り得るが、
  配信も常に `text/plain` + `nosniff` なので、ブラウザに SVG/HTML と
  して解釈されることはない — XSS 面はここで閉じる。
- サイズ上限(5 MiB)・エントリあたり上限(20)は変えない。embedding-2
  の PDF 上限は 6 ページであり、「原本ではなく根拠」という 0008 の
  添付観はファイル一般化後も変わらない。

### 2.2 配信と MCP 表現

- REST 配信は従来どおり API がプロキシし、`inline` + `nosniff` で返す。
  許可リストに実行可能なものが無いことが安全性の根拠であり、リストを
  広げるときは配信の再検討が条件になる。`text/plain` には
  `charset=utf-8` を付けて返す(charset 無しの text はブラウザの推測に
  委ねることになる)。
- MCP の `get_attachment` は media type で表現を選ぶ:
  `image/*` → image content、`text/plain` → text content、
  `application/pdf` → embedded resource(base64 blob)。

### 2.3 インポートの帰属: 参照駆動 + 正準名前空間

帰属の原則は変わらない: 本文のマークダウンリンクが指すファイルは、
その本文のエントリに付く(場所は問わず、元の場所は `okf_path` で保存)。

これに一つ追加する: **どの本文からも参照されていない**非 markdown
ファイルでも、バンドルパスが `<type>/<id>/<name>` の形でバンドル内の
エントリ `<type>/<id>` の正準ディレクトリに載っているものは、その
エントリに付く。エクスポートの正準レイアウト(0008)の逆写像であり、
「エントリの OKF 名前空間に置かれたものはエントリの一部」という同じ
規則の読み出し側である。これで crypto_bitcoin 型の「.md の横のデータ
ファイル」が、本文リンクなしでもラウンドトリップする。

どちらにも当たらないファイル(ルート直下の孤児、どのエントリの名前
空間でもない場所のファイル)は従来どおり skip 報告に載る。

### 2.4 バイト列は GCS のみ。bytea は廃止

- `blob.bytes` 列は migration 0009 で落とす。落とす前に未移行の行が
  残っていれば migration は明確なエラーで止まる(「`OCHAKAI_GCS_BUCKET`
  を設定して一度起動すれば移行される」)。起動シーケンスは
  ブロブストア設定 → バックフィル → migration の順に組み替え、
  バケットを設定して起動すれば一度のブートで移行と列削除が終わる。
- `OCHAKAI_GCS_BUCKET` 未設定のインスタンスは**添付機能なし**で動く:
  attach は「GCS なしでは markdown エントリのみ」というエラー
  (REST: 501)を返し、インポートは添付だけ失敗として報告される。
  「Docker だけでローカル起動」は markdown の知識ベースとしては
  従来どおり成立し、添付を使うときだけバケットが要る。
- 0011 §2.4 の「再インライン化による自己修復」パスは削除する。
  ロールバック先の bytea がもう無い。

## 3. やらないこと

- **添付バイト列の埋め込み(マルチモーダル検索)。** 許可リストを
  embedding-2 の交わりに揃えるのは将来それをやるための地ならしで
  あって、本ドキュメントでは検索対象は従来どおりテキストのみ。
- **サイズ上限の引き上げ。** 「原本置き場」化は 0008 の添付観への
  逆行。必要になったらそのとき別途決める。
- **許可リストの設定化。** 形式の追加は配信の安全性(2.2)との
  抱き合わせ判断であり、env で緩められてはならない。
- **bytea モードの温存・S3 等の非 GCP バックエンド。** 0003・0011 の
  とおり。`blob.Store` インターフェイスはテストのフェイクのためだけに
  残る。
